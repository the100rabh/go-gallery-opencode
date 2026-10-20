.PHONY: build fmt vet test run docker docker-build docker-up clean

BINARY := media-gallery

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/media-gallery

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

run: build
	./$(BINARY) -config config.yaml

docker:
	docker build -t media-gallery:latest .

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -f $(BINARY)
