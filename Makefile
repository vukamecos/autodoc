BINARY     := autodoc
CMD        := ./cmd/autodoc
BUILD_DIR  := ./bin
CONFIG     := autodoc.yaml

# Build information
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

.PHONY: build run test lint clean

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)

run: build
	$(BUILD_DIR)/$(BINARY) run --config $(CONFIG)

run-once: build
	$(BUILD_DIR)/$(BINARY) once --config $(CONFIG)

test:
	go test -race -v -count=1 ./...

lint:
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...

clean:
	rm -rf $(BUILD_DIR)

validate: build
	$(BUILD_DIR)/$(BINARY) validate --config $(CONFIG)
