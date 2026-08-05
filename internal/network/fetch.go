package network

import (
	"log/slog"
	"time"

	"github.com/dop251/goja"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
)

// FetchOptions configures the egress used by fetch() calls from scripts.
type FetchOptions struct {
	Proxy       *ProxyConfig
	DialTimeout time.Duration
	Logger      *slog.Logger
}

// FetchFactory returns a goja host function implementing the synchronous
// fetch(url, params?) global described in miaospeed-scripts/global.d.ts.
// It returns goja.Null() when every retry attempt fails, matching the
// existing goja engine's resp == nil behavior.
func FetchFactory(vm *goja.Runtime, opts FetchOptions) func(call goja.FunctionCall) goja.Value {
	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	return func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		params := call.Argument(1)

		method := "GET"
		body := ""
		sni := ""
		useHost := false
		noRedir := false
		retry := 1
		timeout := int64(3000)
		headers := map[string]string{}
		cookies := map[string]string{}

		if paramsObj, ok := jsObject(params); ok {
			if v, ok := jsString(paramsObj.Get("method")); ok {
				method = v
			}
			if v, ok := jsString(paramsObj.Get("body")); ok {
				body = v
			}
			if v, ok := jsString(paramsObj.Get("sni")); ok {
				sni = v
			}
			if v, ok := jsBool(paramsObj.Get("useHost")); ok {
				useHost = v
			}
			if v, ok := jsBool(paramsObj.Get("noRedir")); ok {
				noRedir = v
			}
			if v, ok := jsInt64(paramsObj.Get("retry")); ok {
				retry = int(v)
			}
			if v, ok := jsInt64(paramsObj.Get("timeout")); ok {
				timeout = v
			}
			if hdrObj, ok := jsObject(paramsObj.Get("headers")); ok {
				for _, key := range hdrObj.Keys() {
					if v, ok := jsString(hdrObj.Get(key)); ok {
						headers[key] = v
					}
				}
			}
			if ckObj, ok := jsObject(paramsObj.Get("cookies")); ok {
				for _, key := range ckObj.Keys() {
					if v, ok := jsString(ckObj.Get(key)); ok {
						cookies[key] = v
					}
				}
			}
		}

		logger.Debug("fetch call", "method", method, "url", url, "useHost", useHost, "noRedir", noRedir, "retry", retry, "timeoutMs", timeout)

		proxyCfg := opts.Proxy
		client, err := NewClient(proxyCfg, useHost, sni, dialTimeout)
		if err != nil {
			logger.Warn("failed to build http client", "url", url, "err", err)
			return goja.Null()
		}

		respBody, resp, redirects := RequestWithRetry(client, retry, timeout, &RequestOptions{
			Method:  method,
			URL:     url,
			Headers: headers,
			Cookies: cookies,
			Body:    []byte(body),
			NoRedir: noRedir,
			SNI:     sni,
		}, logger)

		if resp == nil {
			return goja.Null()
		}

		ret := map[string]interface{}{
			"status":     resp.Status,
			"statusCode": resp.StatusCode,
			"cookies":    resp.Cookies(),
			"headers":    flattenHeaders(resp.Header),
			"method":     method,
			"url":        url,
			"body":       string(respBody),
			"redirects":  redirects,
		}
		return vm.ToValue(ret)
	}
}

func jsObject(v goja.Value) (*goja.Object, bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, false
	}
	obj, ok := v.(*goja.Object)
	return obj, ok
}

func jsString(v goja.Value) (string, bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "", false
	}
	s, ok := v.Export().(string)
	return s, ok
}

func jsBool(v goja.Value) (bool, bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return false, false
	}
	b, ok := v.Export().(bool)
	return b, ok
}

func jsInt64(v goja.Value) (int64, bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return 0, false
	}
	switch n := v.Export().(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	}
	return 0, false
}
