import AppKit

@MainActor enum Busy {
    static var count = 0 {
        didSet { NotificationCenter.default.post(name: .busyChanged, object: count > 0) }
    }
    /// Wrap an async Synca call: progress spinner + log on finish.
    static func run(_ body: @escaping () async -> RunResult, then: @escaping @MainActor (RunResult) -> Void) {
        count += 1
        Task {
            let r = await body()
            await MainActor.run { count -= 1; then(r) }
        }
    }
}

final class MainWindowController: NSWindowController, NSToolbarDelegate {
    let boards = BoardsViewController()
    let detail = DetailViewController()
    let split = NSSplitViewController()

    var scope: SyncaScope = .user {
        didSet { loadInventory() }
    }

    private let scopeTabs = NSSegmentedControl(labels: ["User", "Project"], trackingMode: .selectOne, target: nil, action: nil)
    private let spinner = NSProgressIndicator()
    private let statusField = NSTextField(labelWithString: "")
    private var statusTimer: Timer?
    private let helpPopover = NSPopover()

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1280, height: 800),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false)
        super.init(window: window)
        window.title = "synca"
        window.minSize = NSSize(width: 980, height: 600)
        window.contentMinSize = NSSize(width: 960, height: 560)
        window.titlebarSeparatorStyle = .automatic

        // ---- 2-column split: boards | detail ----
        let boardsItem = NSSplitViewItem(viewController: boards)
        boardsItem.minimumThickness = 220
        boardsItem.preferredThicknessFraction = 0.24
        let detailItem = NSSplitViewItem(viewController: detail)
        detailItem.minimumThickness = 480
        split.splitViewItems = [boardsItem, detailItem]
        split.splitView.isVertical = true // side-by-side panes
        // v2: two panes. The old name stored a three-pane (boards|detail|log) divider.
        split.splitView.autosaveName = "synca.split.v2"
        window.contentView = split.view
        window.setFrameAutosaveName("synca.main")

        buildToolbar(window)

        boards.onSelect = { [weak self] sel in self?.detail.show(selection: sel) }
        boards.onSync = { [weak self] kind in self?.planSync(target: kind.rawValue) }
        boards.onAdd = { [weak self] kind in
            kind == .skills ? self?.detail.showInstallSkill() : self?.detail.showAddMcp()
        }
        detail.actions = self

        NotificationCenter.default.addObserver(self, selector: #selector(reload), name: .reloadInventory, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(busyChanged(_:)), name: .busyChanged, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(showStatus(_:)), name: .flashStatus, object: nil)
        installKeyMonitor()

        loadInventory()
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) not supported") }

    // MARK: - Toolbar

    private enum TID { static let scope = NSToolbarItem.Identifier("scope")
        static let syncAll = NSToolbarItem.Identifier("syncAll")
        static let reload = NSToolbarItem.Identifier("reload")
        static let update = NSToolbarItem.Identifier("update")
        static let help = NSToolbarItem.Identifier("help")
        static let progress = NSToolbarItem.Identifier("progress")
        static let status = NSToolbarItem.Identifier("status") }

    private func buildToolbar(_ window: NSWindow) {
        // v2: scope + actions lead. The old identifier restores a saved
        // layout that parked those items on the trailing side.
        let toolbar = NSToolbar(identifier: "synca.toolbar.v2")
        toolbar.delegate = self
        toolbar.displayMode = .iconOnly
        window.toolbar = toolbar
        window.toolbarStyle = .unifiedCompact
    }

    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        [TID.scope, TID.syncAll, TID.reload, TID.update, TID.help, TID.progress, TID.status, .flexibleSpace, .space]
    }
    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        [TID.scope, TID.syncAll, TID.reload, TID.update, TID.help, .flexibleSpace, TID.status, TID.progress]
    }

    func toolbar(_ toolbar: NSToolbar, itemForItemIdentifier id: NSToolbarItem.Identifier, willBeInsertedIntoToolbar flag: Bool) -> NSToolbarItem? {
        let item = NSToolbarItem(itemIdentifier: id)
        switch id {
        case TID.scope:
            scopeTabs.segmentStyle = .separated
            scopeTabs.selectedSegment = 0
            scopeTabs.target = self
            scopeTabs.action = #selector(scopeChanged(_:))
            item.view = scopeTabs
        case TID.syncAll:
            item.label = "Sync All"
            item.image = NSImage(systemSymbolName: "arrow.triangle.2.circlepath", accessibilityDescription: "Sync all")
            item.target = self; item.action = #selector(syncAllClicked)
        case TID.reload:
            item.label = "Reload"
            item.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: "Reload")
            item.target = self; item.action = #selector(reload)
        case TID.update:
            item.label = "Update"
            item.image = NSImage(systemSymbolName: "arrow.down.circle", accessibilityDescription: "Check for updates")
            item.target = self; item.action = #selector(updateClicked)
        case TID.help:
            item.label = "Help"
            item.image = NSImage(systemSymbolName: "questionmark.circle", accessibilityDescription: "Help")
            item.target = self; item.action = #selector(helpClicked(_:))
        case TID.progress:
            spinner.style = .spinning
            spinner.controlSize = .small
            item.view = spinner
            item.visibilityPriority = .high
        case TID.status:
            statusField.font = .systemFont(ofSize: 11)
            statusField.textColor = .secondaryLabelColor
            statusField.maximumNumberOfLines = 1
            statusField.cell?.truncatesLastVisibleLine = true
            item.view = statusField
            item.minSize = NSSize(width: 80, height: 18)
            item.maxSize = NSSize(width: 420, height: 18)
        default: return nil
        }
        return item
    }

    // MARK: - Actions

    @objc private func scopeChanged(_ sender: NSSegmentedControl) {
        scope = sender.selectedSegment == 0 ? .user : .project
    }

    @objc func reload() { loadInventory() }
    @objc func showDashboard() { detail.showEmpty() }

    func loadInventory() {
        let s = scope
        // Lists fetch on a background thread to keep the UI responsive.
        Busy.count += 1
        Task {
            let (skills, mcps) = await Task.detached(priority: .userInitiated) {
                (Synca.listSkills(s), Synca.listMcps(s))
            }.value
            await MainActor.run {
                Busy.count -= 1
                boards.set(scope: s, skills: skills, mcps: mcps)
            }
        }
    }

    @objc private func syncAllClicked() { planSync(target: "all") }
    @objc private func updateClicked() { detail.showUpdate() }

    func planSync(target: String, key: String? = nil) {
        let s = scope
        Busy.run({ await Synca.syncPlan(scope: s, target: target, key: key) }) { [weak self] r in
            guard let self else { return }
            var plan = Plan(title: "sync \(target)" + (key.map { " · \($0)" } ?? ""),
                            output: r.output.trimmingCharacters(in: .whitespacesAndNewlines), ok: r.ok)
            plan.summary = PlanParser.summary(of: r.output)
            plan.conflicts = PlanParser.conflicts(in: r.output)
            plan.apply = .sync(target: target, key: key)
            self.detail.show(plan: plan, scope: s)
        }
    }

    @objc private func helpClicked(_ sender: NSToolbarItem) {
        if helpPopover.isShown { helpPopover.close(); return }
        let vc = NSViewController()
        let text = NSTextView(frame: NSRect(x: 0, y: 0, width: 300, height: 190))
        text.isEditable = false
        text.font = .systemFont(ofSize: 12)
        text.textContainerInset = NSSize(width: 12, height: 10)
        text.string = """
        Shortcuts
          j / ↓   next item      k / ↑   previous item
          1   user scope         2   project scope
          s   sync everything    r   reload inventory
          u   check updates      ?   this help

        Every action runs the synca CLI underneath.
        Sync always shows a dry-run plan before applying.
        """
        vc.view = text
        helpPopover.contentViewController = vc
        helpPopover.behavior = .transient
        // Image-only toolbar items don't expose a view. Help sits with the
        // other leading actions, so anchor the popover to that side.
        if let content = window?.contentView {
            let rect = NSRect(x: 12, y: content.bounds.maxY - 40,
                              width: 40, height: 30)
            helpPopover.show(relativeTo: rect, of: content, preferredEdge: .maxY)
        }
    }

    @objc private func busyChanged(_ n: Notification) {
        let busy = n.object as? Bool ?? false
        busy ? spinner.startAnimation(nil) : spinner.stopAnimation(nil)
    }

    @objc private func showStatus(_ n: Notification) {
        guard let text = n.object as? String else { return }
        statusField.stringValue = text
        statusTimer?.invalidate()
        statusTimer = .scheduledTimer(withTimeInterval: 6, repeats: false) { [weak self] _ in
            self?.statusField.stringValue = ""
        }
    }

    static func flash(_ text: String) {
        NotificationCenter.default.post(name: .flashStatus, object: text)
    }

    // MARK: - TUI-flavoured hotkeys

    private func installKeyMonitor() {
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self, event.window == self.window else { return event }
            // Don't steal keys while typing in a field/editor.
            if let fr = self.window?.firstResponder,
               fr is NSTextView || fr is NSTextField || fr is NSText {
                if let t = fr as? NSText, t.isEditable { return event }
                if let f = fr as? NSTextField, f.isEditable { return event }
            }
            switch event.charactersIgnoringModifiers {
            case "j": self.boards.moveSelection(1); return nil
            case "k": self.boards.moveSelection(-1); return nil
            case "s": self.planSync(target: "all"); return nil
            case "u": self.detail.showUpdate(); return nil
            case "r": self.reload(); return nil
            case "?": self.helpClicked(NSToolbarItem(itemIdentifier: TID.help)); return nil
            case "1": self.scopeTabs.selectedSegment = 0; self.scope = .user; return nil
            case "2": self.scopeTabs.selectedSegment = 1; self.scope = .project; return nil
            default: return event
            }
        }
    }
}

