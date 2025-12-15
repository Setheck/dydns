IMAGE_NAME:="dyndns"

dbuild:
	docker build \
		-t $(IMAGE_NAME):dev \
		.

build:
	mkdir -p bin
	go build -o bin/ ./...

clean:
	rm -rf bin
