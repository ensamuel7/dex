DEX_VERSION ?= 0.1.3
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
