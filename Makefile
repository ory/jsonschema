SHELL=/bin/bash -o pipefail

# Formats the code
.PHONY: format
format:
		go tool goimports -w -local github.com/ory *.go $$(go list -f '{{.Dir}}' ./...)

# Runs tests in short mode, without database adapters
.PHONY: docker
docker:
		docker build -f .docker/Dockerfile-build -t oryd/kratos:latest .

.PHONY: lint
lint:
		GO111MODULE=on golangci-lint run -v ./...
