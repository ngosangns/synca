import Foundation
import Testing
@testable import SyncaKit

@Suite struct PlanParserTests {
    @Test func extractsChipsAndConflicts() {
        let out = """
        1. {"kind":"link_skill","skill_key":"a"}
        2. {"kind":"conflict_skill","skill_key":"b"}
        3. {"kind":"conflict_mcp","server":"srv"}
        4. {"kind":"link_skill","skill_key":"c"}
        not json
        """
        let plan = Plan(title: "t", output: out, ok: true, apply: .sync(target: .all, key: nil))
        #expect(plan.chips.first == Plan.Chip(kind: "link_skill", count: 2))
        #expect(plan.conflicts.map(\.key) == ["b", "srv"])
        #expect(plan.conflicts.map(\.kind) == [.skill, .mcp])
    }
}

@Suite struct CLIArgsTests {
    let cli = SyncaCLI(executable: URL(fileURLWithPath: "/bin/echo"))

    @Test func projectScopeRequiresDirectory() {
        #expect(throws: CLIError.self) { try cli.scopeArgs(.project, projectDir: nil) }
        #expect(throws: CLIError.self) { try cli.scopeArgs(.project, projectDir: "") }
    }

    @Test func syncPlanOmitsKeyForAll() throws {
        let a = try cli.syncPlanArgs(.user, dir: nil, target: .all, key: "x")
        #expect(a == ["sync", "all", "--scope", "user", "--dry-run"])
        let b = try cli.syncPlanArgs(.project, dir: "/p", target: .skills, key: "x")
        #expect(b == ["sync", "skills", "--scope", "project", "--cwd", "/p", "--dry-run", "--key", "x"])
    }

    @Test func purgeIsConfirmed() throws {
        let a = try cli.removeSkillArgs(.user, dir: nil, key: "k", purge: true)
        #expect(a.suffix(2) == ["--purge", "--yes"])
    }

    @Test func decodesSnakeCaseInventory() throws {
        let json = #"[{"key":"k","display_name":"K","description":null,"scope":"user","mismatch":true,"presence":[{"agent":"grok","path":"/x","is_symlink":true,"symlink_target":"/y","content_hash":"h"}]}]"#
        let s = try SyncaCLI.decode([SkillEntry].self, from: json)
        #expect(s[0].title == "K" && s[0].mismatch && s[0].presence[0].isSymlink)
    }
}

@Suite struct RunTests {
    @Test func capturesOutputAndExit() async throws {
        let sh = SyncaCLI(executable: URL(fileURLWithPath: "/bin/sh"))
        let r = try await sh.run(["-c", "echo out; echo err 1>&2; exit 3"])
        #expect(!r.ok && r.exit == 3 && r.output.contains("out") && r.output.contains("err"))
    }

    @Test func timeoutKillsProcess() async {
        let sh = SyncaCLI(executable: URL(fileURLWithPath: "/bin/sh"))
        await #expect(throws: CLIError.self) {
            _ = try await sh.run(["-c", "sleep 5"], timeout: .milliseconds(200))
        }
    }

    @Test func cancellationStopsProcess() async {
        let sh = SyncaCLI(executable: URL(fileURLWithPath: "/bin/sh"))
        let t = Task { try await sh.run(["-c", "sleep 5"]) }
        try? await Task.sleep(for: .milliseconds(100))
        t.cancel()
        let start = Date()
        _ = try? await t.value
        #expect(Date().timeIntervalSince(start) < 2)
    }

    @Test func missingBinaryThrowsSpawn() async {
        let bad = SyncaCLI(executable: URL(fileURLWithPath: "/nonexistent/synca"))
        await #expect(throws: CLIError.self) { _ = try await bad.run(["x"]) }
    }
}

@Suite struct LogStoreTests {
    @Test func pagesNewestFirstAndSurvivesRestart() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        let store = LogStore(directory: dir)
        for i in 1...50 { await store.append(command: "c\(i)", scope: "user", status: "ok", exit: 0, output: "") }
        let first = await store.page(limit: 20)
        #expect(first.map(\.id) == Array((31...50).reversed()))
        let older = await store.page(before: 31, limit: 20)
        #expect(older.first?.id == 30 && older.count == 20)

        let reopened = LogStore(directory: dir)
        let e = await reopened.append(command: "next", scope: "user", status: "ok", exit: 0, output: "")
        #expect(e.id == 51)
    }
}

@Suite struct FileTreeTests {
    @Test func foldersFirstAndIgnoresNoise() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let fm = FileManager.default
        try fm.createDirectory(at: root.appendingPathComponent("refs"), withIntermediateDirectories: true)
        try fm.createDirectory(at: root.appendingPathComponent(".git"), withIntermediateDirectories: true)
        try "x".write(to: root.appendingPathComponent("SKILL.md"), atomically: true, encoding: .utf8)
        try "y".write(to: root.appendingPathComponent("refs/a.md"), atomically: true, encoding: .utf8)
        let nodes = FileTree.load(root: root)
        #expect(nodes.map(\.name) == ["refs", "SKILL.md"])
        #expect(nodes[0].children?.map(\.name) == ["a.md"])
        #expect(FileTree.preview(of: root.appendingPathComponent("SKILL.md")) == "x")
    }
}
