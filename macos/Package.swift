// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "synca-app",
    platforms: [.macOS(.v14)],
    targets: [
        // Pure logic: CLI client, models, plan parser, log store, file tree.
        // No UI imports, so it is unit-testable and reusable.
        .target(name: "SyncaKit", path: "Sources/SyncaKit"),
        // SwiftUI app: state (Model), design system (Design), feature views.
        .executableTarget(
            name: "synca-app",
            dependencies: ["SyncaKit"],
            path: "Sources/synca-app"
        ),
        .testTarget(
            name: "SyncaKitTests",
            dependencies: ["SyncaKit"],
            path: "Tests/SyncaKitTests"
        ),
    ]
)
