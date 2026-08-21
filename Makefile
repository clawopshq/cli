BINARY := clawops
PKG    := ./cmd/clawops

.PHONY: build test lint fmt run clean

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./...

lint:
	go vet ./...
	gofmt -l . | tee /dev/stderr | (! read)

fmt:
	gofmt -w .

run:
	go run $(PKG) $(ARGS)

clean:
	rm -f $(BINARY)
	rm -rf dist/
