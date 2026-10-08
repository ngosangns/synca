import SwiftUI
import Observation
import SyncaKit

enum InventoryState: Equatable {
    case idle, loading, ready, needsProject
    case failed(String)
}

enum Selection: Hashable {
    case skill(String)
    case mcp(String)
}

enum UpdateState {
    case checking
    case result(UpdateInfo)
    case installing
    case failed(String)
}

/// What the detail column shows.
enum DetailRoute {
    case item                  // selected skill/MCP, or the empty state
    case installSkill
    case addMcp
    case working(String)       // an operation without a result view yet
    case plan(Plan)
    case update(UpdateState)
}

struct Operation: Identifiable {
    let id = UUID()
    var title: String
}

struct Toast: Identifiable, Equatable {
    enum Style { case success, error, info }
    let id = UUID()
    let message: String
    let style: Style
}

struct Confirmation: Identifiable {
    let id = UUID()
    let title: String
    let message: String
    let confirmTitle: String
    let destructive: Bool
    let action: @MainActor () -> Void
}

/// Single source of truth for UI state. Views read; user intents call the
/// methods below. Every CLI call goes through `perform`, which registers an
/// `Operation` (drives the status bar / busy states) and logs the command.
@Observable @MainActor
final class AppModel {
    // MARK: Environment
    let cli: SyncaCLI
    private(set) var cliVersion: String?
    private(set) var cliChecked = false
    var cliMissing: Bool { cliChecked && cliVersion == nil }

    // MARK: Scope / project
    var scope: SyncaScope = .user {
        didSet {
            guard scope != oldValue else { return }
            skills = []; mcps = []; selection = nil; route = .item
            reload()
        }
    }
    private(set) var projects: ProjectList
    var projectDir: String? { projects.active }
    /// Presents the project manager sheet.
    var showProjects = false

    // MARK: Inventory
    private(set) var skills: [SkillEntry] = []
    private(set) var mcps: [McpEntry] = []
    private(set) var inventory: InventoryState = .idle
    /// True while a reload runs on top of already-visible data.
    private(set) var isRefreshing = false
    var selection: Selection? {
        didSet { if selection != nil, selection != oldValue { route = .item } }
    }
    var filter = ""

    var selectedSkill: SkillEntry? {
        if case .skill(let k) = selection { return skills.first { $0.key == k } }
        return nil
    }
    var selectedMcp: McpEntry? {
        if case .mcp(let k) = selection { return mcps.first { $0.key == k } }
        return nil
    }
    var filteredSkills: [SkillEntry] {
        filter.isEmpty ? skills : skills.filter {
            $0.key.localizedCaseInsensitiveContains(filter) || ($0.description ?? "").localizedCaseInsensitiveContains(filter)
        }
    }
    var filteredMcps: [McpEntry] {
        filter.isEmpty ? mcps : mcps.filter { $0.key.localizedCaseInsensitiveContains(filter) }
    }

    // MARK: Detail / feedback
    var route: DetailRoute = .item
    private(set) var operations: [Operation] = []
    var isBusy: Bool { !operations.isEmpty }
    var toast: Toast?
    var confirmation: Confirmation?
    var showActivity = true

    // MARK: Activity log
    private(set) var activity: [LogEntry] = []
    private(set) var activityLoading = false
    private(set) var activityHasMore = true

    private let log: LogStore
    private var loadTask: Task<Void, Never>?
    private var toastTask: Task<Void, Never>?

    private let defaults: UserDefaults

    init(cli: SyncaCLI = .locate(), log: LogStore = .shared, defaults: UserDefaults = .standard) {
        self.cli = cli
        self.log = log
        self.defaults = defaults
        self.projects = Self.loadProjects(defaults)
    }

    // MARK: Lifecycle

    func start() {
        reload()
        Task {
            cliVersion = await cli.version()
            cliChecked = true
        }
        loadMoreActivity(reset: true)
    }

    // MARK: Project

