BIN_DIR := $(HOME)/.local/share/synca/bin
LINK := $(HOME)/.local/bin/synca
# Single source of truth: the Version const the updater compares against.
VERSION := $(shell sed -n 's/.*Version *= *"\(.*\)"/\1/p' internal/core/update.go)
NAME := synca-$(VERSION)-go

.PHONY: build test install clean

build:
	go build -trimpath -ldflags "-s -w" -o bin/synca ./cmd/synca

test:
	go test ./...

# Install next to the other versioned binaries and point ~/.local/bin/synca at it.
# The -go suffix keeps an older same-version binary available for rollback:
#   ln -sf $(BIN_DIR)/synca-$(VERSION) $(LINK)
install: build
	mkdir -p $(BIN_DIR) $(dir $(LINK))
	cp bin/synca $(BIN_DIR)/$(NAME)
	chmod 755 $(BIN_DIR)/$(NAME)
	ln -sf $(BIN_DIR)/$(NAME) $(LINK)
	@echo "installed $(BIN_DIR)/$(NAME) -> $(LINK)"

clean:
	rm -rf bin
