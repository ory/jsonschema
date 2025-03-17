SHELL=/bin/bash -o pipefail

export PATH := .bin:${PATH}

.bin/golangci-lint: Makefile
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b .bin v1.64.8

# Formats the code
.PHONY: format
format:
		go tool goimports -w -local github.com/ory *.go $$(go list -f '{{.Dir}}' ./...)

# Runs tests in short mode, without database adapters
.PHONY: docker
docker:
		docker build -f .docker/Dockerfile-build -t oryd/kratos:latest .

.PHONY: lint
lint: .bin/golangci-lint
		golangci-lint run -v ./...
