BIN      := bin/anitui
PKG      := github.com/EmoFa/anitui
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/internal/buildinfo.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

export CGO_ENABLED := 0

.PHONY: build test test-live lint cross clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/anitui

test:
	go test ./...

# Hits real provider sites; not run in CI.
test-live:
	go test -tags live ./...

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null; then golangci-lint run; else echo "golangci-lint not installed; ran go vet only"; fi

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/anitui-$$os-$$arch$$ext ./cmd/anitui || exit 1; \
	done

clean:
	rm -rf bin dist
