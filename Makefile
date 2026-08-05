BINARY := miaoprobe
PKG := ./cmd/miaoprobe
DIST := dist

.PHONY: build test vet lint compat-test release-snapshot clean

build:
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
