VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/antrvan746/pomodoro-cli/internal/cli.Version=$(VERSION)

.PHONY: build install test

build:
	go build -ldflags "$(LDFLAGS)" -o bin/pomo ./cmd/pomo

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/pomo

test:
	go test ./...
