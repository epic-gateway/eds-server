PROJECT ?= eds-server
REPO ?= registry.gitlab.com/acnodal/epic
PREFIX ?= ${PROJECT}
REGISTRY_IMAGE ?= ${REPO}/${PREFIX}
SUFFIX = ${USER}-dev

TAG ?= ${REGISTRY_IMAGE}:${SUFFIX}

##@ Default Goal
.PHONY: help
help: ## Display this help
	@echo "Usage:"
	@echo "  make <goal> [VAR=value ...]"
	@echo
	@echo "Variables"
	@echo "  PREFIX Docker tag prefix (useful to set the docker registry)"
	@echo "  SUFFIX Docker tag suffix (the part after ':')"
	@awk 'BEGIN {FS = ":.*##"}; \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  %-15s %s\n", $$1, $$2 } \
		/^##@/ { printf "\n%s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development Goals

.PHONY: check
check: ## Run some code quality checks
	go vet ./...
	go test -race -short ./...

run: ## Run the service using "go run" (KUBECONFIG needs to be set)
	go run ./main.go --debug

image:	## Build the Docker image
	docker build --build-arg=GITLAB_USER --build-arg=GITLAB_PASSWORD --tag=${TAG} .

install:	image ## Push the image to the repo
	docker push ${TAG}

runimage: image ## Run the service using "docker run"
	docker run --rm --publish=18000:18000 ${TAG}
