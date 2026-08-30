IMAGE_NAME:="dydns"
VERSION:=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT:=$(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS:=-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test vet clean dbuild

build:
	mkdir -p bin
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/ ./...

test:
	go test -race -cover ./...

vet:
	go vet ./...

dbuild:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(IMAGE_NAME):dev \
		.

clean:
	rm -rf bin
