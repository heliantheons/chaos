BINARY := build/chaos
GOLANGCI_LINT ?= golangci-lint

.PHONY: build run test lint fmt tidy clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) .

run:
	go run ./main.go

test:
	go test ./...

lint:
	$(GOLANGCI_LINT) run ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

clean:
	rm -rf build
