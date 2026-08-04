BINARY := miaoprobe
PKG := ./cmd/miaoprobe
DIST := dist

.PHONY: build test vet fmt compat-test cross clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...
	gofmt -l .

# Runs the hard compatibility acceptance test against a local checkout of
# miaospeed-scripts. Override the fixtures directory if it lives elsewhere:
#   make compat-test MIAOSPEED_SCRIPTS_DIR=/path/to/miaospeed-scripts/dist
MIAOSPEED_SCRIPTS_DIR ?= /path/to/miaospeed-scripts/dist
compat-test:
	MIAOSPEED_SCRIPTS_DIR=$(MIAOSPEED_SCRIPTS_DIR) go test ./internal/compat/... -v -run TestCompatibilityAgainstMiaospeedScripts -timeout 20m

cross:
	mkdir -p $(DIST)
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o $(DIST)/$(BINARY)-linux-amd64 $(PKG)
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o $(DIST)/$(BINARY)-linux-arm64 $(PKG)
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o $(DIST)/$(BINARY)-darwin-arm64 $(PKG)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(DIST)/$(BINARY)-windows-amd64.exe $(PKG)

clean:
	rm -rf $(DIST) $(BINARY)
