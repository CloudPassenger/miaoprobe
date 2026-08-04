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
| `--format`  | `table` | `table` or `json`                                          |

## Serve mode: Prometheus exporter

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

Exposed metrics:

- `miaoprobe_unlock_status{id,name,region,tags}` — gauge, mapped from the
  script's `background` RGB result per `miaospeed-scripts/consts/colors.ts`:
  `1` unlocked, `0` failed, `0.5` warning, `-1` unknown. Scripts whose result
  is "N/A" (`142,140,142`) are skipped (no series emitted) rather than
  reported as any numeric status.
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

## Cross-compilation

`CGO_ENABLED=0` builds are required and verified for every target below:

```sh
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o miaoprobe-linux-amd64   ./cmd/miaoprobe
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o miaoprobe-linux-arm64   ./cmd/miaoprobe
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o miaoprobe-darwin-arm64  ./cmd/miaoprobe
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o miaoprobe-windows-amd64.exe ./cmd/miaoprobe
```

Or simply `make cross` to build all four into `dist/`.

## Compatibility testing

`internal/compat` runs every script from a `miaospeed-scripts` build's
`index.json` through the real engine in direct-egress mode and asserts the
host API contract holds (no host-API incompatibility errors; `handler()`
results parse into `{text, background}`). It does not assert the business
correctness of unlock results, since that depends on the network the test
happens to run on.

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
internal/exporter/ Prometheus metrics, background→status color mapping, polling
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
