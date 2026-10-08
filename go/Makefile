BIN_DIR := $(HOME)/.local/share/synca/bin
LINK := $(HOME)/.local/bin/synca
VERSION := 0.1.12
NAME := synca-$(VERSION)-go

.PHONY: build test install clean

build:
	go build -trimpath -ldflags "-s -w" -o bin/synca ./cmd/synca

test:
	go test ./...

# Install alongside the Rust release binaries and point ~/.local/bin/synca at it.
# Rollback: ln -sf $(BIN_DIR)/synca-$(VERSION) $(LINK)
install: build
	mkdir -p $(BIN_DIR) $(dir $(LINK))
	cp bin/synca $(BIN_DIR)/$(NAME)
	chmod 755 $(BIN_DIR)/$(NAME)
	ln -sf $(BIN_DIR)/$(NAME) $(LINK)
	@echo "installed $(BIN_DIR)/$(NAME) -> $(LINK)"

clean:
	rm -rf bin
