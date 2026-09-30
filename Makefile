APP_NAME := yci
PREFIX   ?= $(HOME)/.local
BIN_DIR  := $(PREFIX)/bin

XDG_CONFIG_HOME ?= $(HOME)/.config
FISH_COMP_DIR   ?= $(XDG_CONFIG_HOME)/fish/completions

# Scratch profile for destructive experiments; override to relocate.
SANDBOX_DIR    ?= /tmp/yci-sandbox

.PHONY: all build install clean fmt fmt-check vet test check sqlc sqlc-check hooks sandbox

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

# Copy the real database into a throwaway profile. Migrations and commands
# that need to be tried destructively run here, never against live data:
# rolling the schema back with a Down migration drops columns and the rows
# written since, whereas restoring a copy of the database predates the
# change entirely. Overwrite the profile from scratch on every run so a
# previous experiment cannot leak into the next.
sandbox:
	@if [ ! -f "$(XDG_CONFIG_HOME)/yci/data.db" ]; then \
		echo "No database at $(XDG_CONFIG_HOME)/yci/data.db — nothing to copy."; exit 1; \
	fi
	@# Go marks its module cache read-only (0555 on directories), so a bare
	@# rm -rf fails with EPERM once a build has populated GOPATH under this
	@# directory. Make the tree writable first, or the sandbox can only ever
	@# be created once per machine.
	@chmod -R u+w $(SANDBOX_DIR) 2>/dev/null || true
	@rm -rf $(SANDBOX_DIR)
	@mkdir -p $(SANDBOX_DIR)/.config/yci
	@# VACUUM INTO rather than cp: it takes a consistent snapshot even if a
	@# write is in flight, and it produces a single file whether the source
	@# is in WAL mode (where a plain cp would miss the -wal sidecar and
	@# silently copy a stale database) or not.
	@sqlite3 "$(XDG_CONFIG_HOME)/yci/data.db" "VACUUM INTO '$(SANDBOX_DIR)/.config/yci/data.db';" \
		|| { echo "Failed to snapshot the database."; exit 1; }
	@if [ -f "$(XDG_CONFIG_HOME)/yci/config.toml" ]; then \
		cp $(XDG_CONFIG_HOME)/yci/config.toml $(SANDBOX_DIR)/.config/yci/config.toml; \
	fi
	@schema=$$(sqlite3 $(SANDBOX_DIR)/.config/yci/data.db "SELECT max(version_id) FROM goose_db_version WHERE is_applied=1;" 2>/dev/null); \
	echo "Sandbox ready (schema $${schema:-?})."
	@echo "Run commands against it with:"
	@echo "  HOME=$(SANDBOX_DIR) XDG_CONFIG_HOME=$(SANDBOX_DIR)/.config ./$(APP_NAME) <command>"
	@echo "The real database at $(XDG_CONFIG_HOME)/yci/data.db is untouched."

hooks:
	install -m 0755 scripts/hooks/pre-commit .git/hooks/pre-commit
