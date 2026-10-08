import Foundation
import SyncaKit
@testable import synca_app

/// Throwaway HOME with fake skills / MCP configs. Every CLI call made through
/// `cli` is confined to it, so destructive operations never touch real data.
final class Sandbox {
    let home: URL
    let project: URL
    let cli: SyncaCLI
    let logDir: URL
    let defaults: UserDefaults
    private let suite: String

    static func realCLI() -> SyncaCLI { .locate() }

    init(file: StaticString = #filePath) throws {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent("synca-sbx-\(UUID().uuidString)", isDirectory: true)
        home = root.appendingPathComponent("home")
        project = root.appendingPathComponent("proj")
        logDir = root.appendingPathComponent("log")
        for d in [home, project, logDir] { try FileManager.default.createDirectory(at: d, withIntermediateDirectories: true) }
        try Self.makeRepo(project)
        // CLI must see a clean HOME (also used by `dirs::home_dir`).
        cli = Self.realCLI().with(environment: ["HOME": home.path])
        suite = "synca.tests.\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suite)!
    }

    deinit {
        defaults.removePersistentDomain(forName: suite)
        try? FileManager.default.removeItem(at: home.deletingLastPathComponent())
    }

    /// The CLI only treats a folder as a project root when it contains `.git`.
    static func makeRepo(_ dir: URL) throws {
        try FileManager.default.createDirectory(at: dir.appendingPathComponent(".git"), withIntermediateDirectories: true)
    }

    @MainActor func makeModel() -> AppModel {
        AppModel(cli: cli, log: LogStore(directory: logDir), defaults: defaults)
    }

    // MARK: Fixtures

    /// Canonical skill under ~/.agents/skills with extra files for the tree.
    @discardableResult
    func skill(_ name: String, in base: URL? = nil, description: String = "fixture skill",
               extra: [String: String] = [:]) throws -> URL {
        let dir = (base ?? home.appendingPathComponent(".agents/skills")).appendingPathComponent(name)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try "---\nname: \(name)\ndescription: \(description)\n---\nbody\n"
            .write(to: dir.appendingPathComponent("SKILL.md"), atomically: true, encoding: .utf8)
        for (rel, text) in extra {
            let f = dir.appendingPathComponent(rel)
            try FileManager.default.createDirectory(at: f.deletingLastPathComponent(), withIntermediateDirectories: true)
            try text.write(to: f, atomically: true, encoding: .utf8)
        }
        return dir
    }

    /// Cursor MCP config (JSON `mcpServers`).
    func cursorMcp(_ servers: [String: [String: Any]]) throws {
        let f = home.appendingPathComponent(".cursor/mcp.json")
        try FileManager.default.createDirectory(at: f.deletingLastPathComponent(), withIntermediateDirectories: true)
        let data = try JSONSerialization.data(withJSONObject: ["mcpServers": servers], options: [.sortedKeys])
        try data.write(to: f)
    }

    func exists(_ rel: String) -> Bool {
        FileManager.default.fileExists(atPath: home.appendingPathComponent(rel).path)
            || (try? FileManager.default.destinationOfSymbolicLink(atPath: home.appendingPathComponent(rel).path)) != nil
    }
}

/// Polls `condition` on the main actor until true or timeout.
@MainActor func eventually(timeout: Duration = .seconds(20), _ condition: @MainActor () -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    while ContinuousClock.now < deadline {
        if condition() { return true }
        try? await Task.sleep(for: .milliseconds(25))
    }
    return condition()
}
