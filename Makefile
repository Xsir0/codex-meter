SHELL := /bin/sh

VERSION ?= 0.3.1
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test vet race dist release clean fmt check

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/codex-meter ./cmd/codex-meter

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

fmt:
	gofmt -w ./cmd ./internal

check: test vet

dist:
	VERSION="$(VERSION)" COMMIT="$(COMMIT)" DATE="$(DATE)" ./scripts/build-dist.sh

release: dist
	VERSION="$(VERSION)" ./scripts/package-release.sh

clean:
	rm -rf bin dist
