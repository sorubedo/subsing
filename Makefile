.PHONY: build test vet check-updates update-deps

build:
	go build -trimpath -ldflags="-s -w -buildid=" -o subsing .

test:
	go test ./...

vet:
	go vet ./...

check-updates:
	@echo "Current: $$(grep 'github.com/sagernet/sing-box ' go.mod | awk '{print $$2}')"
	@echo "Upstream:"
	@git ls-remote --tags https://github.com/Sagernet/sing-box.git | sed 's/.*refs\/tags\///' | sort -V | tail -10

update-deps:
	@echo "Usage: make update-deps VERSION=v1.14.0-alpha.XX"
	@[ -n "$(VERSION)" ] || (echo "ERROR: VERSION is required" && exit 1)
	go get github.com/sagernet/sing-box@$(VERSION)
	go mod tidy
	go build ./...
	go vet ./...
	@echo "Updated to $(VERSION) successfully"
