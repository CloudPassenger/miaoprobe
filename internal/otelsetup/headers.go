package otelsetup

import (
	"fmt"
	"strings"
)

// ParseHeaders parses a comma-separated "key=value" list, as used by the
// serve subcommand's --otel.headers flag for OTLP auth (e.g. Grafana
// Cloud's "Authorization=Basic <base64>"). An empty string yields a nil map
// so the OTLP exporter falls back to OTEL_EXPORTER_OTLP_HEADERS.
func ParseHeaders(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	headers := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("otelsetup: invalid header %q, want key=value", pair)
		}
		headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return headers, nil
}
