SHELL := /bin/bash

GO_FILES := $(shell find cmd internal -name '*.go' -type f -print | sort)

.DEFAULT_GOAL := help

.PHONY: build test fmt lint check install help

build:
	go build ./...

test:
	go test -race -count=1 ./...

fmt:
	gofmt -w $(GO_FILES)
	goimports -w $(GO_FILES)

lint:
	command -v goimports >/dev/null || { printf '%s\n' 'Install goimports: go install golang.org/x/tools/cmd/goimports@v0.31.0' >&2; exit 1; }
	command -v typos >/dev/null || { printf '%s\n' 'Install typos: cargo install typos-cli' >&2; exit 1; }
	if gofmt -l $(GO_FILES) | grep -q .; then \
		printf '%s\n' 'Run make fmt; gofmt reported changes.' >&2; \
		exit 1; \
	fi
	if goimports -l $(GO_FILES) | grep -q .; then \
		printf '%s\n' 'Run make fmt; goimports reported changes.' >&2; \
		exit 1; \
	fi
	go vet ./...
	typos README.md skills docs .github

check: lint build test
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...

install:
	go install ./cmd/...

help:
	@printf '%s\n' 'Targets:'
	@printf '%s\n' '  make build    Build every command'
	@printf '%s\n' '  make test     Run tests with the race detector'
	@printf '%s\n' '  make fmt      Format Go source with gofmt and goimports'
	@printf '%s\n' '  make lint     Check formatting, imports, vet, and prose typos'
	@printf '%s\n' '  make check    Run lint, build, tests, and cross-builds'
	@printf '%s\n' '  make install  Install all commands from the local checkout'
