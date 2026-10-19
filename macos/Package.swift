// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "synca-app",
    platforms: [.macOS(.v14)],
    dependencies: [
        .package(path: "../../ns-shell"),
    ],
    targets: [
        // Client of the synca CLI. No UI imports.
        // Zed: crates/client. The Go module is crates/project.
        .target(name: "SyncaKit", path: "Sources/SyncaKit"),
        // Composition root. App binds SyncaCommand. WindowShell is the
        // workspace chrome. Design is the component crate. See ../ARCHITECTURE.md.
        .executableTarget(
            name: "synca-app",
            dependencies: ["SyncaKit", .product(name: "NSShell", package: "ns-shell")],
            path: "Sources/synca-app"
        ),
        // Drives AppModel end to end against the real CLI in a sandboxed HOME.
        .testTarget(
            name: "SyncaAppTests",
            dependencies: ["synca-app", "SyncaKit"],
            path: "Tests/SyncaAppTests"
        ),
        .testTarget(
            name: "SyncaKitTests",
            dependencies: ["SyncaKit"],
            path: "Tests/SyncaKitTests"
        ),
    ]
)