    /// Adds folders (the last becomes active).
    func addProjects(_ paths: [String]) {
        guard !paths.isEmpty else { return }
        mutateProjects { list in paths.forEach { list.add($0) } }
    }

    func selectProject(_ path: String) { mutateProjects { $0.select(path) } }
    func removeProject(_ path: String) { mutateProjects { $0.remove(path) } }

    private func mutateProjects(_ change: (inout ProjectList) -> Void) {
        let before = projects.active
        change(&projects)
        Self.saveProjects(projects, defaults)
        if scope == .project, projects.active != before {
            skills = []; mcps = []; selection = nil; route = .item; reload()
        }
    }

    private static let projectsKey = "synca.projects"
    private static let activeProjectKey = "synca.projectDir" // also the pre-manager single-project key

    private static func loadProjects(_ d: UserDefaults) -> ProjectList {
        return ProjectList(paths: d.stringArray(forKey: projectsKey) ?? [],
                           active: d.string(forKey: activeProjectKey))
    }

    private static func saveProjects(_ list: ProjectList, _ d: UserDefaults) {
        d.set(list.paths, forKey: projectsKey)
        if let a = list.active { d.set(a, forKey: activeProjectKey) } else { d.removeObject(forKey: activeProjectKey) }
    }

    // MARK: Inventory

    /// Starts a (re)load, cancelling any load in flight. The operation is
    /// registered synchronously so the UI is busy the instant this returns.
    /// The returned task finishes when the inventory is up to date.
    @discardableResult
    func reload() -> Task<Void, Never> {
        loadTask?.cancel()
        let op = begin("Loading \(scope.title.lowercased()) inventory")
        let task = Task { await loadInventory(op: op) }
        loadTask = task
        return task
    }

    private func loadInventory(op: UUID) async {
        let s = scope, dir = projectDir
        defer { end(op) }
        if s == .project, (dir ?? "").isEmpty {
            skills = []; mcps = []; inventory = .needsProject; isRefreshing = false; return
        }
        if skills.isEmpty && mcps.isEmpty { inventory = .loading }
        isRefreshing = true
        defer { if !Task.isCancelled { isRefreshing = false } }
        do {
            async let sk = cli.skills(s, projectDir: dir)
            async let mc = cli.mcps(s, projectDir: dir)
            let (newSkills, newMcps) = try await (sk, mc)
            guard !Task.isCancelled, s == scope else { return }
            skills = newSkills; mcps = newMcps; inventory = .ready
            if let sel = selection, !contains(sel) { selection = nil }
        } catch is CancellationError {
        } catch {
            guard !Task.isCancelled, s == scope else { return }
            inventory = .failed(error.localizedDescription)
        }
    }

    private func contains(_ sel: Selection) -> Bool {
        switch sel {
        case .skill(let k): skills.contains { $0.key == k }
        case .mcp(let k): mcps.contains { $0.key == k }
        }
    }

    // MARK: Operations & feedback

    private func begin(_ title: String) -> UUID {
        let op = Operation(title: title)
        operations.append(op)
        return op.id
    }
    private func end(_ id: UUID) { operations.removeAll { $0.id == id } }

    func notify(_ message: String, _ style: Toast.Style = .info) {
        let t = Toast(message: message, style: style)
        toast = t
        toastTask?.cancel()
        toastTask = Task {
            try? await Task.sleep(for: .seconds(style == .error ? 7 : 4))
            if toast?.id == t.id { toast = nil }
        }
    }

    func dismissToast() { toast = nil }

    private func fail(_ error: Error) { notify(error.localizedDescription, .error) }

