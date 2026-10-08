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
    private let stack = NSStackView()
    private var currentPlan: Plan?
    private var conflicts: [PlanConflict] = []
    private var conflictRows: [(conflict: PlanConflict, popup: NSPopUpButton)] = []

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
        scroll.translatesAutoresizingMaskIntoConstraints = false
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 10
        stack.edgeInsets = NSEdgeInsets(top: 18, left: 20, bottom: 24, right: 20)
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.documentView = stack
        view.addSubview(scroll)
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: view.topAnchor),
            scroll.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            stack.widthAnchor.constraint(equalTo: scroll.widthAnchor),
        ])
        showEmpty()
    }

    private func reset() {
        stack.arrangedSubviews.forEach { $0.removeFromSuperview() }
        currentPlan = nil
        conflictRows = []
    }

    private func fadeIn() {
        stack.alphaValue = 0
        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = 0.18
            stack.animator().alphaValue = 1
        }
    }

    // MARK: - States

    func showEmpty() {
        reset()
        let label = NSTextField(labelWithString: "Select a skill or MCP server")
        label.font = .systemFont(ofSize: 15, weight: .medium)
        label.textColor = .secondaryLabelColor
        let hint = NSTextField(wrappingLabelWithString:
            "j/k to move · 1/2 switch scope · s sync all · r reload · u update · ? help")
        hint.font = .systemFont(ofSize: 12)
        hint.textColor = .tertiaryLabelColor
        stack.addArrangedSubview(NSView())
        stack.addArrangedSubview(label)
        stack.addArrangedSubview(hint)
        fadeIn()
    }

    func show(selection: Selection) {
        reset()
        switch selection {
        case .skill(let s): buildSkillDetail(s)
        case .mcp(let m): buildMcpDetail(m)
        }
        fadeIn()
    }

    // MARK: - Skill detail

    private func buildSkillDetail(_ s: SkillEntry) {
        stack.addArrangedSubview(header(s.display_name ?? s.key, warning: s.mismatch))
        if s.mismatch {
            stack.addArrangedSubview(caption("Agent copies differ — sync to unify them.", color: .systemOrange))
        }
        if let desc = s.description, !desc.isEmpty {
            stack.addArrangedSubview(body(desc))
        }
        stack.addArrangedSubview(caption("Present on \(s.presence.count) agent\(s.presence.count == 1 ? "" : "s")"))
        stack.addArrangedSubview(sep())

        let rows = s.presence.map {
            ["agent": $0.agent, "path": $0.path,
             "target": $0.symlink_target ?? ($0.is_symlink ? "(symlink)" : "—"),
             "hash": String(($0.content_hash ?? "—").prefix(10))]
        }
        stack.addArrangedSubview(table(columns: ["agent", "path", "target", "hash"], rows: rows, height: min(180, CGFloat(rows.count) * 24 + 6)))

        stack.addArrangedSubview(sep())
        let buttons = NSStackView()
        buttons.orientation = .horizontal
        buttons.spacing = 8
        let sync = pushButton("Sync this", "arrow.triangle.2.circlepath")
        sync.target = self; sync.action = #selector(syncThisSkill)
        sync.tag = 0
        sync.identifier = NSUserInterfaceItemIdentifier(s.key)
        let unlink = pushButton("Unlink", "link.badge.minus")
        unlink.target = self; unlink.action = #selector(unlinkSkill)
        unlink.identifier = NSUserInterfaceItemIdentifier(s.key)
        let purge = pushButton("Purge", "trash")
        purge.target = self; purge.action = #selector(purgeSkill)
        purge.identifier = NSUserInterfaceItemIdentifier(s.key)
        purge.contentTintColor = .systemRed
        buttons.addArrangedSubview(sync)
        buttons.addArrangedSubview(unlink)
        buttons.addArrangedSubview(purge)
        stack.addArrangedSubview(buttons)
    }

    // MARK: - MCP detail

    private func buildMcpDetail(_ m: McpEntry) {
        stack.addArrangedSubview(header(m.key, warning: m.mismatch))
        if m.mismatch {
            stack.addArrangedSubview(caption("Agent configurations differ — sync to unify them.", color: .systemOrange))
        }
        let first = m.presence.first?.normalized
        var facts: [(String, String)] = [
            ("agents", "\(m.presence.count)"),
        ]
        if let n = first {
            facts.append(("transport", n.transport ?? "—"))
            if let cmd = n.command { facts.append(("command", cmd.joined(separator: " "))) }
            if let url = n.url { facts.append(("url", url)) }
            if let args = n.args, !args.isEmpty { facts.append(("args", args.joined(separator: " "))) }
            if let en = n.enabled { facts.append(("enabled", en ? "true" : "false")) }
            if let env = n.env_keys, !env.isEmpty { facts.append(("env keys", env.joined(separator: ", "))) }
        }
        let grid = NSGridView(views: facts.map { (k, v) in
            [caption(k), body(v)]
        })
        grid.column(at: 0).xPlacement = .trailing
        grid.columnSpacing = 10
        grid.rowSpacing = 4
        stack.addArrangedSubview(grid)
        stack.addArrangedSubview(sep())

        let rows = m.presence.map {
            ["agent": $0.agent, "path": $0.path,
             "fingerprint": String(($0.fingerprint ?? "—").prefix(12))]
        }
        stack.addArrangedSubview(table(columns: ["agent", "path", "fingerprint"], rows: rows, height: min(160, CGFloat(rows.count) * 24 + 6)))

        stack.addArrangedSubview(sep())
        let buttons = NSStackView()
        buttons.spacing = 8
        let sync = pushButton("Sync this", "arrow.triangle.2.circlepath")
        sync.target = self; sync.action = #selector(syncThisMcp)
        sync.identifier = NSUserInterfaceItemIdentifier(m.key)
        let remove = pushButton("Remove", "trash")
        remove.target = self; remove.action = #selector(removeMcp)
        remove.identifier = NSUserInterfaceItemIdentifier(m.key)
        remove.contentTintColor = .systemRed
        buttons.addArrangedSubview(sync)
        buttons.addArrangedSubview(remove)
        stack.addArrangedSubview(buttons)
    }

    // MARK: - Forms

    func showInstallSkill() {
        reset()
        stack.addArrangedSubview(header("Install skill"))
        stack.addArrangedSubview(caption("From a directory path or a Git URL — previewed as a dry run first."))
        installField.placeholderString = "~/path/to/skill  or  https://github.com/org/repo"
        installField.frame = NSRect(x: 0, y: 0, width: 420, height: 24)
        installField.widthAnchor.constraint(equalToConstant: 420).isActive = true
        stack.addArrangedSubview(installField)
        let b = pushButton("Preview install", "play")
        b.target = self; b.action = #selector(previewInstall)
        stack.addArrangedSubview(b)
        fadeIn()
    }

    func showAddMcp() {
        reset()
        stack.addArrangedSubview(header("Add MCP server"))
        mcpName.placeholderString = "server name"
        mcpName.widthAnchor.constraint(equalToConstant: 260).isActive = true
        mcpTransport.removeAllItems()
        mcpTransport.addItems(withTitles: ["stdio", "local", "http", "sse"])
        mcpCommand.placeholderString = "command (e.g. npx -y pkg)"
        mcpCommand.widthAnchor.constraint(equalToConstant: 420).isActive = true
        mcpUrl.placeholderString = "url (for http/sse)"
        mcpUrl.widthAnchor.constraint(equalToConstant: 420).isActive = true
        mcpEnabled.state = .on
        let grid = NSGridView(views: [
            [caption("name"), mcpName],
            [caption("transport"), mcpTransport],
            [caption("command"), mcpCommand],
            [caption("url"), mcpUrl],
            [NSView(), mcpEnabled],
        ])
        grid.column(at: 0).xPlacement = .trailing
        grid.columnSpacing = 10
        grid.rowSpacing = 8
        stack.addArrangedSubview(grid)
        let b = pushButton("Preview add", "play")
        b.target = self; b.action = #selector(previewMcp)
        stack.addArrangedSubview(b)
        fadeIn()
    }

    // MARK: - Update

    func showUpdate() {
        reset()
        stack.addArrangedSubview(header("Update synca"))
        stack.addArrangedSubview(caption("Check the latest release and install it."))
        let b = pushButton("Check for updates", "arrow.down.circle")
        b.target = self; b.action = #selector(updateCheck)
        stack.addArrangedSubview(b)
        fadeIn()
        actions?.runUpdateCheck()
    }

    func show(updateResult info: UpdateInfo) {
        reset()
        stack.addArrangedSubview(header("Update synca"))
        let grid = NSGridView(views: [
            [caption("current"), body(info.current ?? "unknown")],
            [caption("latest"), body(info.latest ?? "unknown")],
        ])
        grid.column(at: 0).xPlacement = .trailing
        grid.columnSpacing = 10
        grid.rowSpacing = 4
        stack.addArrangedSubview(grid)
        if info.update_available == true {
            stack.addArrangedSubview(caption("An update is available.", color: .systemGreen))
            let b = pushButton("Install update", "arrow.down.circle.fill")
            b.target = self; b.action = #selector(updateInstall)
            stack.addArrangedSubview(b)
        } else if info.ok == true {
            stack.addArrangedSubview(caption("You're on the latest version.", color: .secondaryLabelColor))
        } else {
            stack.addArrangedSubview(body(info.message ?? "Update check failed — see log."))
        }
        if let url = info.url, let link = URL(string: url) {
            let b = NSButton(title: "Open release page", target: self, action: #selector(openLink(_:)))
            b.isBordered = false
            b.contentTintColor = .systemBlue
            b.identifier = NSUserInterfaceItemIdentifier(link.absoluteString)
            stack.addArrangedSubview(b)
        }
        fadeIn()
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
                let chip = NSTextField(labelWithString: "\(kind) ×\(count)")
                chip.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
                chip.textColor = .secondaryLabelColor
                chip.wantsLayer = true
                chip.layer?.cornerRadius = 4
                chip.layer?.backgroundColor = NSColor.quaternaryLabelColor.cgColor
                chips.addArrangedSubview(chip)
            }
            stack.addArrangedSubview(chips)
        }

        if !plan.output.isEmpty {
            stack.addArrangedSubview(outputView(plan.output, maxHeight: 200))
        }

        if !conflicts.isEmpty {
            stack.addArrangedSubview(sep())
            stack.addArrangedSubview(caption("Conflicts — choose how each resolves:", color: .systemOrange))
            for (i, c) in conflicts.enumerated() {
                let row = NSStackView()
                row.spacing = 8
                let icon = NSImageView(image: NSImage(systemSymbolName:
                    c.kind == .skill ? "shippingbox" : "terminal", accessibilityDescription: nil)!)
                icon.contentTintColor = .secondaryLabelColor
                let label = NSTextField(labelWithString: c.key)
                label.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
                let popup = NSPopUpButton()
                popup.addItems(withTitles: ["skip", "keep source", "keep target"])
                popup.tag = i
                popup.target = self
                popup.action = #selector(conflictChanged(_:))
                row.addArrangedSubview(icon)
                row.addArrangedSubview(label)
                row.addArrangedSubview(NSView())
                row.addArrangedSubview(popup)
                stack.addArrangedSubview(row)
                conflictRows.append((c, popup))
            }
        }

        stack.addArrangedSubview(sep())
        let buttons = NSStackView()
        buttons.spacing = 8
        if plan.ok {
            let apply = pushButton(plan.apply.map { applyTitle($0) } ?? "Apply", "checkmark.circle")
            apply.target = self; apply.action = #selector(applyPlan)
            buttons.addArrangedSubview(apply)
        } else {
            stack.addArrangedSubview(caption("Command failed — see log for details.", color: .systemRed))
        }
        let cancel = pushButton("Dismiss", "xmark")
        cancel.target = self; cancel.action = #selector(dismissPlan)
        buttons.addArrangedSubview(cancel)
        stack.addArrangedSubview(buttons)
        fadeIn()
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
        let t = NSTextField(labelWithString: text)
        t.font = .systemFont(ofSize: 18, weight: .semibold)
        row.addArrangedSubview(t)
        if warning {
            let w = NSImageView(image: NSImage(systemSymbolName: "exclamationmark.triangle.fill",
                                               accessibilityDescription: "mismatch")!)
            w.contentTintColor = .systemOrange
            row.addArrangedSubview(w)
        }
        return row
    }

    private func body(_ text: String) -> NSTextField {
        let f = NSTextField(wrappingLabelWithString: text)
        f.font = .systemFont(ofSize: 13)
        return f
    }

    private func caption(_ text: String, color: NSColor = .secondaryLabelColor) -> NSTextField {
        let f = NSTextField(labelWithString: text)
        f.font = .systemFont(ofSize: 12)
        f.textColor = color
        return f
    }

    private func sep() -> NSBox {
        let b = NSBox()
        b.boxType = .separator
        return b
    }

    private func pushButton(_ title: String, _ symbol: String) -> NSButton {
        let b = NSButton(title: title,
                         image: NSImage(systemSymbolName: symbol, accessibilityDescription: title)!,
                         target: nil, action: nil)
        b.bezelStyle = .toolbar
        return b
    }

    private func outputView(_ text: String, maxHeight: CGFloat) -> NSView {
        let tv = NSTextView()
        tv.isEditable = false
        tv.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        tv.textContainerInset = NSSize(width: 6, height: 6)
        tv.string = text
        let sv = NSScrollView()
        sv.documentView = tv
        sv.hasVerticalScroller = true
        sv.borderType = .lineBorder
        sv.translatesAutoresizingMaskIntoConstraints = false
        sv.widthAnchor.constraint(equalToConstant: 480).isActive = true
        sv.heightAnchor.constraint(lessThanOrEqualToConstant: maxHeight).isActive = true
        sv.heightAnchor.constraint(greaterThanOrEqualToConstant: 80).isActive = true
        return sv
    }

    private func table(columns: [String], rows: [[String: String]], height: CGFloat) -> NSView {
        let tv = NSTableView()
        tv.rowHeight = 22
        tv.intercellSpacing = NSSize(width: 8, height: 4)
        for c in columns {
            let col = NSTableColumn(identifier: NSUserInterfaceItemIdentifier(c))
            col.title = c
            col.width = c == "path" || c == "target" ? 200 : 80
            tv.addTableColumn(col)
        }
        let ds = SimpleTable(columns: columns, rows: rows)
        tv.dataSource = ds
        tv.delegate = ds
        objc_setAssociatedObject(tv, "ds", ds, .OBJC_ASSOCIATION_RETAIN)
        let sv = NSScrollView()
        sv.documentView = tv
        sv.hasVerticalScroller = true
        sv.borderType = .lineBorder
        sv.translatesAutoresizingMaskIntoConstraints = false
        sv.widthAnchor.constraint(equalToConstant: 480).isActive = true
        sv.heightAnchor.constraint(equalToConstant: height).isActive = true
        return sv
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
            ? .monospacedSystemFont(ofSize: 11, weight: .regular)
            : .systemFont(ofSize: 12)
        f.lineBreakMode = .byTruncatingMiddle
        return f
    }
}
