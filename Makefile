DEX_VERSION ?= 0.9.14
IMAGE_NAME  := ensamuel7/dexlang
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

# The example suite under the debug memory checker, which validates every
# retain and release against the object's header and reports anything still
# alive at exit against the .dx statement that allocated it.
#
# A memory error — a double release, a pointer that is not a managed object —
# fails the run on its own. Objects merely still alive at exit are reported but
# tolerated, because a handler registered with http.route or ws.handleMessage
# is owned by a table that outlives main; add DEX_MEM_LEAKS=error once a
# program's expected-live set is known.
memcheck:
	DEX_MEM_CHECK=1 go run . test

.PHONY: build install check memcheck docker-build docker-publish release


docker-build:
	docker build --pull --no-cache --build-arg DEX_VERSION=$(DEX_VERSION) -t $(IMAGE_NAME):$(DEX_VERSION) -t $(IMAGE_NAME):latest .
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
