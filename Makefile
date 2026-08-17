.PHONY: build test

build:
	go build -o bin/romp ./cmd/romp

test:
	go test ./...
