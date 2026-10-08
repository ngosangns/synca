// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "synca-app",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(
            name: "synca-app",
            path: "Sources/synca-app"
        )
    ]
)
