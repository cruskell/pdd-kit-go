GO ?= go

.PHONY: build test check integration
build:
	$(GO) build -o bin/pdd ./cmd/pdd

test:
	$(GO) test ./...

check:
	$(GO) test -race ./...
	$(GO) vet ./...

integration:
	PDD_INTEGRATION=1 $(GO) test ./internal/pdd -run TestGobraIntegration -v -count=1
