SHELL=/bin/bash -o pipefail

.PHONY: tools
tools:
		go install github.com/ory/go-acc github.com/ory/x/tools/listx github.com/sqs/goreturns

# Formats the code
.PHONY: format
format:
		$$(go env GOPATH)/bin/goreturns -w -local github.com/ory $$($$(go env GOPATH)/bin/listx .)

# Runs tests in short mode, without database adapters
.PHONY: docker
docker:
		docker build -f .docker/Dockerfile-build -t oryd/kratos:latest .

.PHONY: lint
lint:
		GO111MODULE=on golangci-lint run -v ./...
