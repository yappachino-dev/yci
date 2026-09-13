APP_NAME := yci
PREFIX   ?= $(HOME)/.local
BIN_DIR  := $(PREFIX)/bin

XDG_CONFIG_HOME ?= $(HOME)/.config
FISH_COMP_DIR   ?= $(XDG_CONFIG_HOME)/fish/completions

.PHONY: all build install clean fmt fmt-check vet test check sqlc sqlc-check hooks

all: build

build: sqlc
	@echo "Building $(APP_NAME)..."
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$$(git describe --tags --always --dirty)" -o $(APP_NAME) cmd/cli/main.go

install: build
	@echo "Installing bindary to $(BIN_DIR)..."
	mkdir -p $(BIN_DIR)
	mv $(APP_NAME) $(BIN_DIR)/

	@if [ -d "$(XDG_CONFIG_HOME)/fish" ] || [ -n "$$FISH_VERSION" ]; then \
		echo "Generating Fish completions in $(FISH_COMP_DIR)..."; \
		mkdir -p $(FISH_COMP_DIR); \
		$(BIN_DIR)/$(APP_NAME) completion fish > $(FISH_COMP_DIR)/$(APP_NAME).fish; \
	fi

	@echo "Installation complete"

clean:
	@echo "Cleaning up..."
	rm -f $(APP_NAME)

fmt:
	gofmt -w ./internal ./cmd

fmt-check:
	@out=$$(gofmt -l ./internal ./cmd); if [ -n "$$out" ]; then \
		echo "Files need gofmt:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

test:
	go test -race ./...

sqlc:
	go tool sqlc generate

# Regenerating must be a no-op; a diff means hand-edited or stale
# generated code.
sqlc-check:
	go tool sqlc generate
	git diff --exit-code -- internal/store db/queries

check: fmt-check vet build test sqlc-check

hooks:
	install -m 0755 scripts/hooks/pre-commit .git/hooks/pre-commit
