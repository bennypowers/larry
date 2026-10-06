.PHONY: test build install lint clean

MODULE    := bennypowers.dev/larry
BINARY    := larry
VERSION   ?= 0.0.0-dev
LDFLAGS   := -ldflags "-s -w -X main.Version=$(VERSION)"

# Go commands
GO        := go
GOFLAGS   := -trimpath -mod=readonly

# Test coverage
COVERAGE  := -coverprofile=coverage.out

.PHONY: build
build:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY) ./cmd/larry

.PHONY: install
install:
	$(GO) install $(GOFLAGS) $(LDFLAGS) ./cmd/larry

.PHONY: lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run ./...; \
	else \
	  echo "golangci-lint not found; falling back to go vet"; $(GO) vet $(GOFLAGS) ./...; \
	fi

.PHONY: test
test:
	$(GO) test $(GOFLAGS) -v ./...

.PHONY: test-coverage
test-coverage:
	$(GO) test $(GOFLAGS) -v $(COVERAGE) ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: clean
clean:
	rm -f $(BINARY) coverage.out coverage.html
