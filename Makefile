GO ?= go
MYGO_VERSION ?= v0.6.2

.PHONY: run build package-macos fmt test vet

run:
	CGO_ENABLED=0 $(GO) run .

build:
	mkdir -p build
	CGO_ENABLED=0 $(GO) build -o build/dbl .

package-macos:
	CGO_ENABLED=0 $(GO) run github.com/egoist/mygo/cmd/mygo@$(MYGO_VERSION) build -platform darwin/arm64

fmt:
	$(GO) fmt ./...

test:
	CGO_ENABLED=0 $(GO) test ./...

vet:
	CGO_ENABLED=0 $(GO) vet ./...
