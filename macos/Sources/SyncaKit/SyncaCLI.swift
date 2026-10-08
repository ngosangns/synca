import Foundation

public struct RunResult: Sendable {
    public let ok: Bool
    public let exit: Int32
    public let output: String
    public var trimmed: String { output.trimmingCharacters(in: .whitespacesAndNewlines) }
}

public enum CLIError: Error, LocalizedError, Sendable {
    case notFound
    case spawn(String)
    case timedOut(seconds: Int)
    case needsProject
    case decode(String)

    public var errorDescription: String? {
        switch self {
        case .notFound: "The synca CLI was not found. Install it to ~/.local/bin/synca or set SYNCA_BIN."
        case .spawn(let m): "Could not start synca: \(m)"
        case .timedOut(let s): "synca did not finish within \(s)s and was stopped."
        case .needsProject: "Choose a project folder to use the Project scope."
        case .decode(let m): "Unexpected synca output: \(m)"
        }
    }
}

/// Lets cancel / timeout / exit race safely and records why we killed it.
private final class ProcessBox: @unchecked Sendable {
    let process = Process()
    private let lock = NSLock()
    private var _timedOut = false
    var timedOut: Bool { lock.lock(); defer { lock.unlock() }; return _timedOut }
    func terminate(timedOut: Bool = false) {
        lock.lock(); defer { lock.unlock() }
        guard process.isRunning else { return }
        if timedOut { _timedOut = true }
        process.terminate()
    }
}

/// Async wrapper around the `synca` binary. The Rust CLI stays the single
/// source of truth; this type only spawns it — never blocks a thread.
public struct SyncaCLI: Sendable {
    public let executable: URL
    public let baseArgs: [String]
    /// Extra variables for the child process (e.g. `HOME` to sandbox it).
    public let environment: [String: String]

    public init(executable: URL, baseArgs: [String] = [], environment: [String: String] = [:]) {
        self.executable = executable
        self.baseArgs = baseArgs
        self.environment = environment
    }

    /// Same CLI, with `extra` merged into the child environment.
    public func with(environment extra: [String: String]) -> SyncaCLI {
        SyncaCLI(executable: executable, baseArgs: baseArgs, environment: environment.merging(extra) { $1 })
    }

    /// `SYNCA_BIN` → `~/.local/bin/synca` → `synca` on PATH via /usr/bin/env.
    public static func locate(environment: [String: String] = ProcessInfo.processInfo.environment,
                              home: String = NSHomeDirectory()) -> SyncaCLI {
        let fm = FileManager.default
        if let env = environment["SYNCA_BIN"], fm.isExecutableFile(atPath: env) {
            return SyncaCLI(executable: URL(fileURLWithPath: env))
        }
        for candidate in ["\(home)/.local/bin/synca", "/opt/homebrew/bin/synca", "/usr/local/bin/synca"]
        where fm.isExecutableFile(atPath: candidate) {
            return SyncaCLI(executable: URL(fileURLWithPath: candidate))
        }
        return SyncaCLI(executable: URL(fileURLWithPath: "/usr/bin/env"), baseArgs: ["synca"])
    }

    /// GUI apps inherit a minimal PATH; the CLI shells out to git etc.
    private static var childEnvironment: [String: String] {
        var env = ProcessInfo.processInfo.environment
        let extra = ["\(NSHomeDirectory())/.local/bin", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"]
        let current = (env["PATH"] ?? "").split(separator: ":").map(String.init)
        env["PATH"] = (current + extra.filter { !current.contains($0) }).joined(separator: ":")
        return env
    }

    public func scopeArgs(_ scope: SyncaScope, projectDir: String?) throws -> [String] {
        switch scope {
        case .user: return ["--scope", "user"]
        case .project:
            guard let projectDir, !projectDir.isEmpty else { throw CLIError.needsProject }
            return ["--scope", "project", "--cwd", projectDir]
        }
    }

    /// Runs the CLI. Honors Task cancellation and a hard timeout.
    public func run(_ args: [String], timeout: Duration = .seconds(30)) async throws -> RunResult {
        let box = ProcessBox()
        let p = box.process
        p.executableURL = executable
        p.arguments = baseArgs + args
        p.environment = Self.childEnvironment.merging(environment) { $1 }
        let out = Pipe(), err = Pipe()
        p.standardOutput = out
        p.standardError = err
        p.standardInput = FileHandle.nullDevice
        // Each pipe is drained to EOF by its own task. A readabilityHandler can lose the
        // tail of the output when it is cleared while a read is still in flight.
        let outReader = Task.detached { (try? out.fileHandleForReading.readToEnd()) ?? Data() }
        let errReader = Task.detached { (try? err.fileHandleForReading.readToEnd()) ?? Data() }

        let timeoutTask = Task {
            try await Task.sleep(for: timeout)
            box.terminate(timedOut: true)
        }
        defer { timeoutTask.cancel() }

        let status: Int32
        do {
            status = try await withTaskCancellationHandler {
                try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Int32, Error>) in
                    p.terminationHandler = { cont.resume(returning: $0.terminationStatus) }
                    do { try p.run() } catch {
                        // Closing our write ends lets the readers see EOF.
                        try? out.fileHandleForWriting.close()
                        try? err.fileHandleForWriting.close()
                        cont.resume(throwing: CLIError.spawn(error.localizedDescription))
                    }
                }
            } onCancel: { box.terminate() }
        } catch {
            _ = await (outReader.value, errReader.value)
            throw error
        }
        // EOF arrives once the child (and any grandchild holding the pipe) has exited.
        let outData = await outReader.value, errData = await errReader.value

