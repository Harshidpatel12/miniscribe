.PHONY: build test clean smoke

build:
	mkdir -p bin
	go build -o bin/miniscribe cmd/miniscribe/main.go

test:
	go test -v ./...

clean:
	rm -rf bin/

smoke: build
	./bin/miniscribe models list
	./bin/miniscribe models pull moonshine
