BINARY := coai

.PHONY: build test lint release

build:
	go build -trimpath -ldflags "-s -w" -o $(BINARY) .

test:
	go test ./...

lint:
	go vet ./...

release: test lint
	go build -trimpath -ldflags "-s -w" -o $(BINARY) .
