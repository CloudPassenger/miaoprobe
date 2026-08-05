package network

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
)

// MaxRequestTimeoutMs bounds the per-attempt timeout a script may request
// via fetch()'s `timeout` param. Scripts are untrusted input as far as
// scheduling is concerned: an unclamped value (combined with up to 10
// retries) would let a single script stall a whole poll cycle for minutes,
// since goja's interrupt cannot preempt a blocking Go call.
const MaxRequestTimeoutMs int64 = 30000

// DefaultRequestTimeoutMs is used when a script omits `timeout` or passes a
// non-positive value, matching miaospeed's fetch default.
const DefaultRequestTimeoutMs int64 = 3000

// RequestOptions mirrors the fields fetch() accepts from a script.
type RequestOptions struct {
	Method  string
	URL     string
	Headers map[string]string
	Cookies map[string]string
	Body    []byte
	NoRedir bool
	SNI     string
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// clampTimeoutMs normalizes a script-supplied per-attempt timeout into
// (0, MaxRequestTimeoutMs].
func clampTimeoutMs(v int64) int64 {
	if v <= 0 {
		return DefaultRequestTimeoutMs
	}
	if v > MaxRequestTimeoutMs {
		return MaxRequestTimeoutMs
	}
	return v
}

func doRequest(ctx context.Context, client *http.Client, opt *RequestOptions) ([]byte, *http.Response, []string, error) {
	var reader io.Reader
	if len(opt.Body) > 0 {
		reader = bytes.NewReader(opt.Body)
	}

	req, err := http.NewRequestWithContext(ctx, opt.Method, opt.URL, reader)
	if err != nil {
		return nil, nil, nil, err
	}
	for k, v := range opt.Headers {
		req.Header.Add(k, v)
	}
	for k, v := range opt.Cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	redirects := []string{}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if opt.NoRedir || len(redirects) > 64 {
			return http.ErrUseLastResponse
		}
		redirects = append(redirects, req.URL.String())
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, redirects, err
	}
	return body, resp, redirects, nil
}

// RequestWithRetry sends the request up to retry times (clamped to [1,10]),
// waiting timeoutMs milliseconds (clamped to MaxRequestTimeoutMs) for each
// attempt, and returns the first successful response. If every attempt
// fails, resp is nil.
//
// Each attempt's deadline is derived from ctx, so cancelling ctx (process
// shutdown, or the per-probe deadline) aborts the in-flight request and
// stops further retries instead of running to completion.
func RequestWithRetry(ctx context.Context, client *http.Client, retry int, timeoutMs int64, opt *RequestOptions, logger *slog.Logger) ([]byte, *http.Response, []string) {
	retry = clampInt(retry, 1, 10)
	timeoutMs = clampTimeoutMs(timeoutMs)
	if logger == nil {
		logger = logging.Discard()
	}

	var body []byte
	var resp *http.Response
	var redirects []string

	for i := 0; resp == nil && i < retry; i++ {
		if err := ctx.Err(); err != nil {
			logger.Debug("http request canceled before attempt", "method", opt.Method, "url", opt.URL, "attempt", i+1, "err", err)
			break
		}

		logger.Debug("http request", "method", opt.Method, "url", opt.URL, "attempt", i+1, "retry", retry, "timeoutMs", timeoutMs)

		attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		b, r, rd, err := doRequest(attemptCtx, client, opt)
		cancel()

		if err != nil {
			logger.Debug("http request attempt failed", "method", opt.Method, "url", opt.URL, "attempt", i+1, "err", err)
			continue
		}

		body, resp, redirects = b, r, rd
		logging.Trace(logger, "http response", "method", opt.Method, "url", opt.URL, "attempt", i+1,
			"statusCode", r.StatusCode, "bodyBytes", len(b), "redirects", len(rd))
	}

	if resp == nil {
		logger.Warn("http request exhausted retries", "method", opt.Method, "url", opt.URL, "retry", retry)
	}

	return body, resp, redirects
}

// flattenHeaders converts an http.Header into a lowercase-keyed
// map[string]string, matching the Record<string, string> contract in
// miaospeed-scripts/global.d.ts (multi-valued headers are comma-joined).
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return out
}
