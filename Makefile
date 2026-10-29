DEX_VERSION ?= 0.9.21
IMAGE_NAME  := ensamuel7/dexlang
PREFIX      ?= /usr/local

# Build the compiler from the current source. The version is stamped so a local
# build is distinguishable from an installed release, and the commit so two
# local builds of the same version are distinguishable from each other.
DEX_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null)

build:
	go build -ldflags "-X main.Version=$(DEX_VERSION)-dev -X main.Commit=$(DEX_COMMIT)" -o dex .

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

sanitize:
	DEX_SANITIZE=address,undefined go run . test examples

tsan:
	DEX_SANITIZE=thread go run . test examples

faultinject:
	DEX_FAULT=1 go test ./faultinject/ -run TestFaultInjection -v -count=1 -timeout 180m

strict-c:
	sanitize/strict-c.sh

sanitize-linux:
	docker build -q -t dex-sanitize -f sanitize/Dockerfile .
	docker run --rm -v "$(CURDIR)":/repo:ro dex-sanitize sh -c 'mkdir -p /src && tar -C /repo --exclude=./build -cf - . | tar -C /src -xf - && /src/sanitize/run.sh $(MODES)'

.PHONY: build install check memcheck sanitize tsan faultinject strict-c sanitize-linux docker-build docker-publish release


# A local image for this machine's own architecture, to try before publishing.
docker-build:
	docker build --pull --no-cache --build-arg DEX_VERSION=$(DEX_VERSION) -t $(IMAGE_NAME):$(DEX_VERSION) -t $(IMAGE_NAME):latest .
	@echo "Built $(IMAGE_NAME):$(DEX_VERSION)"
	@docker run --rm $(IMAGE_NAME):$(DEX_VERSION) version

# Every platform in DOCKER_PLATFORMS, built and pushed as one image. Docker's
# default builder cannot build for more than one platform, so a buildx builder
# with the docker-container driver is made the first time. Built on a Mac
# alone, the image was arm64 only and did not run on x86 servers or CI.
DOCKER_PLATFORMS ?= linux/amd64,linux/arm64
DOCKER_BUILDER   ?= dexlang-multi

docker-publish:
	@echo "About to build $(DOCKER_PLATFORMS) and push $(IMAGE_NAME):$(DEX_VERSION) and $(IMAGE_NAME):latest"
	@read -p "Continue? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@docker buildx inspect $(DOCKER_BUILDER) > /dev/null 2>&1 || docker buildx create --name $(DOCKER_BUILDER) --driver docker-container > /dev/null
	docker buildx build --builder $(DOCKER_BUILDER) --platform $(DOCKER_PLATFORMS) --pull --no-cache \
		--build-arg DEX_VERSION=$(DEX_VERSION) -t $(IMAGE_NAME):$(DEX_VERSION) -t $(IMAGE_NAME):latest --push .
	@for p in $$(echo $(DOCKER_PLATFORMS) | tr ',' ' '); do \
		printf '%s: ' $$p; docker run --rm --pull always --platform $$p $(IMAGE_NAME):$(DEX_VERSION) version || exit 1; \
	done
	@echo "Published $(IMAGE_NAME):$(DEX_VERSION) for $(DOCKER_PLATFORMS)"

release:
	@test -n "$(VERSION)" || (echo "Usage: make release VERSION=0.1.4" && exit 1)
	git tag -a v$(VERSION) -m "Release v$(VERSION)"
	git push origin v$(VERSION)
	@echo "Tagged v$(VERSION) — GitHub Actions will build and publish"
