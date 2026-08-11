package network

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	DefaultStartupURL     = "https://cp.cloudflare.com/generate_204"
	startupRetryInterval  = time.Second
	startupAttemptTimeout = 3 * time.Second
)

type connectivityCheckOptions struct {
	retryInterval  time.Duration
	attemptTimeout time.Duration
}

// WaitForConnectivity waits until target is reachable through the configured
// egress path or timeout expires. A non-positive timeout disables the check.
func WaitForConnectivity(ctx context.Context, cfg *ProxyConfig, target string, timeout time.Duration, logger *slog.Logger) error {
	return waitForConnectivity(ctx, cfg, target, timeout, logger, connectivityCheckOptions{
		retryInterval:  startupRetryInterval,
		attemptTimeout: startupAttemptTimeout,
	})
}

func waitForConnectivity(ctx context.Context, cfg *ProxyConfig, target string, timeout time.Duration, logger *slog.Logger, opts connectivityCheckOptions) error {
	if timeout <= 0 {
		return nil
	}
	if target == "" {
		target = DefaultStartupURL
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	started := time.Now()
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; ; attempt++ {
		remaining := time.Until(started.Add(timeout))
		if remaining <= 0 {
			break
		}
		attemptTimeout := min(opts.attemptTimeout, remaining)
		if err := checkConnectivity(deadlineCtx, cfg, target, attemptTimeout); err == nil {
			logger.Info("network ready", "attempts", attempt, "elapsed", time.Since(started).Round(time.Millisecond), "proxy", cfg != nil)
			return nil
		} else {
			lastErr = err
		}

		remaining = time.Until(started.Add(timeout))
		if remaining <= 0 {
			break
		}
		retryIn := min(opts.retryInterval, remaining)
		logger.Warn("network not ready", "attempt", attempt, "retry_in", retryIn, "err", lastErr)

		timer := time.NewTimer(retryIn)
		select {
		case <-deadlineCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("startup connectivity check failed after %s: %w", timeout, lastErr)
		case <-timer.C:
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("startup connectivity check failed after %s: %w", timeout, lastErr)
}

func checkConnectivity(ctx context.Context, cfg *ProxyConfig, target string, timeout time.Duration) error {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := NewClient(cfg, false, "", timeout)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("create connectivity request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("connectivity endpoint returned %s", resp.Status)
	}
	return nil
}
