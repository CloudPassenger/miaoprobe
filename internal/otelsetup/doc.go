// Package otelsetup builds the OpenTelemetry MeterProvider used by the serve
// subcommand. A single set of instruments is fed by two readers: a
// Prometheus bridge (pull, for /metrics) and, optionally, an OTLP periodic
// reader that pushes the same data to a remote OpenTelemetry endpoint (e.g.
// an OTel Collector or a vendor's OTLP gateway such as Grafana Cloud),
// without requiring an external collector process.
package otelsetup
