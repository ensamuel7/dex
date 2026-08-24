DEX_VERSION ?= 0.2.9
IMAGE_NAME  := dexlang/dexlang

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
