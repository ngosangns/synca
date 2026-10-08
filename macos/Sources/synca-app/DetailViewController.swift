import AppKit
import ObjectiveC

@MainActor protocol DetailActions: AnyObject {
    var currentScope: SyncaScope { get }
    func requestSync(target: String, key: String?)
    func applySync(plan: Plan)
    func applyInstallSkill(plan: Plan)
    func applyAddMcp(plan: Plan)
    func previewInstallSkill(source: String)
    func previewAddMcp(payload: Plan.AddMcpPayload)
    func removeSkill(key: String, purge: Bool)
    func removeMcp(key: String)
    func runUpdateCheck()
    func runUpdateInstall()
}

final class DetailViewController: NSViewController {
    weak var actions: DetailActions?

    private let scroll = NSScrollView()
    /// Flipped so arranged subviews lay out top-to-bottom and the scroll
    /// view's initial position is the top of the document.
    private final class FlippedStack: NSStackView {
        override var isFlipped: Bool { true }
    }

    private let stack = FlippedStack()
    private let centerHost = NSView()
    private var currentPlan: Plan?
    private var conflicts: [PlanConflict] = []

    // form state
    private let installField = NSTextField()
    private let mcpName = NSTextField()
    private let mcpTransport = NSPopUpButton()
    private let mcpCommand = NSTextField()
    private let mcpUrl = NSTextField()
    private let mcpEnabled = NSButton(checkboxWithTitle: "enabled", target: nil, action: nil)

