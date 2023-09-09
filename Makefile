REPO ?= quay.io/epic-gateway
PREFIX ?= eds-server
SUFFIX = ${USER}-dev

TAG ?= ${REPO}/${PREFIX}:${SUFFIX}

##@ Default Goal
.PHONY: help
help: ## Display this help
	@echo "Usage:"
	@echo "  make <goal> [VAR=value ...]"
	@echo
	@echo "Variables"
	@echo "  REPO   The registry part of the Docker tag"
	@echo "  PREFIX Docker tag prefix (useful to set the docker registry)"
	@echo "  SUFFIX Docker tag suffix (the part after ':')"
	@awk 'BEGIN {FS = ":.*##"}; \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  %-15s %s\n", $$1, $$2 } \
		/^##@/ { printf "\n%s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development Goals

.PHONY: test
test: ## Run some code quality checks
	go vet ./...
	go test -race -short ./...

run: ## Run the service using "go run" (KUBECONFIG needs to be set)
	go run ./main.go --debug

image-build:	## Build the container image
	docker build --tag=${TAG} .

image-push:	image-build ## Push the image to the repo
	docker push ${TAG}

image-run: image-build ## Run the image
	docker run --rm --publish=18000:18000 ${TAG}
