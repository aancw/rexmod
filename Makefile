# Developer shortcuts. Installing doesn't need any of this: see README.md.
.PHONY: build panel preview dist release test lint install uninstall clean

build:            ## rexmod CLI into build/ (what ~/.local/bin/rexmod points to while developing)
	cd cmd/rexmod && go build -o ../../build/rexmod .

panel:            ## panel dylib straight into the install; restart Rex to load it
	panel/build.sh

preview:          ## panel views without Rex: screenshots and checks in build/preview
	xcrun swiftc -module-name RexModPreview -target arm64-apple-macos15.0 -swift-version 5 -import-objc-header panel/Bridge.h \
	  panel/Support.swift panel/Theme.swift panel/FlowIcons.swift panel/Model.swift panel/Views.swift panel/preview/main.swift \
	  -framework AppKit -framework SwiftUI -framework CoreServices -o build/rexmod-preview
	build/rexmod-preview build/preview

dist:             ## prebuilt files install.sh ships (arm64, macOS 15+)
	scripts/build-dist.sh

release:          ## release tarball in build/release
	scripts/package-release.sh

test:
	cd cmd/rexmod && go vet ./... && go test ./...

lint:
	shellcheck -s sh install.sh uninstall.sh scripts/*.sh panel/build.sh
	gofmt -l cmd

install: dist
	./install.sh

uninstall:
	"$(HOME)/Library/Application Support/rexmod/scripts/uninstall.sh"

clean:
	rm -rf build dist
