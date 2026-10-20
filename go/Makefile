BIN_DIR := $(HOME)/.local/share/synca/bin
LINK := $(HOME)/.local/bin/synca
# Single source of truth: the repo-root VERSION file, mirrored by core.Version.
VERSION := $(shell tr -d '[:space:]' < ../VERSION)
NAME := synca-$(VERSION)

.PHONY: build test install clean

build:
	go build -trimpath -ldflags "-s -w" -o bin/synca ./cmd/synca

test:
	go test ./...

# Install next to the other versioned binaries and point ~/.local/bin/synca at it.
# Name matches the updater: ~/.local/share/synca/bin/synca-$(VERSION).
install: build
	mkdir -p $(BIN_DIR) $(dir $(LINK))
	cp bin/synca $(BIN_DIR)/$(NAME)
	chmod 755 $(BIN_DIR)/$(NAME)
	ln -sfn ../share/synca/bin/$(NAME) $(LINK)
	@echo "installed $(BIN_DIR)/$(NAME) -> $(LINK)"

clean:
	rm -rf bin
