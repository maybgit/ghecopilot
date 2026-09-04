package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ghecopilot/internal/router"
	"ghecopilot/pkg/certificate"
	"ghecopilot/pkg/hosts"
	"ghecopilot/pkg/message"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
)

// 检查端口是否被占用，如果被占用则退出程序
func checkPortAndExit(host string, port int) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("端口: %d 已被占用, 运行结束!", port)
	}
	conn.Close()
}

func main() {
	// 设置 GIN 模式为 release，关闭 debug 日志
	gin.SetMode(gin.ReleaseMode)

	// 设置日志输出
	setupLogging()

	// 在非生产环境中加载 .env 文件
	if os.Getenv("ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			log.Printf("Warning: Error loading .env file: %v", err)
		}
	}

	log.Println("Current Environment: ", os.Getenv("ENV"))

	// 设置默认环境变量
	initDefaultEnv()

	// 检查并自动配置hosts文件
	configureHosts()

	r := gin.Default()
	// 添加 HSTS 中间件
	r.Use(func(c *gin.Context) {
		c.Header("Strict-Transport-Security", "max-age=0")
		c.Next()
	})

	// 初始化router
	router.NewHTTPRouter(r)

	// 获取配置
	httpPort, _ := strconv.Atoi(os.Getenv("PORT"))
	httpsPort, _ := strconv.Atoi(os.Getenv("HTTPS_PORT"))
	host := os.Getenv("HOST")

	// 初始化证书
	certFile, keyFile, reloadChan, err := certificate.InitCertificates()
	if err != nil {
		log.Fatalf("Failed to initialize certificates: %v", err)
	}

	// 检查端口是否被占用
	checkPortAndExit(host, httpPort)
	checkPortAndExit(host, httpsPort)

	// 创建一个带取消功能的上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建一个错误组
	g, groupCtx := errgroup.WithContext(ctx)

	// 创建一个通道来表示服务器已经启动
	serverStarted := make(chan struct{}, 2)

	// 启动HTTP服务器
	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, httpPort),
		Handler: r,
	}
	g.Go(func() error {
		log.Printf("Starting HTTP server on %s\n", httpServer.Addr)
		serverStarted <- struct{}{}
		return httpServer.ListenAndServe()
	})

	// 创建一个函数来启动HTTPS服务器
	var httpsServer *http.Server
	startHTTPSServer := func() *http.Server {
		server := &http.Server{
			Addr:    fmt.Sprintf("%s:%d", host, httpsPort),
			Handler: r,
			TLSConfig: &tls.Config{
				MinVersion:               tls.VersionTLS12,
				MaxVersion:               tls.VersionTLS13,
				PreferServerCipherSuites: true,
				// 启用 SNI 支持 (Server Name Indication)
				GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
					cert, err := tls.LoadX509KeyPair(certFile, keyFile)
					if err != nil {
						log.Printf("[TLS] 加载证书失败: %v", err)
						return nil, err
					}
					return &cert, nil
				},
				// 仅启用安全的密钥交换算法，避免已弃用的 RC4/SHA1 套件
				CipherSuites: []uint16{
					tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
					tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				},
				// 仅支持现代椭圆曲线
				CurvePreferences: []tls.CurveID{
					tls.X25519,
					tls.CurveP256,
					tls.CurveP384,
					tls.CurveP521,
				},
				// 启用 TLS 会话票证以提高性能
				SessionTicketsDisabled: false,
			},
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 0, // 流式(SSE)反向代理：关闭写超时，避免本地大模型首token等待超过120s时重置HTTP/2流
		}

		g.Go(func() error {
			log.Printf("Starting HTTPS server on %s\n", server.Addr)
			if httpsServer == nil { // 仅在首次启动时发送信号
				serverStarted <- struct{}{}
			}
			return server.ListenAndServeTLS(certFile, keyFile)
		})

		return server
	}

	// 启动初始HTTPS服务器
	httpsServer = startHTTPSServer()

	// 等待两个服务器都启动
	<-serverStarted
	<-serverStarted

	// 显示消息或消息框
	message.ShowAppLaunchMessage()

	// 监听证书更新和关闭信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 处理证书更新和服务器关闭
	go func() {
		for {
			select {
			case <-reloadChan:
				log.Println("Certificate update detected, reloading HTTPS server...")

				// 创建关闭超时上下文
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)

				// 关闭当前的HTTPS服务器
				if err := httpsServer.Shutdown(shutdownCtx); err != nil {
					log.Printf("Error shutting down HTTPS server: %v", err)
				}
				shutdownCancel()

				// 启动新的HTTPS服务器
				httpsServer = startHTTPSServer()

			case <-quit:
				log.Println("Shutdown signal received, exiting...")
				cancel()
				return

			case <-groupCtx.Done():
				log.Println("Unexpected exit, trying to shutdown gracefully...")
				cancel()
				return
			}
		}
	}()

	// 等待取消信号
	<-ctx.Done()

	// 给服务器一些时间来完成正在处理的请求
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// 优雅地关闭服务器
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server Shutdown: %v", err)
	}

	if err := httpsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTPS server Shutdown: %v", err)
	}

	// 等待所有 goroutine 完成
	if err := g.Wait(); err != nil && err != http.ErrServerClosed {
		log.Printf("Error during server operations: %v", err)
	}
}

// configureHosts 自动配置hosts文件
func configureHosts() {
	// 从环境变量获取域名列表
	domainsStr := os.Getenv("HOSTS_DOMAINS")
	if domainsStr == "" {
		log.Println("[HOSTS] 未配置 HOSTS_DOMAINS，跳过hosts检查")
		return
	}

	// 解析域名列表
	domains := strings.Split(domainsStr, ",")
	var validDomains []string
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain != "" {
			validDomains = append(validDomains, domain)
		}
	}

	if len(validDomains) == 0 {
		log.Println("[HOSTS] 没有有效的域名配置")
		return
	}

	// 创建hosts管理器
	manager := hosts.NewHostsManager(validDomains)

	// 检查并修复
	if err := manager.CheckAndFix(); err != nil {
		log.Printf("[HOSTS] 警告: %v", err)
		log.Println("[HOSTS] 请手动检查hosts文件: C:\\Windows\\System32\\drivers\\etc\\hosts")
	}
}

func setupLogging() {
	// 创建日志目录
	logDir := "logs"
	err := os.MkdirAll(logDir, 0755)
	if err != nil {
		log.Fatal("无法创建日志目录:", err)
	}

	// 创建日志文件，使用当前日期作为文件名
	currentTime := time.Now()
	logFileName := currentTime.Format("2006-01-02") + ".log"
	logFilePath := filepath.Join(logDir, logFileName)

	// 打开日志文件
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal("无法创建日志文件:", err)
	}

	// 同时输出到控制台和文件
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func initDefaultEnv() {
	// 设置默认环境变量
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "development")
	}
	if os.Getenv("PORT") == "" {
		os.Setenv("PORT", "1188")
	}
	if os.Getenv("HTTPS_PORT") == "" {
		os.Setenv("HTTPS_PORT", "443")
	}
	if os.Getenv("HOST") == "" {
		os.Setenv("HOST", "0.0.0.0")
	}
}
