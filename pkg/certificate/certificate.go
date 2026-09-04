package certificate

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	certPath    = "cert/ssl.pem"
	keyPath     = "cert/ssl.key"
	caCertPath  = "cert/ca.pem"
	caKeyPath   = "cert/ca.key"
	defaultHost = "my.ghe.com"
)

var (
	httpsServerReload chan struct{}
)

// getDomains 动态设置域名列表
func getDomains() []string {
	// 从 HOSTS_DOMAINS 环境变量获取域名
	domainsStr := os.Getenv("HOSTS_DOMAINS")
	if domainsStr != "" {
		domains := strings.Split(domainsStr, ",")
		var result []string
		for _, d := range domains {
			d = strings.TrimSpace(d)
			if d != "" {
				result = append(result, d)
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	// 如果环境变量未设置，从 hosts 管理器获取域名
	return getDomainsFromHostsManager()
}

// getDomainsFromHostsManager 从 hosts 管理器中获取域名（避免循环导入）
func getDomainsFromHostsManager() []string {
	domainsStr := os.Getenv("HOSTS_DOMAINS")
	if domainsStr == "" {
		// 默认使用主机名
		hostname, err := os.Hostname()
		if err == nil && hostname != "" && hostname != "localhost" {
			domainsStr = hostname
		} else {
			domainsStr = defaultHost
		}
	}

	domains := strings.Split(domainsStr, ",")
	var result []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d != "" {
			result = append(result, d)
		}
	}
	return result
}

// buildSANs 从域名列表构建 SAN 的 DNSNames 和 IPAddresses
func buildSANs(domains []string) (dnsNames []string, ipAddrs []net.IP) {
	seen := make(map[string]bool)
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		// 处理 IP 地址
		if ip := net.ParseIP(d); ip != nil {
			if !seen[ip.String()] {
				seen[ip.String()] = true
				ipAddrs = append(ipAddrs, ip)
			}
			continue
		}
		// 处理通配符域名: *.domain.com -> 同时加入 *.domain.com 和 domain.com
		if strings.HasPrefix(d, "*.") {
			base := strings.TrimPrefix(d, "*.")
			if !seen[d] {
				seen[d] = true
				dnsNames = append(dnsNames, d)
			}
			if !seen[base] {
				seen[base] = true
				dnsNames = append(dnsNames, base)
			}
		} else if !seen[d] {
			seen[d] = true
			dnsNames = append(dnsNames, d)
		}
	}
	return
}

// generateCA 生成自签名 CA 证书和密钥
func generateCA() (caCert *x509.Certificate, caKey *rsa.PrivateKey, caCertDER []byte, err error) {
	caKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成 CA 密钥失败: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成序列号失败: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "ghecopilot Local CA",
			Organization: []string{"ghecopilot"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err = x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("创建 CA 证书失败: %w", err)
	}

	caCert, err = x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析 CA 证书失败: %w", err)
	}

	return caCert, caKey, caCertDER, nil
}

// generateServerCert 使用 CA 签发服务器证书
func generateServerCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, domains []string) (serverCert *x509.Certificate, serverKey *rsa.PrivateKey, serverCertDER []byte, err error) {
	serverKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成服务器密钥失败: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成序列号失败: %w", err)
	}

	dnsNames, ipAddrs := buildSANs(domains)
	if len(dnsNames) == 0 && len(ipAddrs) == 0 {
		dnsNames = []string{defaultHost}
	}

	// CN 使用第一个域名
	cn := defaultHost
	if len(dnsNames) > 0 {
		cn = dnsNames[0]
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{"ghecopilot"},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().AddDate(10, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: ipAddrs,
	}

	serverCertDER, err = x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("创建服务器证书失败: %w", err)
	}

	serverCert, err = x509.ParseCertificate(serverCertDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析服务器证书失败: %w", err)
	}

	return serverCert, serverKey, serverCertDER, nil
}

