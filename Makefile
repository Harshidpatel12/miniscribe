.PHONY: build test clean smoke fmt

build:
	mkdir -p bin
	go build -o bin/miniscribe ./cmd/miniscribe

fmt:
	go fmt ./...

test:
	go fmt ./...
	go test -v ./...

clean:
	rm -rf bin/

smoke: build
	./bin/miniscribe models list
	./bin/miniscribe models pull moonshine
