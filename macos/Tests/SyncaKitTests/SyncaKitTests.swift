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

@Suite struct ProjectListTests {
    @Test func addDedupesNormalizesAndActivates() {
        var l = ProjectList()
        l.add("/tmp/a/")
        l.add("/tmp/b")
        l.add("/tmp/a/../a")
        #expect(l.paths == ["/tmp/a", "/tmp/b"])
        #expect(l.active == "/tmp/a")
    }

    @Test func removingActiveFallsToNeighbourThenNil() {
        var l = ProjectList(paths: ["/p/1", "/p/2", "/p/3"], active: "/p/2")
        l.remove("/p/2")
        #expect(l.paths == ["/p/1", "/p/3"] && l.active == "/p/3")
        l.remove("/p/3"); l.remove("/p/1")
        #expect(l.paths.isEmpty && l.active == nil)
    }

    @Test func removingInactiveKeepsActiveAndSelectIgnoresUnknown() {
        var l = ProjectList(paths: ["/p/1", "/p/2"], active: "/p/1")
        l.remove("/p/2")
        #expect(l.active == "/p/1")
        l.select("/nope")
        #expect(l.active == "/p/1")
    }

    @Test func initIncludesActiveEvenIfMissingFromPaths() {
        let l = ProjectList(paths: ["/p/1"], active: "/p/legacy")
        #expect(l.paths == ["/p/1", "/p/legacy"] && l.active == "/p/legacy")
    }
}

@Suite struct TextDiffTests {
    @Test func identicalTextCollapsesToNothingChanged() {
        let rows = TextDiff.diff(old: "a\nb\n", new: "a\nb\n")
        #expect(rows.count == 1 && rows[0].kind == .collapsed && rows[0].hidden == 2)
    }

    @Test func detectsReplacedAddedAndRemovedLinesWithNumbers() {
        let rows = TextDiff.rows(old: TextDiff.lines(of: "one\ntwo\nthree\n"), new: TextDiff.lines(of: "one\nTWO\nthree\nfour\n"))
        let changes = rows.filter { $0.kind != .same }.map { "\($0.kind)|\($0.text)|\($0.oldNo ?? 0)|\($0.newNo ?? 0)" }
        #expect(changes == ["removed|two|2|0", "added|TWO|0|2", "added|four|0|4"])
        #expect(rows.first?.oldNo == 1 && rows.first?.newNo == 1)
    }

    @Test func longUnchangedRunsAreCollapsedKeepingContext() {
        let old = (1...30).map(String.init).joined(separator: "\n")
        let new = old.replacingOccurrences(of: "\n15\n", with: "\nFIFTEEN\n")
        let rows = TextDiff.diff(old: old, new: new, context: 2)
        #expect(rows.first?.kind == .collapsed && rows.first?.hidden == 12)
        #expect(rows.last?.kind == .collapsed)
        #expect(rows.filter { $0.kind == .same }.count == 4)
    }

    @Test func splitAlignsReplacementOnOneRow() {
        let rows = TextDiff.rows(old: ["a", "x", "c"], new: ["a", "y", "c"])
        let split = TextDiff.split(rows)
        #expect(split.count == 3)
        #expect(split[1].left?.text == "x" && split[1].right?.text == "y")
    }

    @Test func splitLeavesGapsForPureAdditions() {
        let split = TextDiff.split(TextDiff.rows(old: ["a"], new: ["a", "b"]))
        #expect(split.last?.left == nil && split.last?.right?.text == "b")
    }

    @Test func emptySidesAndCRLF() {
        #expect(TextDiff.rows(old: [], new: ["x"]).map(\.kind) == [.added])
        #expect(TextDiff.rows(old: ["x"], new: []).map(\.kind) == [.removed])
        #expect(TextDiff.lines(of: "a\r\nb\r\n") == ["a", "b"])
        #expect(TextDiff.lines(of: "") == [])
    }
}