// writePEM 将证书和密钥写入 PEM 文件
func writePEM(certPath, keyPath string, certDER []byte, key *rsa.PrivateKey) error {
	certOut := &bytes.Buffer{}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("编码证书 PEM 失败: %w", err)
	}
	if err := os.WriteFile(certPath, certOut.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入证书文件失败: %w", err)
	}

	keyOut := &bytes.Buffer{}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return fmt.Errorf("编码密钥 PEM 失败: %w", err)
	}
	if err := os.WriteFile(keyPath, keyOut.Bytes(), 0600); err != nil {
		return fmt.Errorf("写入密钥文件失败: %w", err)
	}
	return nil
}

// loadCA 从文件加载已有的 CA 证书和密钥
func loadCA() (caCert *x509.Certificate, caKey *rsa.PrivateKey, caCertDER []byte, err error) {
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取 CA 证书失败: %w", err)
	}
	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取 CA 密钥失败: %w", err)
	}

	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return nil, nil, nil, fmt.Errorf("无法解码 CA 证书 PEM")
	}
	caCert, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析 CA 证书失败: %w", err)
	}
	caCertDER = block.Bytes

	keyBlock, _ := pem.Decode(caKeyPEM)
	if keyBlock == nil {
		return nil, nil, nil, fmt.Errorf("无法解码 CA 密钥 PEM")
	}
	caKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析 CA 密钥失败: %w", err)
	}

	return caCert, caKey, caCertDER, nil
}

// installCAToSystem 将 CA 证书安装到系统信任库（best-effort）
func installCAToSystem() error {
	switch runtime.GOOS {
	case "windows":
		return installCAWindows()
	case "darwin":
		return installCAMacOS()
	default:
		return installCALinux()
	}
}

func installCAWindows() error {
	psCommand := fmt.Sprintf("$cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2('%s'); $store = New-Object System.Security.Cryptography.X509Certificates.X509Store('Root','CurrentUser'); $store.Open('ReadWrite'); $store.Add($cert); $store.Close()", caCertPath)
	psCmd := exec.Command("powershell", "-NoProfile", "-Command", psCommand)
	psCmd.Stdout = os.Stdout
	psCmd.Stderr = os.Stderr

	log.Println("[CERT] 正在将 CA 证书导入 Windows 当前用户信任库...")
	if err := psCmd.Run(); err != nil {
		return fmt.Errorf("导入 CA 到 Windows 信任库失败: %w", err)
	}
	log.Println("[CERT] CA 证书已成功导入 Windows 当前用户信任库!")
	return nil
}

func installCAMacOS() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法获取用户主目录: %w", err)
	}
	keychain := filepath.Join(homeDir, "Library", "Keychains", "login.keychain-db")
	cmd := exec.Command("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", keychain, caCertPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Println("[CERT] 正在将 CA 证书导入 macOS 钥匙串...")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("导入 CA 到 macOS 钥匙串失败: %w", err)
	}
	log.Println("[CERT] CA 证书已成功导入 macOS 钥匙串!")
	return nil
}

func installCALinux() error {
	destDir := "/usr/local/share/ca-certificates"
	destFile := filepath.Join(destDir, "ghecopilot-ca.crt")
	data, err := os.ReadFile(caCertPath)
	if err != nil {
		return fmt.Errorf("读取 CA 证书失败: %w", err)
	}
	if err := os.WriteFile(destFile, data, 0644); err != nil {
		return fmt.Errorf("写入 CA 到系统目录失败(可能需要 root 权限): %w", err)
	}
	cmd := exec.Command("update-ca-certificates")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update-ca-certificates 执行失败(可能需要 root 权限): %w", err)
	}
	log.Println("[CERT] CA 证书已成功安装到 Linux 系统信任库!")
	return nil
}

