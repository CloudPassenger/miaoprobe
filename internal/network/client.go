package network

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

// NewClient builds an http.Client for one request. direct bypasses cfg
// entirely (used for fetch's useHost option). sni overrides the TLS
// ServerName when set. dialTimeout bounds the underlying TCP connect.
func NewClient(cfg *ProxyConfig, direct bool, sni string, dialTimeout time.Duration) (*http.Client, error) {
	dialer := &net.Dialer{Timeout: dialTimeout}
	dialFn, err := dialContextFunc(cfg, direct, dialer)
	if err != nil {
		return nil, err
	}

	tlsConf := &tls.Config{}
	if sni != "" {
		tlsConf.ServerName = sni
	}

	transport := &http.Transport{
		DialContext:           dialFn,
		MaxIdleConns:          100,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConf,
	}

	if !direct && cfg != nil && (cfg.Scheme == schemeHTTP || cfg.Scheme == schemeHTTPS) {
		proxyURL := &url.URL{Scheme: cfg.Scheme, Host: cfg.Host}
		if cfg.User != "" {
			proxyURL.User = url.UserPassword(cfg.User, cfg.Pass)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &http.Client{Transport: transport}, nil
}
