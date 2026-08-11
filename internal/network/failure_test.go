package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
)

func TestClassifyRequestError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", want: ""},
		{name: "timeout", err: context.DeadlineExceeded, want: RequestFailureTimeout},
		{name: "proxy", err: fmt.Errorf("proxyconnect tcp: %w", syscall.ECONNREFUSED), want: RequestFailureProxy},
		{name: "dns", err: &net.DNSError{Err: "no such host", Name: "example.invalid"}, want: RequestFailureDNS},
		{name: "refused", err: fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED), want: RequestFailureConnectionRefused},
		{name: "reset", err: fmt.Errorf("read tcp: %w", syscall.ECONNRESET), want: RequestFailureConnectionReset},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, want: RequestFailureConnectionReset},
		{name: "unreachable", err: fmt.Errorf("dial tcp: %w", syscall.EHOSTUNREACH), want: RequestFailureUnreachable},
		{name: "tls", err: errors.New("tls: handshake failure"), want: RequestFailureTLS},
		{name: "generic", err: errors.New("malformed network response"), want: RequestFailureConnection},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyRequestError(tt.err); got != tt.want {
				t.Fatalf("ClassifyRequestError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
