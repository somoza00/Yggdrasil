.PHONY: all build test lint fmt clean run

BIN := bin/ygg-api

all: build

build:
	mkdir -p bin
	go build -o $(BIN) ./cmd/api

run: build
	./$(BIN)

test:
	go test ./...

lint:
	go vet ./...
	gofmt -l .

fmt:
	gofmt -w .

clean:
	rm -rf bin