REPO=registry.gitlab.com/acnodal
PREFIX = xds-operator
SUFFIX = ${USER}-dev
SHELL:=/bin/bash

TAG=${REPO}/${PREFIX}:${SUFFIX}
DOCKERFILE=build/package/Dockerfile

ifndef GITLAB_TOKEN
$(error GITLAB_TOKEN not set. It must contain a gitlab Personal Access Token with repo read access)
endif

##@ Default Goal
.PHONY: help
help: ## Display this help
	@echo -e "Usage:\n  make <goal> [VAR=value ...]"
	@echo -e "\nVariables"
	@echo -e "  PREFIX Docker tag prefix (useful to set the docker registry)"
	@echo -e "  SUFFIX Docker tag suffix (the part after ':')"
	@awk 'BEGIN {FS = ":.*##"}; \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  %-15s %s\n", $$1, $$2 } \
		/^##@/ { printf "\n%s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development Goals

.PHONY: check
check: ## Run some code quality checks
	go vet ./...
	golint -set_exit_status ./...
	go test -race -short ./...

run: ## Run the service using "go run" (KUBECONFIG needs to be set)
	go run ./main.go --debug

image:	check ## Build the Docker image
	@docker build --build-arg=GITLAB_TOKEN --file=${DOCKERFILE} --tag=${TAG} .

install:	image ## Push the image to the repo
	docker push ${TAG}

runimage: image ## Run the service using "docker run"
	docker run --rm --publish=18000:18000 ${TAG}
