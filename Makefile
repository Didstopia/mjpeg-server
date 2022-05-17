# NOTE: Possibly only necessary with versions below 1.16?
# export GO111MODULE=on

export PATH := $(GOPATH)/bin:$(PATH)

BINARY_VERSION?=0.0.1
BINARY_OUTPUT?=mjpeg-server
EXTRA_FLAGS?=-mod=vendor

.PHONY: all install build test clean deps upgrade

all: clean deps build

clean:
	go clean
	rm -f $(BINARY_NAME)

deps:
	go build -v $(EXTRA_FLAGS) ./...

build: deps
	go build -v $(EXTRA_FLAGS) -ldflags "-X main.Version=$(BINARY_VERSION)" -o $(BINARY_OUTPUT)

test: build
	go test -v $(EXTRA_FLAGS) -race -coverprofile=coverage.txt -covermode=atomic ./...

install: build
	go install -v $(EXTRA_FLAGS) -ldflags "-X main.Version=$(BINARY_VERSION)"

upgrade: deps
	go get -u ./...
	go mod vendor

tidy: deps
	go mod tidy
