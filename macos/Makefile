APP = synca.app
# SwiftPM's build system (swift-build vs the legacy driver) chooses the
# products directory. Ask it instead of assuming .build/release.
BIN = $(shell swift build -c release --show-bin-path)/synca-app

all: $(APP)

# Always rebuild — swift tracks its own incremental state.
.PHONY: release
release:
	swift build -c release

$(APP): release Resources/Info.plist
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp $(BIN) $(APP)/Contents/MacOS/synca
	cp Resources/Info.plist $(APP)/Contents/Info.plist
	codesign --force --sign - --identifier com.ngosangns.synca $(APP)

install: $(APP)
	rsync -a --delete $(APP)/ /Applications/$(APP)/

clean:
	rm -rf $(APP) .build

.PHONY: all install clean
