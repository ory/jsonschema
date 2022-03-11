SHELL=/bin/bash -o pipefail

export PATH := .bin:${PATH}

.PHONY: tools
tools:
		GOBIN=$(shell pwd)/.bin/ go install github.com/ory/go-acc golang.org/x/tools/cmd/goimports github.com/jandelgado/gcov2lcov

# Formats the code
.PHONY: format
format: tools
		goimports -w -local github.com/ory *.go $$(go list -f '{{.Dir}}' ./...)

# Runs tests in short mode, without database adapters
.PHONY: docker
docker:
		docker build -f .docker/Dockerfile-build -t oryd/kratos:latest .

.PHONY: lint
lint:
		GO111MODULE=on golangci-lint run -v ./...