@Suite struct ConflictCompareTests {
    private func mk(_ files: [String: String]) throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        for (rel, text) in files {
            let f = root.appendingPathComponent(rel)
            try FileManager.default.createDirectory(at: f.deletingLastPathComponent(), withIntermediateDirectories: true)
            try text.write(to: f, atomically: true, encoding: .utf8)
        }
        return root
    }

    @Test func skillComparisonListsModifiedAddedRemovedIdentical() throws {
        let a = try mk(["SKILL.md": "---\nname: x\n---\nline1\nline2\n", "refs/a.md": "same", "only-src.txt": "s"])
        let b = try mk(["SKILL.md": "---\nname: x\n---\nline1\nlineX\n", "refs/a.md": "same", "only-tgt.txt": "t"])
        defer { try? FileManager.default.removeItem(at: a); try? FileManager.default.removeItem(at: b) }
        let c = PlanConflict(kind: .skill, key: "x", paths: [a.path, b.path], hashes: ["h1", "h2"])
        let cmp = try #require(ConflictCompare.skill(c, entry: nil))
        #expect(cmp.files.map { "\($0.path):\($0.status)" } == [
            "SKILL.md:modified", "only-tgt.txt:added", "only-src.txt:removed", "refs/a.md:identical"])
        let md = try #require(cmp.files.first)
        #expect(md.additions == 1 && md.deletions == 1)
        #expect(cmp.source.fileCount == 3 && cmp.target.fileCount == 3)
    }

    @Test func targetIsFirstCopyWithADifferentHash() throws {
        let a = try mk(["SKILL.md": "A"]), same = try mk(["SKILL.md": "A"]), diff = try mk(["SKILL.md": "B"]), third = try mk(["SKILL.md": "C"])
        defer { [a, same, diff, third].forEach { try? FileManager.default.removeItem(at: $0) } }
        let c = PlanConflict(kind: .skill, key: "x", paths: [a.path, same.path, diff.path, third.path],
                             hashes: ["ha", "ha", "hb", "hc"])
        let cmp = try #require(ConflictCompare.skill(c, entry: nil))
        #expect(cmp.target.path == diff.path)
        #expect(cmp.otherVersions.map(\.path) == [third.path])
    }

    @Test func binaryAndLargeFilesAreNotDiffed() throws {
        let a = try mk(["SKILL.md": "x"]), b = try mk(["SKILL.md": "x"])
        defer { try? FileManager.default.removeItem(at: a); try? FileManager.default.removeItem(at: b) }
        try Data([0, 1, 2]).write(to: a.appendingPathComponent("bin.dat"))
        try Data([0, 9, 9]).write(to: b.appendingPathComponent("bin.dat"))
        try String(repeating: "x", count: 300_000).write(to: a.appendingPathComponent("big.txt"), atomically: true, encoding: .utf8)
        try "small".write(to: b.appendingPathComponent("big.txt"), atomically: true, encoding: .utf8)
        let cmp = try #require(ConflictCompare.skill(PlanConflict(kind: .skill, key: "x", paths: [a.path, b.path], hashes: ["1", "2"]), entry: nil))
        #expect(cmp.files.first { $0.path == "bin.dat" }?.content == .binary)
        #expect(cmp.files.first { $0.path == "big.txt" }?.content == .tooLarge)
    }

    @Test func mcpComparisonDiffsNormalizedConfig() throws {
        func n(_ args: [String]) -> McpNormalized {
            McpNormalized(transport: "stdio", command: ["npx"], url: nil, args: args, enabled: true, envKeys: ["B", "A"])
        }
        let json = """
        {"key":"srv","scope":"user","mismatch":true,"presence":[
          {"agent":"agents","path":"/hub","fingerprint":"f1","normalized":{"transport":"stdio","command":["npx"],"args":["-y","a"],"enabled":true,"env_keys":["B","A"]}},
          {"agent":"cursor","path":"/cur","fingerprint":"f2","normalized":{"transport":"stdio","command":["npx"],"args":["-y","b"],"enabled":true,"env_keys":["B","A"]}}]}
        """
        let entry = try SyncaCLI.decode(McpEntry.self, from: json)
        let c = PlanConflict(kind: .mcp, key: "srv", fingerprints: ["f1", "f2"])
        let cmp = try #require(ConflictCompare.mcp(c, entry: entry))
        #expect(cmp.source.agent == "agents" && cmp.target.agent == "cursor")
        #expect(cmp.source.summary.contains("env keys: A, B"))
        guard case .text(let rows) = cmp.files[0].content else { Issue.record("expected text"); return }
        #expect(rows.filter { $0.kind == .removed }.map(\.text) == ["  a"])
        #expect(rows.filter { $0.kind == .added }.map(\.text) == ["  b"])
        _ = n([])
    }

    @Test func parserKeepsConflictPathsAndFingerprints() {
        let out = """
        1. {"kind":"conflict_skill","skill_key":"alpha","paths":["/a","/b"],"hashes":["h1","h2"]}
        2. {"kind":"conflict_mcp","server":"srv","fingerprints":["f1","f2"]}
        """
        let plan = Plan(title: "t", output: out, ok: true, apply: .sync(target: .all, key: nil))
        #expect(plan.conflicts[0].paths == ["/a", "/b"] && plan.conflicts[0].hashes == ["h1", "h2"])
        #expect(plan.conflicts[1].fingerprints == ["f1", "f2"])
    }
}
