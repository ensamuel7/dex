DEX_VERSION ?= 0.4.3
IMAGE_NAME  := dexlang/dexlang
PREFIX      ?= /usr/local

# Build the compiler from the current source. The version is stamped so a local
# build is distinguishable from an installed release.
build:
	go build -ldflags "-X main.Version=$(DEX_VERSION)-dev" -o dex .

# Install that build over whatever `dex` is on PATH. The editor extension shells
# out to `dex lsp`, so a stale binary there reports errors the current compiler
# does not have — run this after changing the parser, checker, or LSP.
install: build
	install -m 0755 dex $(PREFIX)/bin/dex
	@echo "Installed $$($(PREFIX)/bin/dex version) to $(PREFIX)/bin/dex"

# Everything a compiler change has to pass.
check:
	go test ./...
	go run . test

.PHONY: build install check docker-build docker-publish release


docker-build:
	docker build --build-arg DEX_VERSION=$(DEX_VERSION) -t $(IMAGE_NAME):$(DEX_VERSION) -t $(IMAGE_NAME):latest .
	@echo "Built $(IMAGE_NAME):$(DEX_VERSION)"
	@docker run --rm $(IMAGE_NAME):$(DEX_VERSION) version

docker-publish: docker-build
	@echo "About to push $(IMAGE_NAME):$(DEX_VERSION) and $(IMAGE_NAME):latest"
	@read -p "Continue? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	docker push $(IMAGE_NAME):$(DEX_VERSION)
	docker push $(IMAGE_NAME):latest
	@echo "Published $(IMAGE_NAME):$(DEX_VERSION)"

release:
	@test -n "$(VERSION)" || (echo "Usage: make release VERSION=0.1.4" && exit 1)
	git tag -a v$(VERSION) -m "Release v$(VERSION)"
	git push origin v$(VERSION)
	@echo "Tagged v$(VERSION) — GitHub Actions will build and publish"
