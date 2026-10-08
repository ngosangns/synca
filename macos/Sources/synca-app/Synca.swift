import Foundation

struct RunResult {
    let ok: Bool
    let exit: Int32
    let output: String
}

/// Accumulates pipe output from readabilityHandler callbacks.
private final class DataBox: @unchecked Sendable {
    private(set) var data = Data()
    func append(_ chunk: Data) { data.append(chunk) }
}

/// Wraps the `synca` CLI binary — the Rust implementation stays the single
/// source of truth; this app only shells out to it.
enum Synca {

    static var bin: String {
        if let env = ProcessInfo.processInfo.environment["SYNCA_BIN"],
           FileManager.default.isExecutableFile(atPath: env) { return env }
        let home = NSHomeDirectory()
        let local = "\(home)/.local/bin/synca"
        if FileManager.default.isExecutableFile(atPath: local) { return local }
        return "/usr/bin/env"
    }

    static var binArgs: [String] { bin == "/usr/bin/env" ? ["synca"] : [] }

    static var projectDir: String {
        get { UserDefaults.standard.string(forKey: "synca.projectDir")
            ?? FileManager.default.currentDirectoryPath }
        set { UserDefaults.standard.set(newValue, forKey: "synca.projectDir") }
    }

    static var available: Bool {
        (try? run(["--version"], timeout: 5).ok) == true
    }

    static func scopeArgs(_ scope: SyncaScope) -> [String] {
        scope == .project ? ["--scope", "project", "--cwd", projectDir] : ["--scope", "user"]
    }

    @discardableResult
    static func run(_ args: [String], timeout: TimeInterval = 30) throws -> RunResult {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = binArgs + args
        let out = Pipe(), err = Pipe()
        p.standardOutput = out
        p.standardError = err
        try p.run()

        // Drain both pipes concurrently so a chatty process can't deadlock on
        // a full pipe buffer.
        let outBox = DataBox(), errBox = DataBox()
        out.fileHandleForReading.readabilityHandler = { outBox.append($0.availableData) }
        err.fileHandleForReading.readabilityHandler = { errBox.append($0.availableData) }

        // Enforce timeout by terminating the child if it overruns.
        let killer = DispatchWorkItem { if p.isRunning { p.terminate() } }
        DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: killer)
        p.waitUntilExit()
        killer.cancel()
        out.fileHandleForReading.readabilityHandler = nil
        err.fileHandleForReading.readabilityHandler = nil

        let text = String(decoding: outBox.data, as: UTF8.self) + String(decoding: errBox.data, as: UTF8.self)
        return RunResult(ok: p.terminationStatus == 0, exit: p.terminationStatus, output: text)
    }

    /// Read-only JSON list calls (not written to the command log).
    static func listSkills(_ scope: SyncaScope) -> [SkillEntry] {
        guard let r = try? run(["skills", "list"] + scopeArgs(scope) + ["--json"]),
              let data = r.output.trimmingCharacters(in: .whitespacesAndNewlines).data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([SkillEntry].self, from: data)) ?? []
    }

    static func listMcps(_ scope: SyncaScope) -> [McpEntry] {
        guard let r = try? run(["mcp", "list"] + scopeArgs(scope) + ["--json"]),
              let data = r.output.trimmingCharacters(in: .whitespacesAndNewlines).data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([McpEntry].self, from: data)) ?? []
    }

    /// Mutating / dry-run calls: executed off-main and appended to the log.
    @discardableResult
    static func runLogged(_ args: [String], scope: SyncaScope, kind: String) async -> RunResult {
        let result = await Task.detached(priority: .userInitiated) {
            (try? run(args, timeout: 300))
                ?? RunResult(ok: false, exit: -1, output: "failed to spawn \(args.joined(separator: " "))")
        }.value
        let display = (["synca"] + args).map { $0.contains(" ") ? "'\($0)'" : $0 }.joined(separator: " ")
        LogStore.shared.append(command: display, scope: scope.rawValue,
                               status: result.ok ? kind : "error",
                               exit: Int(result.exit), output: result.output.trimmingCharacters(in: .whitespacesAndNewlines))
        return result
    }

    static func syncPlan(scope: SyncaScope, target: String, key: String? = nil) async -> RunResult {
        var args = ["sync", target] + scopeArgs(scope) + ["--dry-run"]
        if let key, target != "all" { args += ["--key", key] }
        return await runLogged(args, scope: scope, kind: "dry-run")
    }

    /// Apply = per-key resolutions for non-skip conflicts, then the main run
    /// with remaining conflicts skipped.
    static func syncApply(scope: SyncaScope, target: String, key: String?, conflicts: [PlanConflict]) async -> [String] {
        var messages: [String] = []
        for c in conflicts where c.policy != "skip" {
            let kind = c.kind == .skill ? "skills" : "mcp"
            let args = ["sync", kind] + scopeArgs(scope) + ["--on-conflict", c.policy, "--key", c.key]
            let r = await runLogged(args, scope: scope, kind: "ok")
            messages.append(r.ok ? "resolved \(c.key) (\(c.policy))" : "FAILED \(c.key)")
        }
        var args = ["sync", target] + scopeArgs(scope) + ["--on-conflict", "skip"]
        if let key, target != "all" { args += ["--key", key] }
        let r = await runLogged(args, scope: scope, kind: "ok")
        messages.append(r.ok ? "sync applied" : "sync failed — see log")
        return messages
    }

    static func installSkill(scope: SyncaScope, source: String, dryRun: Bool) async -> RunResult {
        var args = ["skills", "install", source] + scopeArgs(scope)
        if dryRun { args.append("--dry-run") }
        return await runLogged(args, scope: scope, kind: dryRun ? "dry-run" : "ok")
    }

    static func removeSkill(scope: SyncaScope, key: String, purge: Bool) async -> RunResult {
        var args = ["skills", "remove", key] + scopeArgs(scope)
        if purge { args += ["--purge", "--yes"] }
        return await runLogged(args, scope: scope, kind: "ok")
    }

    static func addMcp(scope: SyncaScope, payload p: Plan.AddMcpPayload, dryRun: Bool) async -> RunResult {
        var args = ["mcp", "add", p.name, "--transport", p.transport] + scopeArgs(scope)
        if let c = p.command, !c.isEmpty { args += ["--command", c] }
        if let u = p.url, !u.isEmpty { args += ["--url", u] }
        args += ["--enabled", p.enabled ? "true" : "false"]
        if dryRun { args.append("--dry-run") }
        return await runLogged(args, scope: scope, kind: dryRun ? "dry-run" : "ok")
    }

    static func removeMcp(scope: SyncaScope, key: String) async -> RunResult {
        await runLogged(["mcp", "remove", key] + scopeArgs(scope), scope: scope, kind: "ok")
    }

    static func updateCheck() async -> RunResult {
        await runLogged(["update", "--check", "--json"], scope: .user, kind: "ok")
    }

    static func updateInstall() async -> RunResult {
        await runLogged(["update", "--json"], scope: .user, kind: "ok")
    }
}
