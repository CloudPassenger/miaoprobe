BINARY := miaoprobe
PKG := ./cmd/miaoprobe
DIST := dist

.PHONY: build build-slim fetch-scripts test vet lint compat-test release-snapshot clean

# build produces the default binary, with the latest miaospeed-scripts
# nightly build embedded (see internal/script/embedded_scripts.go): when
# --scripts isn't given, it's used automatically, and `miaoprobe --version`
# reports its version.
build: fetch-scripts
	CGO_ENABLED=0 go build -tags embedscripts -o $(BINARY) $(PKG)

# fetch-scripts downloads the latest miaospeed-scripts nightly release
# (https://github.com/CloudPassenger/miaospeed-scripts) into
# internal/script/embedded/, for build to bake in via go:embed.
fetch-scripts:
	go run ./tools/fetchscripts

# build-slim produces a binary with no embedded scripts, requiring
# --scripts (or the config/env equivalent) at runtime.
build-slim:
	CGO_ENABLED=0 go build -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...
	gofmt -l .

lint:
	golangci-lint run ./...

# Runs the hard compatibility acceptance test against a local checkout of
# miaospeed-scripts. Override the fixtures directory if it lives elsewhere:
#   make compat-test MIAOSPEED_SCRIPTS_DIR=/path/to/miaospeed-scripts/dist
MIAOSPEED_SCRIPTS_DIR ?= /path/to/miaospeed-scripts/dist
compat-test:
	MIAOSPEED_SCRIPTS_DIR=$(MIAOSPEED_SCRIPTS_DIR) go test ./internal/compat/... -v -run TestCompatibilityAgainstMiaospeedScripts -timeout 20m

release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf $(DIST) $(BINARY)
