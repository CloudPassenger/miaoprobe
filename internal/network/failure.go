package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
)

const (
	RequestFailureTimeout           = "timeout"
	RequestFailureDNS               = "dns"
	RequestFailureConnectionRefused = "connection_refused"
	RequestFailureConnectionReset   = "connection_reset"
	RequestFailureUnreachable       = "unreachable"
	RequestFailureTLS               = "tls"
	RequestFailureProxy             = "proxy"
	RequestFailureConnection        = "connection_error"
)

// ClassifyRequestError maps transport errors to a stable, low-cardinality
// reason suitable for metrics labels and machine-readable output.
func ClassifyRequestError(err error) string {
	if err == nil {
		return ""
	}

	message := strings.ToLower(err.Error())
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return RequestFailureTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return RequestFailureTimeout
	}

	// Proxy failures often wrap ordinary DNS or TCP errors. Classify the
	// egress dependency first so operators know the proxy is the failing hop.
	if strings.Contains(message, "proxyconnect") ||
		strings.Contains(message, "proxy error") ||
		strings.Contains(message, "socks5") ||
		strings.Contains(message, "socks connect") {
		return RequestFailureProxy
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) ||
		strings.Contains(message, "no such host") ||
		strings.Contains(message, "server misbehaving") {
		return RequestFailureDNS
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return RequestFailureConnectionRefused
	}
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return RequestFailureConnectionReset
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return RequestFailureUnreachable
	}

	var certificateVerificationErr *tls.CertificateVerificationError
	var unknownAuthorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var certificateInvalidErr x509.CertificateInvalidError
	var recordHeaderErr tls.RecordHeaderError
	if errors.As(err, &certificateVerificationErr) ||
		errors.As(err, &unknownAuthorityErr) ||
		errors.As(err, &hostnameErr) ||
		errors.As(err, &certificateInvalidErr) ||
		errors.As(err, &recordHeaderErr) ||
		strings.Contains(message, "tls:") ||
		strings.Contains(message, "x509:") ||
		strings.Contains(message, "certificate") {
		return RequestFailureTLS
	}

	return RequestFailureConnection
}
