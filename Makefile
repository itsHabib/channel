BINARY := channel
GOBIN  ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
  GOBIN = $(shell go env GOPATH)/bin
endif

.PHONY: build install uninstall test test-cover vet lint lint-fix fmt check run clean

build:
	go build -o $(BINARY) ./cmd/channel

install: build
	cp $(BINARY) $(GOBIN)/$(BINARY)
	@echo "Installed $(BINARY) to $(GOBIN)/$(BINARY)"

uninstall:
	rm -f $(GOBIN)/$(BINARY)
	@echo "Removed $(BINARY) from $(GOBIN)"

test:
	go test -race ./...

test-cover:
	go test -race -cover ./...

vet:
	go vet ./...

lint:
	go vet ./...
	go tool golangci-lint run ./...

lint-fix:
	go tool golangci-lint run --fix ./...

fmt:
	gofmt -w .

check: lint test build

run:
	go run ./cmd/channel

clean:
	rm -f $(BINARY) $(BINARY).exe