    /// Runs a CLI invocation, records it in the activity log.
    @discardableResult
    private func runLogged(_ args: [String], kind: String, timeout: Duration = .seconds(300)) async throws -> RunResult {
        let display = (["synca"] + args).map { $0.contains(" ") ? "'\($0)'" : $0 }.joined(separator: " ")
        let scopeName = scope.rawValue
        do {
            let r = try await cli.run(args, timeout: timeout)
            let e = await log.append(command: display, scope: scopeName, status: r.ok ? kind : "error",
                                     exit: Int(r.exit), output: r.trimmed)
            activity.insert(e, at: 0)
            return r
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            let e = await log.append(command: display, scope: scopeName, status: "error", exit: -1,
                                     output: error.localizedDescription)
            activity.insert(e, at: 0)
            throw error
        }
    }

    // MARK: Sync

    func planSync(target: SyncTarget, key: String? = nil) {
        guard !cliMissing else { return notify(CLIError.notFound.localizedDescription, .error) }
        let title = "sync \(target.rawValue)" + (key.map { " · \($0)" } ?? "")
        route = .working("Planning \(title)…")
        let (s, dir) = (scope, projectDir)
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Planning \(title)")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(cli.syncPlanArgs(s, dir: dir, target: target, key: key), kind: "dry-run")
                route = .plan(Plan(title: title, output: r.trimmed, ok: r.ok, apply: .sync(target: target, key: key)))
            } catch is CancellationError {
            } catch { route = .item; fail(error) }
        }
    }

    func previewInstallSkill(source: String) {
        let (s, dir) = (scope, projectDir)
        route = .working("Previewing install…")
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Previewing install")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(cli.installArgs(s, dir: dir, source: source, dryRun: true), kind: "dry-run")
                route = .plan(Plan(title: "install skill · \(source)", output: r.trimmed, ok: r.ok,
                                   apply: .installSkill(source: source)))
            } catch is CancellationError {
            } catch { route = .installSkill; fail(error) }
        }
    }

    func previewAddMcp(_ payload: AddMcpPayload) {
        let (s, dir) = (scope, projectDir)
        route = .working("Previewing add…")
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Previewing add")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(cli.addMcpArgs(s, dir: dir, payload: payload, dryRun: true), kind: "dry-run")
                route = .plan(Plan(title: "add mcp · \(payload.name)", output: r.trimmed, ok: r.ok,
                                   apply: .addMcp(payload)))
            } catch is CancellationError {
            } catch { route = .addMcp; fail(error) }
        }
    }

    /// Applies a previewed plan. Per-key conflict resolutions run first, then
    /// the main run with the remaining conflicts skipped (never silent overwrite).
    func apply(_ plan: Plan) {
        let (s, dir) = (scope, projectDir)
        let previous = route
        route = .working("Applying \(plan.title)…")
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Applying \(plan.title)")
        Task {
            defer { end(op) }
            do {
                var failures = 0
                switch plan.apply {
                case .sync(let target, let key):
                    for c in plan.conflicts where c.policy != .skip {
                        let r = try await runLogged(cli.resolveArgs(s, dir: dir, conflict: c), kind: "ok")
                        if !r.ok { failures += 1 }
                    }
                    let r = try await runLogged(cli.syncApplyArgs(s, dir: dir, target: target, key: key), kind: "ok")
                    if !r.ok { failures += 1 }
                case .installSkill(let source):
                    let r = try await runLogged(cli.installArgs(s, dir: dir, source: source, dryRun: false), kind: "ok")
                    if !r.ok { failures += 1 }
                case .addMcp(let payload):
                    let r = try await runLogged(cli.addMcpArgs(s, dir: dir, payload: payload, dryRun: false), kind: "ok")
                    if !r.ok { failures += 1 }
                }
                route = .item
                notify(failures == 0 ? "Applied: \(plan.title)" : "\(failures) step(s) failed — see Activity",
                       failures == 0 ? .success : .error)
                reload()
            } catch is CancellationError {
            } catch { route = previous; fail(error) }
        }
    }

    // MARK: Remove

    func confirmRemoveSkill(_ key: String, purge: Bool) {
        confirmation = Confirmation(
            title: purge ? "Purge skill “\(key)”?" : "Unlink skill “\(key)”?",
            message: purge ? "Removes the canonical copy and every agent link. This cannot be undone."
                           : "Removes agent links but keeps the canonical copy.",
            confirmTitle: purge ? "Purge" : "Unlink", destructive: true
        ) { [weak self] in self?.removeSkill(key, purge: purge) }
    }

    func confirmRemoveMcp(_ key: String) {
        confirmation = Confirmation(
            title: "Remove MCP server “\(key)”?",
            message: "Removes it from every agent config in this scope.",
            confirmTitle: "Remove", destructive: true
        ) { [weak self] in self?.removeMcp(key) }
    }

    private func removeSkill(_ key: String, purge: Bool) {
        let (s, dir) = (scope, projectDir)
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin(purge ? "Purging \(key)" : "Unlinking \(key)")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(cli.removeSkillArgs(s, dir: dir, key: key, purge: purge), kind: "ok")
                // The CLI exits 0 with "nothing to remove" when it found no paths; never report that as success.
                let removedNothing = r.ok && r.output.contains("nothing to remove")
                if removedNothing {
                    notify("Nothing was removed for “\(key)” — see Activity", .error)
                } else {
                    notify(r.ok ? (purge ? "Purged “\(key)”" : "Unlinked “\(key)”") : "Remove failed — see Activity",
                           r.ok ? .success : .error)
                }
                if r.ok, !removedNothing, selection == .skill(key) { selection = nil }
                reload()
            } catch is CancellationError {
            } catch { fail(error) }
        }
    }

    private func removeMcp(_ key: String) {
        let (s, dir) = (scope, projectDir)
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Removing \(key)")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(cli.removeMcpArgs(s, dir: dir, key: key), kind: "ok")
                let removedNothing = r.ok && r.output.contains("nothing to remove")
                if removedNothing {
                    notify("Nothing was removed for “\(key)” — see Activity", .error)
                } else {
                    notify(r.ok ? "Removed “\(key)”" : "Remove failed — see Activity", r.ok ? .success : .error)
                }
                if r.ok, !removedNothing, selection == .mcp(key) { selection = nil }
                reload()
            } catch is CancellationError {
            } catch { fail(error) }
        }
    }

    // MARK: Update

    func checkUpdate() {
        route = .update(.checking)
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Checking for updates")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(["update", "--check", "--json"], kind: "ok", timeout: .seconds(60))
                let info = (try? SyncaCLI.decode(UpdateInfo.self, from: r.output))
                    ?? UpdateInfo(ok: false, current: nil, latest: nil, updateAvailable: false, url: nil,
                                  message: r.trimmed)
                route = .update(.result(info))
            } catch is CancellationError {
            } catch { route = .update(.failed(error.localizedDescription)) }
        }
    }

    func installUpdate() {
        route = .update(.installing)
        // Register synchronously so the UI is busy (buttons disabled) before the task starts.
        let op = begin("Installing update")
        Task {
            defer { end(op) }
            do {
                let r = try await runLogged(["update", "--json"], kind: "ok", timeout: .seconds(300))
                let info = try? SyncaCLI.decode(UpdateInfo.self, from: r.output)
                notify(info?.message ?? (r.ok ? "Update finished" : "Update failed — see Activity"),
                       r.ok ? .success : .error)
                if r.ok { cliVersion = await cli.version() }
                route = .item
            } catch is CancellationError {
            } catch { route = .update(.failed(error.localizedDescription)) }
        }
    }

    // MARK: Activity

    func loadMoreActivity(reset: Bool = false) {
        guard !activityLoading, reset || activityHasMore else { return }
        activityLoading = true
        let before = reset ? nil : activity.last?.id
        Task {
            let page = await log.page(before: before, limit: 40)
            if reset { activity = page } else { activity.append(contentsOf: page) }
            activityHasMore = page.count == 40
            activityLoading = false
        }
    }

    func clearActivity() {
        Task {
            await log.clear()
            activity = []
            activityHasMore = false
        }
    }
}
