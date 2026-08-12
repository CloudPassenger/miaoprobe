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

Point `--scripts` at a local checkout of
[`miaospeed-scripts`](https://github.com/CloudPassenger/miaospeed-scripts)'s
build output (`dist/`), which contains `index.json` plus one `.js` file per
check, built with `pnpm run build` in that repository. You can also point
`--scripts` at a single `.js` file for ad-hoc testing.

`--scripts` is optional on the **default build**: `make build` (or any
`go build -tags embedscripts`) fetches the latest `miaospeed-scripts`
*nightly* release and bakes it into the binary, so `list`/`check`/`serve`
use it automatically whenever `--scripts` isn't set — directly, via a
config file's `scripts:` key, or via `MP_SCRIPTS`. The **slim build**
(`make build-slim`, a plain `go build` without the tag) does not embed
anything and requires `--scripts` (or the config/env equivalent).
`miaoprobe --version` reports the embedded `miaospeed-scripts` version
when there is one; see [Cross-compilation and
releases](#cross-compilation-and-releases).

## Discovering scripts: `list`

```sh
# every script in a miaospeed-scripts build
./miaoprobe list --scripts /path/to/miaospeed-scripts/dist

# narrow down first, then copy IDs/categories/regions/tags into --filter
# for check/serve
./miaoprobe list --scripts /path/to/miaospeed-scripts/dist --filter "category:media;region:hk" --output.format json
```

`list` prints each script's `id`, `name`, `description`, `category`, `regions`,
`tags`, and `priority` (from `index.json`) so you know what's available before
running `check`/`serve`. It accepts the same `--filter` flag described below,
with one difference: **`list` always shows every script by default**, even if
a `filter:` config file section or `MP_FILTER_*` environment variables are
set — it only filters when `--filter` is passed explicitly on its own command
line. This keeps `list` usable as a pure discovery tool regardless of how a
deployment's `check`/`serve` is configured.

| Flag        | Default | Description                                              |
|-------------|---------|-----------------------------------------------------------|
| `--scripts` | -       | a `.js` file or a directory with `index.json`; defaults to this build's embedded `miaospeed-scripts`, if any (see [Script source](#script-source)) |
| `--filter`  | ``      | script selection, see below (ignores config file/environment unless set here) |
| `--output.format`  | `table` | `table` or `json`                                           |

## Selecting scripts: `--filter`

`check`, `serve`, and `list` all take the same `--filter` flag to select
which scripts run, by any combination of exact ID and category/region/tag
membership (as set in `index.json`). It's a single flag, but its value can
come from three places, in different shapes suited to each:

- **Command line**: a compact `key:v1,v2;key2:v3` string, e.g.
  `--filter "category:media,ai;region:hk,us;id:netflix;mode:exclude"`.
  Recognized keys are `id`, `category`, `region`, `tag` (each a
  comma-separated list), and `mode`.
- **Config file** (YAML): a nested `filter:` section with one list per key:
  ```yaml
  filter:
    mode: include       # or exclude; defaults to include
    category: [media, ai]
    region: [hk, us]
    tag: []
    id: [netflix]
  ```
- **Environment**: one `MP_FILTER_*` variable per key, each a
  comma-separated string — `MP_FILTER_CATEGORY=media,ai`,
  `MP_FILTER_REGION=hk,us`, `MP_FILTER_TAG`, `MP_FILTER_ID`,
  `MP_FILTER_MODE=exclude`.

`id`/`category`/`region`/`tag` are OR'd together: a script matches if it
satisfies *any* of them (e.g. `category:media;region:hk` selects every
`media` script plus every `hk` script, not just their intersection).
`mode` (default `include`) then decides whether matches are kept
(`include`) or dropped (`exclude`, i.e. "run everything except these").
Passing nothing (`--filter ""`, no config section, no environment
variables) matches every script.

Precedence follows the same file → environment → flag order as every other
option (see [Configuration](#configuration)), but as a whole: an explicit
`--filter` on the command line completely replaces whatever the config
file/environment specified — it is not merged field-by-field with them.

## Quick start: one-shot check

```sh
go build -o miaoprobe ./cmd/miaoprobe

# run every script in a miaospeed-scripts build, direct egress
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist

# run a single script file
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist/global/netflix.js

# only media scripts in hk/us, through a SOCKS5 proxy, JSON output
./miaoprobe check \
  --scripts /path/to/miaospeed-scripts/dist \
  --probe.proxy socks5://127.0.0.1:1080 \
  --filter "category:media;region:hk,us" \
  --probe.timeout 30s \
  --output.format json

# a couple of specific scripts by ID (see `miaoprobe list`)
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist --filter "id:netflix,disneyplus"

# everything except AI scripts
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist --filter "category:ai;mode:exclude"
```

`check` flags:

| Flag        | Default | Description                                              |
|-------------|---------|-----------------------------------------------------------|
| `--scripts` | -       | a `.js` file or a directory with `index.json`; defaults to this build's embedded `miaospeed-scripts`, if any (see [Script source](#script-source)) |
| `--probe.proxy`   | ``      | `http://host:port` / `https://host:port` / `socks5://host:port`; empty = direct |
| `--filter`  | ``      | script selection, see [Selecting scripts](#selecting-scripts---filter) |
| `--probe.timeout` | `30s`   | per-script execution timeout                               |
| `--probe.startup.timeout` | `0s` | wait for network readiness before probing; zero disables the gate |
| `--probe.startup.url` | `https://cp.cloudflare.com/generate_204` | URL checked through the configured direct or proxy path |
| `--output.format`  | `table` | `table` (colorized box-drawing table) or `json`             |

`--probe.timeout` is a hard deadline: it cancels a script's in-flight `fetch()` as
well as interrupting its JavaScript. A script's own `fetch()` `timeout`
parameter is additionally clamped to 30s per attempt (with `retry` capped at
10, as before), so no single script can stall a run indefinitely.

When `--probe.startup.timeout` is positive, miaoprobe checks the startup URL
through the same direct, HTTP(S)-proxy, or SOCKS5 path used by script
`fetch()`. It retries once per second, starts probing immediately after a
successful `2xx`/`3xx` response, and exits non-zero if the timeout expires.
This avoids recording an all-failed first cycle while a local proxy or network
interface is still starting.

## Serve mode: Prometheus exporter + OpenTelemetry push

```sh
./miaoprobe serve \
  --scripts /path/to/miaospeed-scripts/dist \
  --probe.proxy http://127.0.0.1:8080 \
  --probe.interval 5m \
  --metrics.listen :9765
```

`serve` flags:

| Flag         | Default | Description                                    |
|--------------|---------|-------------------------------------------------|
| `--scripts`  | -       | a directory with `index.json`; defaults to this build's embedded `miaospeed-scripts`, if any (see [Script source](#script-source)) |
| `--probe.proxy`    | ``      | same as `check`                                 |
| `--filter`   | ``      | same as `check`                                 |
| `--probe.interval` | `5m`    | polling interval (duration format, e.g. `30s`, `5m`) |
| `--probe.timeout`  | `30s`   | per-script execution timeout                    |
| `--probe.startup.timeout` | `0s` | same startup network gate as `check` |
| `--probe.startup.url` | `https://cp.cloudflare.com/generate_204` | readiness URL checked through the probe egress path |
| `--probe.concurrency` | `8`  | how many scripts to probe in parallel           |
| `--metrics.listen`   | `:9765` | address `/metrics` is served on                 |
| `--metrics.runtime` | `true` | also export Go runtime/process metrics (`go_*`, `process_*`) for diagnosing miaoprobe itself |

Scripts are probed in parallel (`--probe.concurrency`), so a cycle takes roughly
`ceil(scripts / concurrency)` slow probes rather than the sum of all of
them. If a cycle still outlives `--probe.interval`, ticks are dropped and the
effective polling rate silently drops — `serve` logs a warning when this
happens, and exposes cycle wall time as `miaoprobe_poll_duration_seconds`.

`serve` also builds on a single OpenTelemetry `MeterProvider` internally, so
the same instruments can be read two ways at once: pulled from `/metrics`
(Prometheus) and/or pushed on an interval to a remote OTLP endpoint — no
external OTel Collector needed. OTLP push flags:

| Flag              | Default          | Description                                    |
|-------------------|------------------|-------------------------------------------------|
| `--otel.endpoint`  | ``               | OTLP endpoint to push to; empty disables push (falls back to `OTEL_EXPORTER_OTLP_*` env vars) |
| `--otel.instance`  | host name        | unique `service.instance.id`; Grafana maps it to the `instance` label |
| `--otel.protocol`  | `http/protobuf`  | `http/protobuf` or `grpc`                       |
| `--otel.headers`   | ``               | comma-separated `key=value` headers sent with every export, e.g. auth |
| `--otel.insecure`  | `false`          | disable TLS (local collectors only)             |
| `--otel.interval`  | `1m`             | how often buffered metrics are pushed           |

For `http/protobuf`, if `--otel.endpoint` has no `/v1/metrics` suffix it is
appended automatically, so a gateway base URL works as-is.

Each pushed instance needs a unique `service.instance.id`; otherwise metrics
from multiple servers have the same series identity and cannot be separated
in Grafana. miaoprobe uses the host name by default. Set a stable, descriptive
value explicitly when deploying containers or when host names may collide:

```sh
# Server A
MP_OTEL_INSTANCE=hk-edge-01 ./miaoprobe serve ...

# Server B
MP_OTEL_INSTANCE=us-edge-01 ./miaoprobe serve ...
```

The standard `OTEL_RESOURCE_ATTRIBUTES=service.instance.id=hk-edge-01` setting
is also honored when `--otel.instance` / `MP_OTEL_INSTANCE` is empty. Grafana
Cloud converts this resource attribute to the Prometheus-compatible `instance`
label, so it can be queried directly:

```promql
miaoprobe_unlock_status{instance="hk-edge-01"}
```

Every OTLP export also carries build metadata as resource attributes:

- `service.version` — the GoReleaser version (`0.1.2`, `nightly-<sha>`, or
  `dev` for an ordinary local build).
- `vcs.ref.head.revision` — the Git commit baked into the binary.
- `miaoprobe.build.date` — the GoReleaser build timestamp.
- `miaoprobe.scripts.embedded.version` — the scripts baked into the binary;
  this does not claim to identify an external directory passed with
  `--scripts`.

These are resource attributes, not labels added to every business metric.
That keeps status series stable across upgrades while still allowing OTLP
backends to filter and group deployments by version.

### Pushing to Grafana Cloud

Grafana Cloud's OTLP gateway accepts metrics directly, no collector
required. Find the endpoint and generate an API token under your stack's
"OpenTelemetry" configuration page, then:

```sh
./miaoprobe serve \
  --scripts /path/to/miaospeed-scripts/dist \
  --otel.endpoint https://otlp-gateway-prod-xx.grafana.net/otlp \
  --otel.headers "Authorization=Basic $(echo -n '<instance-id>:<api-token>' | base64 -w0)" \
  --otel.interval 1m
```

`/metrics` keeps serving locally at the same time, so a local Prometheus
scrape and the Grafana Cloud push can both run off the same poll cycle.

Exposed metrics:

- `miaoprobe_build_info{version,revision,build_date,embedded_scripts_version}`
  — gauge, always `1`, identifying the running binary and its embedded script
  bundle. This is separate from the OTLP resource because the local
  Prometheus exporter intentionally does not expose OTel `target_info`.

- `miaoprobe_unlock_status{id}` — gauge, taken from the script's explicit
  `status` field when set, otherwise mapped from the `background` RGB result
  per `miaospeed-scripts/consts/colors.ts`: `1` unlocked, `0` failed,
  `0.5` warning, `-1` unknown. A script that fails to execute, or reports
  "N/A" (`status: "na"` or background `142,140,142`), reports `-1`.

  This series is **always written on every poll**. Gauges are last-value
  aggregations that the SDK never forgets, so skipping a write would leave
  `/metrics` serving the previous — possibly hours old — value as if it were
  current, and an alert on `== 0` would never fire.
- `miaoprobe_script_info{id,name,category,region,tags}` — gauge, always `1`,
  carrying the static metadata. Metadata lives here rather than on
  `miaoprobe_unlock_status` so that editing a script's `region`/`tags`
  cannot orphan a status series (which would then double-count in
  `sum by (id)` aggregations). Join on `id`:

  ```promql
  miaoprobe_unlock_status * on(id) group_left(name, region, tags) miaoprobe_script_info
  ```
- `miaoprobe_probe_result_info{id,region}` — gauge, always `1`, carrying the
  dynamically detected region from the latest successful probe. This is
  separate from the script configuration's static `region` label.
- `miaoprobe_probe_extra_info{id,region,key,label,value,type,unit}` — gauge,
  always `1`, carrying each entry in the latest successful probe result's
  `extra` list. For example, scripts can expose IP quality with
  `key="ip_quality"` and query its current value remotely through Prometheus
  or an OTLP backend. These dynamic information metrics are observable gauges,
  so changed values replace rather than accumulate alongside old series.
- `miaoprobe_probe_failure_info{id,class,reason}` — observable gauge, always
  `1`, carrying the normalized current failure snapshot. Successful probes
  remove the series instead of leaving stale reasons behind. `class` is one
  of `availability`, `network`, `service`, or `probe`; `reason` is one of
  `region_unavailable`, `timeout`, `dns`, `connection_refused`,
  `connection_reset`, `unreachable`, `tls`, `proxy`, `connection_error`,
  `ip_blocked`, `rate_limited`, `parse_error`, `unexpected_response`,
  `script_error`, or `unknown`. Join on `id` when service metadata is needed.
- `miaoprobe_probe_warning_info{id,class,reason}` — observable gauge, always
  `1`, carrying the normalized current warning snapshot. Non-warning results
  remove the series. `class` is `access`, `restriction`, or `probe`; `reason`
  is `waf_blocked`, `originals_only`, `overseas_only`, `partial_access`, or
  `unknown`. Older scripts that omit `statusReason` safely map to
  `restriction/partial_access`.
- `miaoprobe_last_success_timestamp_seconds{id}` — gauge, Unix time of the
  last run that produced a usable status. Alert on staleness rather than
  trusting the status value alone:

  ```promql
  time() - miaoprobe_last_success_timestamp_seconds > 3 * 300
  ```
- `miaoprobe_check_duration_seconds{id}` — gauge, wall time of the last run.
- `miaoprobe_poll_duration_seconds` — gauge, wall time of the last full
  polling cycle across all scripts.
- `miaoprobe_check_errors_total{id}` — counter, incremented when a script
  *fails to execute* (host API error, script exception, or timeout) — not
  when it reports a business "failed"/"unlock" result.

With `--metrics.runtime` (on by default), Go runtime and process telemetry
is exported alongside these: `go_goroutine_count`, `go_memory_used_bytes`,
`go_memory_gc_goal_bytes` and friends via OpenTelemetry's runtime
instrumentation (so they reach the OTLP push too), plus
`process_open_fds`, `process_resident_memory_bytes` and
`process_cpu_seconds_total` from Prometheus' process collector
(`/metrics` only, since it reads `/proc` directly).

These describe miaoprobe itself rather than any unlock status, but they are
what reveals a slow leak over a long deployment: a steadily climbing
`process_open_fds` or `go_goroutine_count` is the signal to look for.

### Prometheus / Grafana

```yaml
# prometheus.yml
scrape_configs:
  - job_name: miaoprobe
    static_configs:
      - targets: ["localhost:9765"]
```

A ready-made Grafana dashboard and alert rules live in
[`dashboards/`](dashboards):

| File | What it is |
|------|------------|
| `miaoprobe-overview-v2.json` | Streaming unlock overview with structured status history, Category/Region/Tag filtering, IP quality, build and embedded-script versions, runtime health, poll performance, and execution diagnostics |
| `miaoprobe-alerts.yaml` | Prometheus/Mimir alert rules |
| `miaoprobe-alerts-test.yaml` | `promtool` unit tests for those rules |

Import `miaoprobe-overview-v2.json` via **Dashboards → New → Import →
Upload JSON file**, then select the Prometheus data source and MiaoProbe
instance. The dashboard's Category, Region, and Tag variables support multiple
values; comma-separated script metadata such as `region="tw,hk"` and
`tags="stream,video,anime"` is matched as independent values.

The System information section reads MiaoProbe version, source revision, build
time, and embedded `miaospeed-scripts` version from OpenTelemetry
`target_info` resource labels. These fields require a build that reports
`service.version`, `vcs.ref.head.revision`, `miaoprobe.build.date`, and
`miaoprobe.scripts.embedded.version`; older instances display `Not reported`.
Execution diagnostics use `miaoprobe_check_errors_total`, which counts script
execution failures such as exceptions, host API errors, and timeouts rather
than ordinary business-level unavailable results.

Load the alert rules into a self-hosted Prometheus with `rule_files:`, or
into Grafana Cloud with
[`mimirtool`](https://grafana.com/docs/mimir/latest/manage/tools/mimirtool/):

```sh
mimirtool rules load dashboards/miaoprobe-alerts.yaml \
  --address=https://prometheus-prod-xx.grafana.net \
  --id=<instance-id> --key=<api-token>
```

Verify them before shipping — the unit tests cover the stale-gauge and
never-succeeded cases that are easy to get wrong:

```sh
promtool check rules dashboards/miaoprobe-alerts.yaml
promtool test rules dashboards/miaoprobe-alerts-test.yaml
```

The thresholds assume the default `--probe.interval 5m` (staleness fires at three
missed cycles). If you poll on a different interval, scale them to match.

To build panels of your own, remember that metadata lives on
`miaoprobe_script_info` rather than on the status gauge, so filtering by
`region`/`tags` needs the join:

```promql
(miaoprobe_unlock_status == 0)
  * on(job, instance, id) group_left(name, region, tags) miaoprobe_script_info
```

Mapped through a value mapping (`1` → green "Unlocked", `0` → red "Failed",
`0.5` → orange "Warning", `-1` → gray "Unknown"), this gives a status grid
similar to MediaUnlockTest's output.

> Note the parentheses around the comparison: `metric == 0 * on(...) other`
> parses as `metric == (0 * on(...) other)` and is rejected as "vector
> matching only allowed between instant vectors".

## Logging

`--log.level` and `--log.format` are global flags accepted by both `check`
and `serve`. Logs always go to stderr, so `check --output.format json` output on
stdout stays parseable even with verbose logging enabled.

| Flag           | Default | Values                                                        |
|----------------|---------|-----------------------------------------------------------------|
| `--log.level`  | `info`  | `trace`, `debug`, `info`, `warn`, `error`                        |
| `--log.format` | `rich`  | `rich` (colored console via [tint][tint], colors auto-disabled when not a TTY), `text` (plain `slog` text), `json` |

```sh
# see every fetch() call and full response detail
./miaoprobe check --scripts /path/to/miaospeed-scripts/dist/global/netflix.js \
  --log.level trace --log.format json
```

- `debug` logs each outgoing `fetch()` call and retry attempt (method, url, retry/timeout config).
- `trace` additionally logs response detail (status code, body size, redirect count) for successful attempts.
- A script's own `println()` output is routed through the same logger (`level=info`, `source=script`), so it also honors `--log.format`.
- Every log line for one script run carries a `script=<id>` attribute for correlation.

[tint]: https://github.com/lmittmann/tint

## Configuration

Every flag on `list`, `check`, and `serve` (including the global
`--log.level`/`--log.format`) can also be set via a YAML config file or
environment variables, for `systemd`-native and container/cloud-native
deployments where passing everything on the command line is awkward.
Precedence, highest to lowest:

1. command-line flag
2. environment variable (the flag name upper-cased with `.` → `_`, prefixed
   `MP_`; e.g. `--otel.endpoint` -> `MP_OTEL_ENDPOINT`)
3. YAML config file
4. flag default

When miaoprobe runs in Docker or Podman, `127.0.0.1` refers to the container,
not the host. If the host proxy is bound only to `127.0.0.1`, use host
networking (and remove the Compose `ports` mapping). If the proxy listens on a
host-reachable address, use `http://host.docker.internal:PORT` with the
Compose `extra_hosts` mapping, or `http://host.containers.internal:PORT`
under Podman.

Flag names are grouped by concern, and that grouping *is* the config
structure — one name for all three sources:

| Group | Flags | Scope |
|-------|-------|-------|
| `log.*` | `level`, `format` | all commands |
| `output.*` | `format` | `list`, `check` |
| `probe.*` | `proxy`, `timeout`, `startup.timeout`, `startup.url`, `interval`, `concurrency` | `check`, `serve` (`interval` and `concurrency` are serve-only) |
| `metrics.*` | `listen` | `serve` |
| `otel.*` | `instance`, `endpoint`, `protocol`, `headers`, `insecure`, `interval` | `serve` |
| *(ungrouped)* | `config`, `scripts`, `filter` | all commands |

A command ignores keys for flags it doesn't have, so one file can configure
all three subcommands.

`--filter` is the one flag whose shape differs per source: it is a compact
`"key:v1,v2;key2:v3"` string on the command line, but a nested section in
YAML and one variable per field in the environment (`MP_FILTER_CATEGORY`,
...). See [Selecting scripts](#selecting-scripts---filter). It follows the
same file → environment → flag precedence, just resolved as one unit rather
than field-by-field — and on `list` specifically, the config
file/environment layers for `--filter` are skipped entirely unless
`--filter` is also passed on that same command line.

The config file is picked up in this order:

1. `--config /path/to/config.yaml`, if given explicitly (errors if the
   file doesn't exist)
2. `$XDG_CONFIG_HOME/miaoprobe/config.yaml`, or `~/.config/miaoprobe/config.yaml`
   if `XDG_CONFIG_HOME` isn't set
3. `/etc/miaoprobe/config.yaml` (the conventional path for a `systemd`-managed
   install)

Only the first file found is used (configs are not merged across
locations). The YAML nesting mirrors the flag name, so `--log.level`
is `log:` → `level:`:

```yaml
# /etc/miaoprobe/config.yaml
scripts: /opt/miaospeed-scripts/dist
log:
  level: info
probe:
  proxy: socks5://127.0.0.1:1080
  interval: 5m
  startup:
    timeout: 15s
    url: https://cp.cloudflare.com/generate_204
otel:
  instance: hk-edge-01
  endpoint: https://otlp-gateway-prod-xx.grafana.net/otlp
  headers: "Authorization=Basic <base64>"
```

Equivalently, for a Docker/Compose deployment:

```sh
docker run -e MP_SCRIPTS=/scripts -e MP_PROBE_PROXY=socks5://host:1080 \
  -e MP_OTEL_ENDPOINT=https://otlp-gateway-prod-xx.grafana.net/otlp \
  ghcr.io/cloudpassenger/miaoprobe serve
```

See [`config.example.yaml`](config.example.yaml) and
[`.env.example`](.env.example) for every key with its default.

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
checksums to a GitHub Release. Each release ships two variants per
platform: `miaoprobe_*` archives built with the latest `miaospeed-scripts`
nightly baked in (see [Script source](#script-source)), and slim
`miaoprobe_*_slim` archives without it; `.goreleaser.yaml`'s
`before.hooks` fetches the nightly once per release via `go run
./tools/fetchscripts`, and its `miaoprobe` build id compiles with `-tags
embedscripts` while `miaoprobe-slim` doesn't.

To build locally without publishing:

```sh
make build              # fetches the latest nightly and embeds it (see below)
make build-slim         # plain binary, --scripts required
make release-snapshot   # goreleaser release --snapshot --clean, output in dist/
```

`make build` (`fetch-scripts` + `go build -tags embedscripts`) downloads
`index.json`/`scripts.zip` from `miaospeed-scripts`' `nightly` GitHub release
into `internal/script/embedded/` (gitignored) and go:embeds it — see
`tools/fetchscripts` and `internal/script/embedded_scripts.go`.

`miaoprobe --version` reports the version/commit/date baked in by goreleaser's
ldflags (`internal/cli.version`, `.commit`, `.date`), plus, for an embedded
build, the baked-in `miaospeed-scripts` version and a note that it's used by
default.

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
- `statusReason` (string): structured explanation for a warning status:
  `"waf_blocked" | "originals_only" | "overseas_only" | "partial_access"`.
- `region` (string): dynamically detected region (e.g. `"US"`), distinct
  from the script's static `regions` config in `index.json`.
- `message` (string): a longer human-readable detail shown under `text`.
- `error` (string): a business-level failure category returned by the script,
  shown in the `ERROR` column/field alongside (not replacing) Go-level
  execution errors.
- `extra` (array of `{key, label, value, type, unit}`): free-form
  additional metrics (e.g. an IP quality score), rendered as `label: value`
  in the CLI table and kept fully structured in `--output.format json`.

`miaoprobe check --output.format json` additionally emits `failureClass` and
`failureReason` for failed or indeterminate results, and `warningClass` and
`warningReason` for warning results. These fields use the same normalized
values as the corresponding `miaoprobe_probe_*_info` metrics; individual
scripts only need to provide `statusReason` for warning results.

```sh
make compat-test
# or, with a different fixtures checkout:
MIAOSPEED_SCRIPTS_DIR=/path/to/miaospeed-scripts/dist make compat-test
```

## Architecture

```
cmd/miaoprobe/     CLI entrypoint
internal/engine/   goja VM: predefined.js (get/safeStringify/safeParse/println),
                   module.exports/handler resolution, precompiled *goja.Program
                   reuse, timeout via vm.Interrupt
internal/network/  http.Client construction for direct/HTTP-proxy/SOCKS5 egress,
                   plus the fetch() host function and its retry/timeout semantics
                   (context-bounded and clamped, so one script cannot stall a poll)
internal/script/   loads a single .js file, or a directory's index.json manifest;
                   also loads a miaospeed-scripts nightly embedded at build time
                   (-tags embedscripts, see tools/fetchscripts)
internal/probe/    glue: run one script once through engine+network; Runner caches
                   compiled programs, one fresh VM per probe for isolation
internal/exporter/ OTel instruments, background→status color mapping, concurrent
                   polling
internal/otelsetup/ MeterProvider: Prometheus pull reader + optional OTLP push reader
internal/logging/  slog setup: rich/text/json formats, custom TRACE level
internal/cli/      check and serve subcommands
internal/compat/   compatibility test against a miaospeed-scripts build
tools/fetchscripts/ fetches the latest miaospeed-scripts nightly for embedding
```

## Out of scope

- No encrypted proxy protocol clients (VMess/Trojan/Shadowsocks/mihomo/...).
- No `netcat` host function (not used by any script contract).
- No `async`/`await`/`Promise` support — `fetch()` is synchronous, matching
  every script in `miaospeed-scripts`.
- No web UI/dashboard; Grafana is the intended downstream consumer.
- No cron scheduling — `serve` only supports a fixed `--probe.interval`.
- No remote script repository fetching or hot reload *at runtime*; point
  `--scripts` at a local checkout and re-run to pick up updates. (Embedded
  builds fetch a nightly snapshot at *build* time — see [Script
  source](#script-source) — not on every run.)

## License

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE)
(AGPL-3.0-only). The full license text is in the [LICENSE](LICENSE) file.