// MARK: - Detail actions

extension MainWindowController: DetailActions {
    func requestSync(target: String, key: String?) { planSync(target: target, key: key) }

    func applySync(plan: Plan) {
        guard case let .sync(target, key) = plan.apply else { return }
        let s = scope
        Busy.count += 1
        Task {
            let messages = await Synca.syncApply(scope: s, target: target, key: key, conflicts: plan.conflicts)
            await MainActor.run {
                Busy.count -= 1
                Self.flash(messages.joined(separator: " · "))
                self.detail.showEmpty()
                self.loadInventory()
            }
        }
    }

    func applyInstallSkill(plan: Plan) {
        guard case let .installSkill(source) = plan.apply else { return }
        let s = scope
        Busy.run({ await Synca.installSkill(scope: s, source: source, dryRun: false) }) { [weak self] r in
            Self.flash(r.ok ? "Skill installed." : r.statusDetail(fallback: "Install failed."))
            self?.detail.showEmpty()
            self?.loadInventory()
        }
    }

    func applyAddMcp(plan: Plan) {
        guard case let .addMcp(payload) = plan.apply else { return }
        let s = scope
        Busy.run({ await Synca.addMcp(scope: s, payload: payload, dryRun: false) }) { [weak self] r in
            Self.flash(r.ok ? "MCP added." : r.statusDetail(fallback: "Add failed."))
            self?.detail.showEmpty()
            self?.loadInventory()
        }
    }

