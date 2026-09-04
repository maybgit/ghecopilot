package httpclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Client returns a shared HTTP client with proper connection pooling and timeouts.
// Using a shared client prevents requests from hanging when connections have been idle.
func Client() *http.Client {
	once.Do(func() {
		timeoutStr := os.Getenv("HTTP_CLIENT_TIMEOUT")
		timeout := 10 * time.Second
		if timeoutStr != "" {
			if d, err := time.ParseDuration(timeoutStr + "s"); err == nil {
				timeout = d
			}
		}
		shared = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	})
	return shared
}

var (
	once   sync.Once
	shared *http.Client
)