    override func loadView() {
        view = NSView()

        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.automaticallyAdjustsContentInsets = false
        scroll.translatesAutoresizingMaskIntoConstraints = false
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = Theme.gap
        stack.edgeInsets = NSEdgeInsets(top: Theme.pad, left: Theme.pad,
                                       bottom: Theme.pad, right: Theme.pad)
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.documentView = stack

        centerHost.isHidden = true
        centerHost.translatesAutoresizingMaskIntoConstraints = false

        view.addSubview(scroll)
        view.addSubview(centerHost)
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: view.topAnchor),
            scroll.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            stack.widthAnchor.constraint(equalTo: scroll.widthAnchor),
            // Keep the document at least as tall as the clip so content
            // stays top-anchored instead of bottom-aligning.
            stack.heightAnchor.constraint(greaterThanOrEqualTo: scroll.heightAnchor),
            centerHost.topAnchor.constraint(equalTo: view.topAnchor),
            centerHost.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            centerHost.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            centerHost.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
        showEmpty()
    }

    private func reset() {
        stack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        centerHost.subviews.forEach { $0.removeFromSuperview() }
        centerHost.isHidden = true
        scroll.isHidden = false
        currentPlan = nil
    }

    /// Appends a stretchy spacer so the stack fills the scroll view when
    /// content is short, keeping real content top-anchored.
    private func endStack() {
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .vertical)
        stack.addArrangedSubview(spacer)
        fadeIn(stack)
    }

    private func fadeIn(_ v: NSView) {
        v.alphaValue = 0
        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = 0.2
            ctx.allowsImplicitAnimation = true
            v.animator().alphaValue = 1
        }
    }

    // MARK: - States

    func showEmpty() {
        reset()
        scroll.isHidden = true
        centerHost.isHidden = false

        let icon = Theme.icon("square.grid.2x2", size: 44, color: .quaternaryLabelColor)
        let label = NSTextField(labelWithString: "Select a skill or MCP server")
        label.font = .systemFont(ofSize: 15, weight: .medium)
        label.textColor = .secondaryLabelColor
        let hint = NSTextField(wrappingLabelWithString:
            "j/k to move · 1/2 switch scope · s sync all\nr reload · u update · ? help")
        hint.font = Theme.captionFont
        hint.textColor = .tertiaryLabelColor
        hint.alignment = .center

        let col = NSStackView(views: [icon, label, hint])
        col.orientation = .vertical
        col.alignment = .centerX
        col.spacing = 10
        col.translatesAutoresizingMaskIntoConstraints = false
        centerHost.addSubview(col)
        // Required center constraints would make this column's intrinsic
        // height the window's fitting size — keep them non-required.
        let cx = col.centerXAnchor.constraint(equalTo: centerHost.centerXAnchor)
        let cy = col.centerYAnchor.constraint(equalTo: centerHost.centerYAnchor)
        cx.priority = .defaultLow
        cy.priority = .defaultLow
        NSLayoutConstraint.activate([
            cx, cy,
            col.widthAnchor.constraint(lessThanOrEqualTo: centerHost.widthAnchor, constant: -40),
        ])
        fadeIn(centerHost)
    }

    func show(selection: Selection) {
        reset()
        switch selection {
        case .skill(let s): buildSkillDetail(s)
        case .mcp(let m): buildMcpDetail(m)
        }
        endStack()
    }

    // MARK: - Skill detail

    private func buildSkillDetail(_ s: SkillEntry) {
        stack.addArrangedSubview(header(s.display_name ?? s.key, warning: s.mismatch))
        if s.mismatch {
            stack.addArrangedSubview(Theme.caption("Agent copies differ — sync to unify them.",
                                                 color: .systemOrange))
        }
        if let desc = s.description, !desc.isEmpty {
            stack.addArrangedSubview(Theme.body(desc))
        }
        stack.addArrangedSubview(Theme.caption("Present on \(s.presence.count) agent\(s.presence.count == 1 ? "" : "s")"))

        let rows = s.presence.map {
            ["agent": $0.agent, "path": $0.path,
             "target": $0.symlink_target ?? ($0.is_symlink ? "(symlink)" : "—"),
             "hash": String(($0.content_hash ?? "—").prefix(10))]
        }
        stack.addArrangedSubview(cardTable(columns: ["agent", "path", "target", "hash"],
                                           rows: rows))

        let buttons = NSStackView()
        buttons.spacing = 8
        let sync = Theme.button("Sync this", symbol: "arrow.triangle.2.circlepath", style: .primary)
        sync.target = self; sync.action = #selector(syncThisSkill)
        sync.identifier = NSUserInterfaceItemIdentifier(s.key)
        let unlink = Theme.button("Unlink", symbol: "link.circle")
        unlink.target = self; unlink.action = #selector(unlinkSkill)
        unlink.identifier = NSUserInterfaceItemIdentifier(s.key)
        let purge = Theme.button("Purge", symbol: "trash", style: .destructive)
        purge.target = self; purge.action = #selector(purgeSkill)
        purge.identifier = NSUserInterfaceItemIdentifier(s.key)
        buttons.addArrangedSubview(sync)
        buttons.addArrangedSubview(unlink)
        buttons.addArrangedSubview(purge)
        buttons.addArrangedSubview(NSView())
        stack.addArrangedSubview(buttons)
    }

    // MARK: - MCP detail

    private func buildMcpDetail(_ m: McpEntry) {
        stack.addArrangedSubview(header(m.key, warning: m.mismatch))
        if m.mismatch {
            stack.addArrangedSubview(Theme.caption("Agent configurations differ — sync to unify them.",
                                                 color: .systemOrange))
        }
        let first = m.presence.first?.normalized
        var facts: [(String, String)] = [("agents", "\(m.presence.count)")]
        if let n = first {
            facts.append(("transport", n.transport ?? "—"))
            if let cmd = n.command { facts.append(("command", cmd.joined(separator: " "))) }
            if let url = n.url { facts.append(("url", url)) }
            if let args = n.args, !args.isEmpty { facts.append(("args", args.joined(separator: " "))) }
            if let en = n.enabled { facts.append(("enabled", en ? "true" : "false")) }
            if let env = n.env_keys, !env.isEmpty { facts.append(("env keys", env.joined(separator: ", "))) }
        }
        let grid = NSGridView(views: facts.map { (k, v) in
            [Theme.caption(k), Theme.body(v)]
        })
        grid.column(at: 0).xPlacement = .trailing
        grid.columnSpacing = 10
        grid.rowSpacing = 5
        stack.addArrangedSubview(grid)

        let rows = m.presence.map {
            ["agent": $0.agent, "path": $0.path,
             "fingerprint": String(($0.fingerprint ?? "—").prefix(12))]
        }
        stack.addArrangedSubview(cardTable(columns: ["agent", "path", "fingerprint"],
                                           rows: rows))

        let buttons = NSStackView()
        buttons.spacing = 8
        let sync = Theme.button("Sync this", symbol: "arrow.triangle.2.circlepath", style: .primary)
        sync.target = self; sync.action = #selector(syncThisMcp)
        sync.identifier = NSUserInterfaceItemIdentifier(m.key)
        let remove = Theme.button("Remove", symbol: "trash", style: .destructive)
        remove.target = self; remove.action = #selector(removeMcp)
        remove.identifier = NSUserInterfaceItemIdentifier(m.key)
        buttons.addArrangedSubview(sync)
        buttons.addArrangedSubview(remove)
        buttons.addArrangedSubview(NSView())
        stack.addArrangedSubview(buttons)
    }

    // MARK: - Forms

    func showInstallSkill() {
        reset()
        stack.addArrangedSubview(Theme.title("Install skill"))
        stack.addArrangedSubview(Theme.caption("From a directory path or a Git URL — previewed as a dry run first."))
        installField.placeholderString = "~/path/to/skill  or  https://github.com/org/repo"
        installField.widthAnchor.constraint(equalToConstant: 380).isActive = true
        let form = Theme.card(installField)
        form.widthAnchor.constraint(equalToConstant: 420).isActive = true
        stack.addArrangedSubview(form)
        let b = Theme.button("Preview install", symbol: "play", style: .primary)
        b.target = self; b.action = #selector(previewInstall)
        stack.addArrangedSubview(b)
        endStack()
    }

    func showAddMcp() {
        reset()
        stack.addArrangedSubview(Theme.title("Add MCP server"))
        mcpName.placeholderString = "server name"
        mcpName.widthAnchor.constraint(equalToConstant: 240).isActive = true
        mcpTransport.removeAllItems()
        mcpTransport.addItems(withTitles: ["stdio", "local", "http", "sse"])
        mcpCommand.placeholderString = "command (e.g. npx -y pkg)"
        mcpCommand.widthAnchor.constraint(equalToConstant: 340).isActive = true
        mcpUrl.placeholderString = "url (for http/sse)"
        mcpUrl.widthAnchor.constraint(equalToConstant: 340).isActive = true
        mcpEnabled.state = .on
        let grid = NSGridView(views: [
            [Theme.caption("name"), mcpName],
            [Theme.caption("transport"), mcpTransport],
            [Theme.caption("command"), mcpCommand],
            [Theme.caption("url"), mcpUrl],
            [NSView(), mcpEnabled],
        ])
        grid.column(at: 0).xPlacement = .trailing
        grid.columnSpacing = 10
        grid.rowSpacing = 8
        let form = Theme.card(grid)
        stack.addArrangedSubview(form)
        let b = Theme.button("Preview add", symbol: "play", style: .primary)
        b.target = self; b.action = #selector(previewMcp)
        stack.addArrangedSubview(b)
        endStack()
    }

    // MARK: - Update

    func showUpdate() {
        reset()
        stack.addArrangedSubview(Theme.title("Update synca"))
        stack.addArrangedSubview(Theme.caption("Check the latest release and install it."))
        endStack()
        actions?.runUpdateCheck()
    }

    func show(updateResult info: UpdateInfo) {
        reset()
        stack.addArrangedSubview(Theme.title("Update synca"))
        let grid = NSGridView(views: [
            [Theme.caption("current"), Theme.body(info.current ?? "unknown")],
            [Theme.caption("latest"), Theme.body(info.latest ?? "unknown")],
        ])
        grid.column(at: 0).xPlacement = .trailing
        grid.columnSpacing = 10
        grid.rowSpacing = 5
        stack.addArrangedSubview(Theme.card(grid))
        if info.update_available == true {
            stack.addArrangedSubview(Theme.caption("An update is available.", color: .systemGreen))
            let b = Theme.button("Install update", symbol: "arrow.down.circle.fill", style: .primary)
            b.target = self; b.action = #selector(updateInstall)
            stack.addArrangedSubview(b)
        } else if info.ok == true {
            let row = NSStackView()
            row.spacing = 8
            row.addArrangedSubview(Theme.icon("checkmark.circle.fill", size: 14, color: .systemGreen))
            row.addArrangedSubview(Theme.caption("You're on the latest version."))
            stack.addArrangedSubview(row)
        } else {
            stack.addArrangedSubview(Theme.body(info.message ?? "Update check failed — see log."))
        }
        if let url = info.url, let link = URL(string: url) {
            let b = Theme.button("Open release page")
            b.isBordered = false
            b.contentTintColor = .controlAccentColor
            b.target = self; b.action = #selector(openLink(_:))
            b.identifier = NSUserInterfaceItemIdentifier(link.absoluteString)
            stack.addArrangedSubview(b)
        }
        endStack()
    }

    // MARK: - Plan (dry run + conflicts)

    func show(plan: Plan, scope: SyncaScope) {
        reset()
        currentPlan = plan
        conflicts = plan.conflicts
        stack.addArrangedSubview(header("Plan · \(plan.title)", warning: !plan.conflicts.isEmpty))

        if !plan.summary.isEmpty {
            let chips = NSStackView()
            chips.spacing = 6
            for (kind, count) in plan.summary {
                chips.addArrangedSubview(Theme.pill("\(kind) ×\(count)"))
            }
            chips.addArrangedSubview(NSView())
            stack.addArrangedSubview(chips)
        }

        if !plan.output.isEmpty {
            stack.addArrangedSubview(consoleView(plan.output, maxHeight: 200))
        }

        if !conflicts.isEmpty {
            stack.addArrangedSubview(Theme.caption("Conflicts — choose how each resolves:",
                                                 color: .systemOrange))
            let col = NSStackView()
            col.orientation = .vertical
            col.spacing = 8
            for (i, c) in conflicts.enumerated() {
                let row = NSStackView()
                row.spacing = 8
                row.alignment = .centerY
                row.addArrangedSubview(Theme.icon(c.kind == .skill ? "shippingbox" : "terminal",
                                                  size: 13))
                let label = NSTextField(labelWithString: c.key)
                label.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
                row.addArrangedSubview(label)
                row.addArrangedSubview(NSView())
                let popup = NSPopUpButton()
                popup.addItems(withTitles: ["skip", "keep source", "keep target"])
                popup.font = Theme.captionFont
                popup.tag = i
                popup.target = self
                popup.action = #selector(conflictChanged(_:))
                row.addArrangedSubview(popup)
                col.addArrangedSubview(row)
            }
            stack.addArrangedSubview(Theme.card(col))
        }

        let buttons = NSStackView()
        buttons.spacing = 8
        if plan.ok {
            let apply = Theme.button(plan.apply.map(applyTitle) ?? "Apply",
                                     symbol: "checkmark.circle", style: .primary)
            apply.target = self; apply.action = #selector(applyPlan)
            buttons.addArrangedSubview(apply)
        } else {
            stack.addArrangedSubview(Theme.caption("Command failed — see log for details.",
                                                 color: .systemRed))
        }
        let cancel = Theme.button("Dismiss", symbol: "xmark")
        cancel.target = self; cancel.action = #selector(dismissPlan)
        buttons.addArrangedSubview(cancel)
        buttons.addArrangedSubview(NSView())
        stack.addArrangedSubview(buttons)
        endStack()
    }

    private func applyTitle(_ apply: Plan.Apply) -> String {
        switch apply {
        case .sync: return "Apply sync"
        case .installSkill: return "Install"
        case .addMcp: return "Add server"
        }
    }

    // MARK: - Actions

    @objc private func syncThisSkill(_ sender: NSButton) {
        guard let key = sender.identifier?.rawValue else { return }
        actions?.requestSync(target: "skills", key: key)
    }
    @objc private func syncThisMcp(_ sender: NSButton) {
        guard let key = sender.identifier?.rawValue else { return }
        actions?.requestSync(target: "mcp", key: key)
    }
    @objc private func unlinkSkill(_ sender: NSButton) {
        guard let key = sender.identifier?.rawValue else { return }
        actions?.removeSkill(key: key, purge: false)
    }
    @objc private func purgeSkill(_ sender: NSButton) {
        guard let key = sender.identifier?.rawValue else { return }
        actions?.removeSkill(key: key, purge: true)
    }
    @objc private func removeMcp(_ sender: NSButton) {
        guard let key = sender.identifier?.rawValue else { return }
        actions?.removeMcp(key: key)
    }
    @objc private func previewInstall() {
        let src = installField.stringValue.trimmingCharacters(in: .whitespaces)
        guard !src.isEmpty else { return }
        actions?.previewInstallSkill(source: src)
    }
    @objc private func previewMcp() {
        let name = mcpName.stringValue.trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty else { return }
        let transport = mcpTransport.titleOfSelectedItem ?? "stdio"
        let payload = Plan.AddMcpPayload(
            name: name, transport: transport,
            command: mcpCommand.stringValue.isEmpty ? nil : mcpCommand.stringValue,
            url: mcpUrl.stringValue.isEmpty ? nil : mcpUrl.stringValue,
            enabled: mcpEnabled.state == .on)
        actions?.previewAddMcp(payload: payload)
    }
    @objc private func updateCheck() { actions?.runUpdateCheck() }
    @objc private func updateInstall() { actions?.runUpdateInstall() }
    @objc private func openLink(_ sender: NSButton) {
        if let s = sender.identifier?.rawValue, let u = URL(string: s) { NSWorkspace.shared.open(u) }
    }
    @objc private func dismissPlan() { showEmpty() }

    @objc private func conflictChanged(_ sender: NSPopUpButton) {
        let policies = ["skip", "keep-source", "keep-target"]
        guard sender.indexOfSelectedItem < policies.count else { return }
        conflicts[sender.tag].policy = policies[sender.indexOfSelectedItem]
        currentPlan?.conflicts = conflicts
    }

    @objc private func applyPlan() {
        guard var plan = currentPlan else { return }
        plan.conflicts = conflicts
        switch plan.apply {
        case .sync: actions?.applySync(plan: plan)
        case .installSkill: actions?.applyInstallSkill(plan: plan)
        case .addMcp: actions?.applyAddMcp(plan: plan)
        case nil: break
        }
    }

    // MARK: - View helpers

    private func header(_ text: String, warning: Bool = false) -> NSView {
        let row = NSStackView()
        row.spacing = 8
        row.alignment = .centerY
        row.addArrangedSubview(Theme.title(text))
        if warning {
            row.addArrangedSubview(Theme.icon("exclamationmark.triangle.fill",
                                              size: 14, color: .systemOrange))
        }
        row.addArrangedSubview(NSView())
        return row
    }

    /// Rounded console-style output area.
    private func consoleView(_ text: String, maxHeight: CGFloat) -> NSView {
        let tv = NSTextView()
        tv.isEditable = false
        tv.font = Theme.monoFont
        tv.textColor = .labelColor
        tv.backgroundColor = .clear
        tv.drawsBackground = false
        tv.textContainerInset = NSSize(width: 4, height: 4)
        tv.string = text
        let sv = NSScrollView()
        sv.documentView = tv
        sv.hasVerticalScroller = true
        sv.drawsBackground = false
        sv.translatesAutoresizingMaskIntoConstraints = false
        sv.widthAnchor.constraint(equalToConstant: 460).isActive = true
        sv.heightAnchor.constraint(lessThanOrEqualToConstant: maxHeight).isActive = true
        sv.heightAnchor.constraint(greaterThanOrEqualToConstant: 80).isActive = true
        return Theme.card(sv, padding: 8)
    }

    /// Table inside a rounded card (Settings-style grouped list).
    private func cardTable(columns: [String], rows: [[String: String]]) -> NSView {
        let tv = NSTableView()
        tv.rowHeight = 24
        tv.intercellSpacing = NSSize(width: 8, height: 4)
        tv.style = .inset
        tv.backgroundColor = .clear
        for c in columns {
            let col = NSTableColumn(identifier: NSUserInterfaceItemIdentifier(c))
            col.title = c
            col.width = c == "path" ? 150 : (c == "target" ? 130 : 80)
            tv.addTableColumn(col)
        }
        let ds = SimpleTable(columns: columns, rows: rows)
        tv.dataSource = ds
        tv.delegate = ds
        objc_setAssociatedObject(tv, "ds", ds, .OBJC_ASSOCIATION_RETAIN)

        let sv = NSScrollView()
        sv.documentView = tv
        sv.hasVerticalScroller = true
        sv.drawsBackground = false
        sv.translatesAutoresizingMaskIntoConstraints = false
        sv.widthAnchor.constraint(equalToConstant: 460).isActive = true
        sv.heightAnchor.constraint(equalToConstant:
            min(180, CGFloat(rows.count) * 28 + 30)).isActive = true
        return Theme.card(sv, padding: 4)
    }
}

final class SimpleTable: NSObject, NSTableViewDataSource, NSTableViewDelegate {
    let columns: [String]
    let rows: [[String: String]]
    init(columns: [String], rows: [[String: String]]) {
        self.columns = columns; self.rows = rows
    }
    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }
    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let key = tableColumn?.identifier.rawValue ?? ""
        let f = NSTextField(labelWithString: rows[row][key] ?? "")
        f.font = key == "path" || key == "target" || key == "hash" || key == "fingerprint"
            ? Theme.monoFont
            : .systemFont(ofSize: 12)
        f.lineBreakMode = .byTruncatingMiddle
        return f
    }
}