// generateCertificates 纯 Go 生成 CA + 服务器证书
func generateCertificates(domains []string) error {
	log.Println("[CERT] 未发现现有证书，使用纯 Go 自动生成...")

	var caCert *x509.Certificate
	var caKey *rsa.PrivateKey
	var caCertDER []byte

	// 尝试加载已有 CA（避免每次启动都重新生成 CA 导致信任库失效）
	if fileExists(caCertPath) && fileExists(caKeyPath) {
		var err error
		caCert, caKey, _, err = loadCA()
		if err != nil {
			log.Printf("[CERT] 警告: 加载已有 CA 失败(%v)，将重新生成 CA", err)
			caCert, caKey, caCertDER = nil, nil, nil
		}
	}

	// 如果没有 CA，生成新的并安装到系统信任库
	if caCert == nil {
		var err error
		caCert, caKey, caCertDER, err = generateCA()
		if err != nil {
			return fmt.Errorf("生成 CA 失败: %w", err)
		}

		// 保存 CA 文件
		if err := writePEM(caCertPath, caKeyPath, caCertDER, caKey); err != nil {
			return fmt.Errorf("保存 CA 文件失败: %w", err)
		}
		log.Printf("[CERT] CA 证书已保存: %s, %s", caCertPath, caKeyPath)

		// 安装 CA 到系统信任库（best-effort，失败不阻断启动）
		if err := installCAToSystem(); err != nil {
			log.Printf("[CERT] 警告: 自动安装 CA 到系统信任库失败: %v", err)
			log.Println("[CERT] 请手动将 CA 证书导入系统信任库: " + caCertPath)
		}
	}

	// 生成服务器证书
	_, serverKey, serverCertDER, err := generateServerCert(caCert, caKey, domains)
	if err != nil {
		return fmt.Errorf("生成服务器证书失败: %w", err)
	}

	// 保存服务器证书
	if err := writePEM(certPath, keyPath, serverCertDER, serverKey); err != nil {
		return fmt.Errorf("保存服务器证书失败: %w", err)
	}
	log.Printf("[CERT] 服务器证书已保存: %s, %s", certPath, keyPath)
	log.Printf("[CERT] 证书域名: %s", strings.Join(domains, ", "))
	return nil
}

// tryGenerateCertificate 尝试自动生成 SSL 证书
func tryGenerateCertificate() {
	// 检查是否已存在证书
	if fileExists(certPath) && fileExists(keyPath) {
		log.Printf("[CERT] 发现现有证书: %s, 跳过自动生成", certPath)
		return
	}

	// 获取域名列表
	domains := getDomains()
	if len(domains) == 0 {
		log.Println("[CERT] 警告: 未获取到域名，使用默认域名")
		domains = []string{defaultHost}
	}

	// 纯 Go 生成证书
	if err := generateCertificates(domains); err != nil {
		log.Printf("[CERT] 证书生成失败: %v", err)
	}
}

// InitCertificates 初始化证书管理 - 如果证书不存在则尝试自动生成
func InitCertificates() (string, string, chan struct{}, error) {
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create cert directory: %v", err)
	}

	httpsServerReload = make(chan struct{}, 1)

	// 验证现有证书文件是否存在
	if !fileExists(certPath) || !fileExists(keyPath) {
		// 尝试自动生成证书
		tryGenerateCertificate()
	}

	// 再次检查证书文件是否存在
	if !fileExists(certPath) || !fileExists(keyPath) {
		return "", "", nil, fmt.Errorf("certificate files not found: %s or %s", certPath, keyPath)
	}

	// 验证证书是否有效
	if err := validateCertificate(); err != nil {
		log.Printf("Warning: certificate validation failed: %v", err)
	}

	log.Printf("Using existing certificate: %s", certPath)

	return certPath, keyPath, httpsServerReload, nil
}

// validateCertificate 验证证书是否有效
func validateCertificate() error {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %v", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %v", err)
	}

	log.Printf("Certificate subject: %s, expires: %s, SANs: %d DNS + %d IP",
		cert.Subject.CommonName,
		cert.NotAfter.Format("2006-01-02"),
		len(cert.DNSNames),
		len(cert.IPAddresses),
	)
	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