        if Task.isCancelled { throw CancellationError() }
        if box.timedOut { throw CLIError.timedOut(seconds: Int(timeout.components.seconds)) }
        return RunResult(ok: status == 0, exit: status,
                         output: String(decoding: outData, as: UTF8.self) + String(decoding: errData, as: UTF8.self))
    }

    public func version() async -> String? {
        guard let r = try? await run(["--version"], timeout: .seconds(5)), r.ok else { return nil }
        return r.trimmed
    }

    // MARK: Typed reads

    private static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        return d
    }()

    public static func decode<T: Decodable>(_ type: T.Type, from output: String) throws -> T {
        do { return try decoder.decode(T.self, from: Data(output.trimmingCharacters(in: .whitespacesAndNewlines).utf8)) }
        catch { throw CLIError.decode(String(output.prefix(200))) }
    }

    public func skills(_ scope: SyncaScope, projectDir: String?) async throws -> [SkillEntry] {
        let r = try await run(["skills", "list"] + scopeArgs(scope, projectDir: projectDir) + ["--json"])
        guard r.ok else { throw CLIError.decode(r.trimmed) }
        return try Self.decode([SkillEntry].self, from: r.output)
    }

    public func mcps(_ scope: SyncaScope, projectDir: String?) async throws -> [McpEntry] {
        let r = try await run(["mcp", "list"] + scopeArgs(scope, projectDir: projectDir) + ["--json"])
        guard r.ok else { throw CLIError.decode(r.trimmed) }
        return try Self.decode([McpEntry].self, from: r.output)
    }

    // MARK: Command builders (pure, unit-tested)

    public func syncPlanArgs(_ s: SyncaScope, dir: String?, target: SyncTarget, key: String?) throws -> [String] {
        var a = ["sync", target.rawValue] + (try scopeArgs(s, projectDir: dir)) + ["--dry-run"]
        if let key, target != .all { a += ["--key", key] }
        return a
    }

    public func syncApplyArgs(_ s: SyncaScope, dir: String?, target: SyncTarget, key: String?) throws -> [String] {
        var a = ["sync", target.rawValue] + (try scopeArgs(s, projectDir: dir)) + ["--on-conflict", "skip"]
        if let key, target != .all { a += ["--key", key] }
        return a
    }

    public func resolveArgs(_ s: SyncaScope, dir: String?, conflict c: PlanConflict) throws -> [String] {
        ["sync", c.kind == .skill ? "skills" : "mcp"] + (try scopeArgs(s, projectDir: dir))
            + ["--on-conflict", c.policy.rawValue, "--key", c.key]
    }

    public func installArgs(_ s: SyncaScope, dir: String?, source: String, dryRun: Bool) throws -> [String] {
        var a = ["skills", "install", source] + (try scopeArgs(s, projectDir: dir))
        if dryRun { a.append("--dry-run") }
        return a
    }

    public func removeSkillArgs(_ s: SyncaScope, dir: String?, key: String, purge: Bool) throws -> [String] {
        var a = ["skills", "remove", key] + (try scopeArgs(s, projectDir: dir))
        if purge { a += ["--purge", "--yes"] }
        return a
    }

    public func addMcpArgs(_ s: SyncaScope, dir: String?, payload p: AddMcpPayload, dryRun: Bool) throws -> [String] {
        var a = ["mcp", "add", p.name, "--transport", p.transport] + (try scopeArgs(s, projectDir: dir))
        if let c = p.command, !c.isEmpty { a += ["--command", c] }
        if let u = p.url, !u.isEmpty { a += ["--url", u] }
        a += ["--enabled", p.enabled ? "true" : "false"]
        if dryRun { a.append("--dry-run") }
        return a
    }

    public func removeMcpArgs(_ s: SyncaScope, dir: String?, key: String) throws -> [String] {
        ["mcp", "remove", key] + (try scopeArgs(s, projectDir: dir))
    }
}
