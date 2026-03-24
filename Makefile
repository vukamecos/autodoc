BINARY     := autodoc
CMD        := ./cmd/autodoc
BUILD_DIR  := ./bin
CONFIG     := autodoc.yaml

.PHONY: build run test lint clean

build:
	go build -o $(BUILD_DIR)/$(BINARY) $(CMD)

run: build
	$(BUILD_DIR)/$(BINARY) -config $(CONFIG)

test:
	go test -race -v -count=1 ./...

lint:
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...

clean:
	rm -rf $(BUILD_DIR)
