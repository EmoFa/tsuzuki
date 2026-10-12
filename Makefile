BIN      := bin/tsuzuki
PKG      := github.com/EmoFa/tsuzuki
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/internal/buildinfo.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

export CGO_ENABLED := 0

.PHONY: build test test-live lint cross snapshot clean android-aar android android-install

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

# The Android app (android/) embeds the Go core as an AAR built by gomobile,
# which needs the Android SDK and NDK (ANDROID_HOME, ANDROID_NDK_HOME). It's
# built from the module in android/bind, which holds gomobile, so the main
# module needn't depend on it (gomobile needs a newer Go than tsuzuki).
AAR := android/app/libs/tsuzuki.aar
ANDROID_TARGETS := android/arm64,android/arm,android/amd64

# gomobile runs go in a temporary module, so the real go goes first on PATH.
android-aar:
	mkdir -p $(dir $(AAR))
	cd android/bind && GOBIN=$(CURDIR)/bin/tools go install golang.org/x/mobile/cmd/gobind golang.org/x/mobile/cmd/gomobile
	cd android/bind && PATH="$(CURDIR)/bin/tools:$$(go env GOROOT)/bin:$$PATH" gomobile bind -target=$(ANDROID_TARGETS) -androidapi 26 \
		-javapkg=io.github.emofa.tsuzuki.go -trimpath -ldflags '$(LDFLAGS)' -o $(CURDIR)/$(AAR) github.com/EmoFa/tsuzuki/mobile

# Debug APKs; `make android RELEASE=1` builds signed release APKs instead.
android: android-aar
	cd android && ./gradlew $(if $(RELEASE),assembleRelease,assembleDebug) -PtsuzukiVersion=$(VERSION)

android-install: android
	adb install -r android/app/build/outputs/apk/debug/app-universal-debug.apk

clean:
	rm -rf bin dist android/app/build $(AAR)
