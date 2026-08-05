package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"

	"golang.org/x/net/proxy"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// ProxyConfig describes the single egress proxy fetch() dials through when
// useHost is not set. A nil ProxyConfig means direct/local egress.
type ProxyConfig struct {
	Scheme string
	Host   string
	User   string
	Pass   string
}

// ParseProxy parses a proxy URL of the form scheme://[user:pass@]host:port.
// An empty string returns (nil, nil), meaning direct egress.
func ParseProxy(raw string) (*ProxyConfig, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url %q: %w", raw, err)
	}
	switch u.Scheme {
	case schemeHTTP, schemeHTTPS, "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (only http/https/socks5 are supported)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("proxy url %q is missing a host", raw)
	}

	cfg := &ProxyConfig{Scheme: u.Scheme, Host: u.Host}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Pass, _ = u.User.Password()
	}
	return cfg, nil
}

// dialContext returns the low-level TCP dial function for the given egress
// mode. HTTP(S) proxies are handled by the caller via http.Transport.Proxy
// (CONNECT tunneling), so they dial directly here; SOCKS5 proxies dial
// through an x/net/proxy client.
func dialContextFunc(cfg *ProxyConfig, direct bool, dialer *net.Dialer) (func(context.Context, string, string) (net.Conn, error), error) {
	if direct || cfg == nil {
		return dialer.DialContext, nil
	}

	switch cfg.Scheme {
	case schemeHTTP, schemeHTTPS:
		return dialer.DialContext, nil
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if cfg.User != "" {
			auth = &proxy.Auth{User: cfg.User, Password: cfg.Pass}
		}
		d, err := proxy.SOCKS5("tcp", cfg.Host, auth, dialer)
		if err != nil {
			return nil, fmt.Errorf("create socks5 dialer: %w", err)
		}
		ctxDialer, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("socks5 dialer does not support context dialing")
		}
		return ctxDialer.DialContext, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", cfg.Scheme)
	}
}