    func removeSkill(key: String, purge: Bool) {
        let s = scope
        let alert = NSAlert()
        alert.messageText = purge ? "Purge skill '\(key)'?" : "Unlink skill '\(key)'?"
        alert.informativeText = purge
            ? "Removes the canonical copy and all agent links. This cannot be undone."
            : "Removes agent links but keeps the canonical copy."
        alert.addButton(withTitle: purge ? "Purge" : "Unlink")
        alert.addButton(withTitle: "Cancel")
        alert.alertStyle = .warning
        alert.beginSheetModal(for: window!) { [weak self] res in
            guard res == .alertFirstButtonReturn, let self else { return }
            Busy.run({ await Synca.removeSkill(scope: s, key: key, purge: purge) }) { [weak self] r in
                Self.flash(r.ok ? (purge ? "Purged '\(key)'." : "Unlinked '\(key)'.") : r.statusDetail(fallback: "Remove failed."))
                self?.detail.showEmpty()
                self?.loadInventory()
            }
        }
    }

    func removeMcp(key: String) {
        let s = scope
        let alert = NSAlert()
        alert.messageText = "Remove MCP server '\(key)'?"
        alert.addButton(withTitle: "Remove")
        alert.addButton(withTitle: "Cancel")
        alert.alertStyle = .warning
        alert.beginSheetModal(for: window!) { [weak self] res in
            guard res == .alertFirstButtonReturn, let self else { return }
            Busy.run({ await Synca.removeMcp(scope: s, key: key) }) { [weak self] r in
                Self.flash(r.ok ? "Removed '\(key)'." : r.statusDetail(fallback: "Remove failed."))
                self?.detail.showEmpty()
                self?.loadInventory()
            }
        }
    }

    func previewInstallSkill(source: String) {
        let s = scope
        Busy.run({ await Synca.installSkill(scope: s, source: source, dryRun: true) }) { [weak self] r in
            guard let self else { return }
            var plan = Plan(title: "install skill · \(source)",
                            output: r.output.trimmingCharacters(in: .whitespacesAndNewlines), ok: r.ok)
            plan.summary = PlanParser.summary(of: r.output)
            plan.apply = .installSkill(source: source)
            self.detail.show(plan: plan, scope: s)
        }
    }

    func previewAddMcp(payload: Plan.AddMcpPayload) {
        let s = scope
        Busy.run({ await Synca.addMcp(scope: s, payload: payload, dryRun: true) }) { [weak self] r in
            guard let self else { return }
            var plan = Plan(title: "add mcp · \(payload.name)",
                            output: r.output.trimmingCharacters(in: .whitespacesAndNewlines), ok: r.ok)
            plan.summary = PlanParser.summary(of: r.output)
            plan.apply = .addMcp(payload)
            self.detail.show(plan: plan, scope: s)
        }
    }

    func runUpdateCheck() {
        Busy.run({ await Synca.updateCheck() }) { [weak self] r in
            let info = (try? JSONDecoder().decode(UpdateInfo.self,
                     from: Data(r.output.trimmingCharacters(in: .whitespacesAndNewlines).utf8)))
                     ?? UpdateInfo(ok: false, current: nil, latest: nil, update_available: false, url: nil,
                                   message: r.output.trimmingCharacters(in: .whitespacesAndNewlines))
            self?.detail.show(updateResult: info)
        }
    }

    func runUpdateInstall() {
        Busy.run({ await Synca.updateInstall() }) { r in
            let info = try? JSONDecoder().decode(UpdateInfo.self,
                     from: Data(r.output.trimmingCharacters(in: .whitespacesAndNewlines).utf8))
            Self.flash(info?.message ?? (r.ok ? "Update finished." : r.statusDetail(fallback: "Update failed.")))
        }
    }

    var currentScope: SyncaScope { scope }
}
