.PHONY: build test vet

build:
	go build -trimpath -ldflags="-s -w -buildid=" -o subsing .

test:
	go test ./...

vet:
	go vet ./...
