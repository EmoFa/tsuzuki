BIN      := bin/tsuzuki
PKG      := github.com/EmoFa/tsuzuki
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/internal/buildinfo.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

export CGO_ENABLED := 0

.PHONY: build test test-live lint cross snapshot clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/tsuzuki

test:
	go test ./...

# Hits real provider sites; not run in CI.
test-live:
	go test -tags live ./...

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null; then \
		for os in linux darwin windows; do echo "golangci-lint $$os"; GOOS=$$os golangci-lint run ./... || exit 1; done; \
	else echo "golangci-lint not installed (mise install); ran go vet only"; fi

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/tsuzuki-$$os-$$arch$$ext ./cmd/tsuzuki || exit 1; \
	done

# Builds release archives into dist/ without publishing.
snapshot:
	PACKAGES_GITHUB_TOKEN=unused goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
