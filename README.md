# MiaoProbe

MiaoProbe is a standalone media-unlock / network-quality probing tool. It
embeds a minimal [goja](https://github.com/dop251/goja) JavaScript engine
that implements the host API contract expected by
[`miaospeed-scripts`](https://github.com/CloudPassenger/miaospeed-scripts) build
artifacts, so the same detection scripts used by `miaospeed` can be run
without pulling in any encrypted-proxy client (mihomo/clash, V8/QuickJS,
CGO, etc.). It runs either as a one-shot CLI check or as a long-running
process that exports results as Prometheus metrics on an interval, in the
spirit of [MediaUnlockTest](https://github.com/HsukqiLee/MediaUnlockTest).

**Egress support is limited to direct/local, HTTP(S) proxy, and SOCKS5
proxy. Encrypted proxy protocols (VMess/Trojan/Shadowsocks/mihomo/...) are
out of scope and will never be supported by this project.**

## Script source

Scripts are not bundled with this repository. Point `--scripts` at a local
checkout of [`miaospeed-scripts`](https://github.com/CloudPassenger/miaospeed-scripts)'s
build output (`dist/`), which contains `index.json` plus one `.js` file per
check, built with `pnpm run build` in that repository. You can also point
`--scripts` at a single `.js` file for ad-hoc testing.

## Quick start: one-shot check

```sh
go build -o miaoprobe ./cmd/miaoprobe

# run every script in a miaospeed-scripts build, direct egress
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist

# run a single script file
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist/global/netflix.js

# only global/stream scripts, through a SOCKS5 proxy, JSON output
./miaoprobe check \
  --scripts /path/to/miaospeed-scripts/dist \
  --proxy socks5://127.0.0.1:1080 \
  --filter global,stream \
  --timeout 30s \
  --format json
```

`check` flags:

| Flag        | Default | Description                                              |
|-------------|---------|-----------------------------------------------------------|
| `--scripts` | -       | required; a `.js` file or a directory with `index.json`   |
| `--proxy`   | ``      | `http://host:port` / `https://host:port` / `socks5://host:port`; empty = direct |
| `--filter`  | ``      | comma-separated region/tag match, e.g. `hk,stream`         |
| `--timeout` | `30s`   | per-script execution timeout                               |
| `--format`  | `table` | `table` (colorized box-drawing table) or `json`             |

## Serve mode: Prometheus exporter + OpenTelemetry push

```sh
./miaoprobe serve \
  --scripts /path/to/miaospeed-scripts/dist \
  --proxy http://127.0.0.1:8080 \
  --interval 5m \
  --listen :9765
```

`serve` flags:

| Flag         | Default | Description                                    |
|--------------|---------|-------------------------------------------------|
| `--scripts`  | -       | required; a directory with `index.json`         |
| `--proxy`    | ``      | same as `check`                                 |
| `--filter`   | ``      | same as `check`                                 |
| `--interval` | `5m`    | polling interval (duration format, e.g. `30s`, `5m`) |
| `--timeout`  | `30s`   | per-script execution timeout                    |
| `--listen`   | `:9765` | address `/metrics` is served on                 |

`serve` also builds on a single OpenTelemetry `MeterProvider` internally, so
the same instruments can be read two ways at once: pulled from `/metrics`
(Prometheus) and/or pushed on an interval to a remote OTLP endpoint — no
external OTel Collector needed. OTLP push flags:

| Flag              | Default          | Description                                    |
|-------------------|------------------|-------------------------------------------------|
| `--otel-endpoint`  | ``               | OTLP endpoint to push to; empty disables push (falls back to `OTEL_EXPORTER_OTLP_*` env vars) |
| `--otel-protocol`  | `http/protobuf`  | `http/protobuf` or `grpc`                       |
| `--otel-headers`   | ``               | comma-separated `key=value` headers sent with every export, e.g. auth |
| `--otel-insecure`  | `false`          | disable TLS (local collectors only)             |
| `--otel-interval`  | `1m`             | how often buffered metrics are pushed           |

For `http/protobuf`, if `--otel-endpoint` has no `/v1/metrics` suffix it is
appended automatically, so a gateway base URL works as-is.

### Pushing to Grafana Cloud

Grafana Cloud's OTLP gateway accepts metrics directly, no collector
required. Find the endpoint and generate an API token under your stack's
"OpenTelemetry" configuration page, then:

```sh
./miaoprobe serve \
  --scripts /path/to/miaospeed-scripts/dist \
  --otel-endpoint https://otlp-gateway-prod-xx.grafana.net/otlp \
  --otel-headers "Authorization=Basic $(echo -n '<instance-id>:<api-token>' | base64 -w0)" \
  --otel-interval 1m
```

`/metrics` keeps serving locally at the same time, so a local Prometheus
scrape and the Grafana Cloud push can both run off the same poll cycle.

Exposed metrics:

- `miaoprobe_unlock_status{id,name,region,tags}` — gauge, taken from the
  script's explicit `status` field when set, otherwise mapped from the
  `background` RGB result per `miaospeed-scripts/consts/colors.ts`:
  `1` unlocked, `0` failed, `0.5` warning, `-1` unknown. Scripts whose result
  is "N/A" (`status: "na"` or background `142,140,142`) are skipped (no
  series emitted) rather than reported as any numeric status.
- `miaoprobe_check_duration_seconds{id}` — gauge, wall time of the last run.
- `miaoprobe_check_errors_total{id}` — counter, incremented when a script
  *fails to execute* (host API error, script exception, or timeout) — not
  when it reports a business "failed"/"unlock" result.

### Prometheus / Grafana

```yaml
# prometheus.yml
scrape_configs:
  - job_name: miaoprobe
    static_configs:
      - targets: ["localhost:9765"]
```

In Grafana, a simple panel query is:

```promql
miaoprobe_unlock_status{tags=~".*stream.*"}
```

mapped through a value mapping (`1` → green "Unlocked", `0` → red "Failed",
`0.5` → orange "Warning", `-1` → gray "Unknown") gives a status grid similar
to MediaUnlockTest's output. Dashboards are not shipped with this project;
build them against the metrics above.

## Logging

`--log-level` and `--log-format` are global flags accepted by both `check`
and `serve`. Logs always go to stderr, so `check --format json` output on
stdout stays parseable even with verbose logging enabled.

| Flag           | Default | Values                                                        |
|----------------|---------|-----------------------------------------------------------------|
| `--log-level`  | `info`  | `trace`, `debug`, `info`, `warn`, `error`                        |
| `--log-format` | `rich`  | `rich` (colored console via [tint][tint], colors auto-disabled when not a TTY), `text` (plain `slog` text), `json` |

```sh
# see every fetch() call and full response detail
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist/global/netflix.js \
  --log-level trace --log-format json
```

- `debug` logs each outgoing `fetch()` call and retry attempt (method, url, retry/timeout config).
- `trace` additionally logs response detail (status code, body size, redirect count) for successful attempts.
- A script's own `println()` output is routed through the same logger (`level=info`, `source=script`), so it also honors `--log-format`.
- Every log line for one script run carries a `script=<id>` attribute for correlation.

[tint]: https://github.com/lmittmann/tint

## Configuration

Every flag on `check` and `serve` (including the global `--log-level`/
`--log-format`) can also be set via a YAML config file or environment
variables, for `systemd`-native and container/cloud-native deployments
where passing everything on the command line is awkward. Precedence,
highest to lowest:

1. command-line flag
2. environment variable (`MP_<FLAG_NAME>`, e.g. `--otel-endpoint` ->
   `MP_OTEL_ENDPOINT`)
3. YAML config file
4. flag default

The config file is picked up in this order:

1. `--config /path/to/config.yaml`, if given explicitly (errors if the
   file doesn't exist)
2. `$XDG_CONFIG_HOME/miaoprobe/config.yaml`, or `~/.config/miaoprobe/config.yaml`
   if `XDG_CONFIG_HOME` isn't set
3. `/etc/miaoprobe/config.yaml` (the conventional path for a `systemd`-managed
   install)

Only the first file found is used (configs are not merged across
locations). YAML keys match flag names verbatim, dashes included:

```yaml
# /etc/miaoprobe/config.yaml
scripts: /opt/miaospeed-scripts/dist
proxy: socks5://127.0.0.1:1080
interval: 5m
log-level: info
otel-endpoint: https://otlp-gateway-prod-xx.grafana.net/otlp
otel-headers: "Authorization=Basic <base64>"
```

Equivalently, for a Docker/Compose deployment:

```sh
docker run -e MP_SCRIPTS=/scripts -e MP_PROXY=socks5://host:1080 \
  -e MP_OTEL_ENDPOINT=https://otlp-gateway-prod-xx.grafana.net/otlp \
  ghcr.io/cloudpassenger/miaoprobe serve
```

## Linting

```sh
make lint   # golangci-lint run ./..., config in .golangci.yml
```

Runs automatically on every push/PR via `.github/workflows/ci.yml`.

## Cross-compilation and releases

Builds are `CGO_ENABLED=0` for every target and produced via
[goreleaser](https://goreleaser.com) (see `.goreleaser.yaml`), covering
linux/darwin/windows across amd64/arm64. Pushing a `vX.Y.Z` tag triggers
`.github/workflows/release.yml`, which builds and publishes archives plus
checksums to a GitHub Release.

To build locally without publishing:

```sh
make release-snapshot   # goreleaser release --snapshot --clean, output in dist/
```

`miaoprobe --version` reports the version/commit/date baked in by goreleaser's
ldflags (`internal/cli.version`, `.commit`, `.date`).

## Compatibility testing

`internal/compat` runs every script from a `miaospeed-scripts` build's
`index.json` through the real engine in direct-egress mode and asserts the
host API contract holds (no host-API incompatibility errors; `handler()`
results parse into `{text, background}`). It does not assert the business
correctness of unlock results, since that depends on the network the test
happens to run on.

## Extended result fields

Beyond the original `miaospeed-scripts` `{text, background}` contract, a
handler may optionally return these miaoprobe-specific fields for richer
display; scripts that only set `text`/`background` are unaffected:

- `status` (string): `"unlocked" | "failed" | "warning" | "unknown" | "na"`.
  When present, this drives the `STATUS` column/`miaoprobe_unlock_status`
  value directly instead of reverse-mapping `background`'s RGB triplet.
- `region` (string): dynamically detected region (e.g. `"US"`), distinct
  from the script's static `regions` config in `index.json`.
- `message` (string): a longer human-readable detail shown under `text`.
- `error` (string): a business-level failure reason (e.g. "connection
  timed out"), shown in the `ERROR` column/field alongside (not replacing)
  Go-level execution errors.
- `extra` (array of `{key, label, value, type, unit}`): free-form
  additional metrics (e.g. an IP quality score), rendered as `label: value`
  in the CLI table and kept fully structured in `--format json`.

```sh
make compat-test
# or, with a different fixtures checkout:
MIAOSPEED_SCRIPTS_DIR=/path/to/miaospeed-scripts/dist make compat-test
```

## Architecture

```
cmd/miaoprobe/     CLI entrypoint
internal/engine/   goja VM: predefined.js (get/safeStringify/safeParse/println),
                   module.exports/handler resolution, timeout via vm.Interrupt
internal/network/  http.Client construction for direct/HTTP-proxy/SOCKS5 egress,
                   plus the fetch() host function and its retry/timeout semantics
internal/script/   loads a single .js file, or a directory's index.json manifest
internal/probe/    glue: run one script once through engine+network
internal/exporter/ OTel instruments, background→status color mapping, polling
internal/otelsetup/ MeterProvider: Prometheus pull reader + optional OTLP push reader
internal/logging/  slog setup: rich/text/json formats, custom TRACE level
internal/cli/      check and serve subcommands
internal/compat/   compatibility test against a miaospeed-scripts build
```

## Out of scope

- No encrypted proxy protocol clients (VMess/Trojan/Shadowsocks/mihomo/...).
- No `netcat` host function (not used by any script contract).
- No `async`/`await`/`Promise` support — `fetch()` is synchronous, matching
  every script in `miaospeed-scripts`.
- No web UI/dashboard; Grafana is the intended downstream consumer.
- No cron scheduling — `serve` only supports a fixed `--interval`.
- No remote script repository fetching or hot reload; point `--scripts` at a
  local checkout and re-run to pick up updates.
