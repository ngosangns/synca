import AppKit

final class BoardsViewController: NSViewController {
    var onSelect: ((Selection) -> Void)?
    var onSync: ((BoardKind) -> Void)?
    var onAdd: ((BoardKind) -> Void)?

    private var skills: [SkillEntry] = []
    private var mcps: [McpEntry] = []
    private var scope: SyncaScope = .user

    private let skillTable = NSTableView()
    private let mcpTable = NSTableView()
    private let skillCount = NSTextField(labelWithString: "")
    private let mcpCount = NSTextField(labelWithString: "")
    private var activeBoard: BoardKind = .skills

    override func loadView() {
        // Translucent sidebar material — the classic macOS look.
        let fx = NSVisualEffectView()
        fx.material = .sidebar
        fx.blendingMode = .behindWindow
        fx.state = .active
        view = fx

        let inner = InnerSplit()
        inner.splitView.isVertical = false // panes stacked top/bottom
        inner.splitView.dividerStyle = .thin
        inner.view.translatesAutoresizingMaskIntoConstraints = false

        let skillsItem = NSSplitViewItem(viewController: SectionHost(
            makeSection(title: "Skills", count: skillCount, table: skillTable, kind: .skills)))
        skillsItem.minimumThickness = 44
        skillsItem.preferredThicknessFraction = 0.5
        let mcpsItem = NSSplitViewItem(viewController: SectionHost(
            makeSection(title: "MCP Servers", count: mcpCount, table: mcpTable, kind: .mcp)))
        mcpsItem.minimumThickness = 44
        mcpsItem.preferredThicknessFraction = 0.5
        inner.splitViewItems = [skillsItem, mcpsItem]

        addChild(inner)
        fx.addSubview(inner.view)

        // project-scope footer: current directory + picker
        let folderIcon = Theme.icon("folder", size: 12)
        projectPath.font = Theme.monoFont
        projectPath.textColor = .secondaryLabelColor
        projectPath.lineBreakMode = .byTruncatingHead
        let pick = Theme.iconButton("ellipsis.circle", tip: "Choose project directory")
        pick.target = self
        pick.action = #selector(chooseProjectDir)
        projectFooter.addArrangedSubview(folderIcon)
        projectFooter.addArrangedSubview(projectPath)
        projectFooter.addArrangedSubview(NSView())
        projectFooter.addArrangedSubview(pick)
        projectFooter.alignment = .centerY
        projectFooter.edgeInsets = NSEdgeInsets(top: 8, left: 12, bottom: 10, right: 10)
        projectFooter.translatesAutoresizingMaskIntoConstraints = false
        projectFooter.isHidden = true
        fx.addSubview(projectFooter)
        // Explicit height keeps the hidden footer from absorbing the split's
        // space through ambiguous Auto Layout resolution.
        footerHeight = projectFooter.heightAnchor.constraint(equalToConstant: 0)

        NSLayoutConstraint.activate([
            inner.view.topAnchor.constraint(equalTo: fx.topAnchor),
            inner.view.leadingAnchor.constraint(equalTo: fx.leadingAnchor),
            inner.view.trailingAnchor.constraint(equalTo: fx.trailingAnchor),
            inner.view.bottomAnchor.constraint(equalTo: projectFooter.topAnchor),
            footerHeight,
            projectFooter.leadingAnchor.constraint(equalTo: fx.leadingAnchor),
            projectFooter.trailingAnchor.constraint(equalTo: fx.trailingAnchor),
            projectFooter.bottomAnchor.constraint(equalTo: fx.bottomAnchor),
        ])
    }

    private var footerHeight: NSLayoutConstraint!

    private let projectFooter = NSStackView()
    private let projectPath = NSTextField(labelWithString: "")

    @objc private func chooseProjectDir() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false
        panel.directoryURL = URL(fileURLWithPath: Synca.projectDir)
        panel.beginSheetModal(for: view.window!) { [weak self] res in
            guard res == .OK, let url = panel.url else { return }
            Synca.projectDir = url.path
            self?.projectPath.stringValue = url.path
            NotificationCenter.default.post(name: .reloadInventory, object: nil)
        }
    }

    /// Split items are laid out at their minimum thickness first, then the
    /// last item absorbs all extra height — rebalance once the view gets a
    /// real frame so both sections share the space evenly.
    private final class InnerSplit: NSSplitViewController {
        private var balanced = false
        override func splitViewDidResizeSubviews(_ notification: Notification) {
            super.splitViewDidResizeSubviews(notification)
            guard !balanced, view.bounds.height > 120 else { return }
            balanced = true
            splitView.setPosition(view.bounds.height * 0.5, ofDividerAt: 0)
        }
    }

    /// Wraps a plain section view so NSSplitViewController can manage it.
    private final class SectionHost: NSViewController {
        private let content: NSView
        init(_ content: NSView) { self.content = content; super.init(nibName: nil, bundle: nil) }
        required init?(coder: NSCoder) { fatalError() }
        override func loadView() { view = content }
    }

    private func makeSection(title: String, count: NSTextField, table: NSTableView,
                             kind: BoardKind) -> NSView {
        let section = NSView()
        section.setContentHuggingPriority(.defaultLow, for: .vertical)

        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 12, weight: .semibold)
        titleLabel.textColor = .secondaryLabelColor
        count.font = Theme.monoDigit
        count.textColor = .tertiaryLabelColor

        let syncButton = Theme.iconButton("arrow.triangle.2.circlepath",
                                          tip: "Sync all \(title.lowercased())")
        syncButton.tag = kind == .skills ? 0 : 1
        syncButton.target = self
        syncButton.action = #selector(syncClicked(_:))
        let addButton = Theme.iconButton("plus",
                                         tip: kind == .skills ? "Install skill" : "Add MCP")
        addButton.tag = kind == .skills ? 0 : 1
        addButton.target = self
        addButton.action = #selector(addClicked(_:))

        let header = NSStackView(views: [titleLabel, count, NSView(), syncButton, addButton])
        header.orientation = .horizontal
        header.alignment = .centerY
        header.spacing = 6
        header.edgeInsets = NSEdgeInsets(top: 10, left: 12, bottom: 8, right: 10)
        header.translatesAutoresizingMaskIntoConstraints = false

        // sourceList style = rounded translucent selection over the vibrancy
        table.headerView = nil
        table.rowHeight = 30
        table.style = .sourceList
        table.selectionHighlightStyle = .sourceList
        table.backgroundColor = .clear
        table.intercellSpacing = NSSize(width: 0, height: 2)
        let col = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("main"))
        table.addTableColumn(col)
        table.delegate = self
        table.dataSource = self
        table.tag = kind == .skills ? 0 : 1
        table.target = self
        table.doubleAction = #selector(tableActivated(_:))
        table.setAccessibilityLabel(title)

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.translatesAutoresizingMaskIntoConstraints = false

        section.addSubview(header)
        section.addSubview(scroll)
        NSLayoutConstraint.activate([
            header.topAnchor.constraint(equalTo: section.topAnchor),
            header.leadingAnchor.constraint(equalTo: section.leadingAnchor),
            header.trailingAnchor.constraint(equalTo: section.trailingAnchor),
            scroll.topAnchor.constraint(equalTo: header.bottomAnchor),
            scroll.leadingAnchor.constraint(equalTo: section.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: section.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: section.bottomAnchor),
        ])
        return section
    }

    func set(scope: SyncaScope, skills: [SkillEntry], mcps: [McpEntry]) {
        self.scope = scope
        self.skills = skills
        self.mcps = mcps
        skillCount.stringValue = "\(skills.count)"
        mcpCount.stringValue = "\(mcps.count)"
        projectFooter.isHidden = scope != .project
        footerHeight.constant = scope == .project ? 36 : 0
        if scope == .project { projectPath.stringValue = Synca.projectDir }
        let prevSkillRow = skillTable.selectedRow
        let prevMcpRow = mcpTable.selectedRow
        skillTable.reloadData()
        mcpTable.reloadData()
        // restore selection if still valid and re-emit so the detail pane
        // shows fresh data, not the stale entry
        if skills.indices.contains(prevSkillRow) {
            skillTable.selectRowIndexes([prevSkillRow], byExtendingSelection: false)
        }
        if mcps.indices.contains(prevMcpRow) {
            mcpTable.selectRowIndexes([prevMcpRow], byExtendingSelection: false)
        }
        if skillTable.selectedRow >= 0 { emitSelection(skillTable) }
        else if mcpTable.selectedRow >= 0 { emitSelection(mcpTable) }
    }

    func moveSelection(_ delta: Int) {
        let table = activeBoard == .skills ? skillTable : mcpTable
        let count = table.numberOfRows
        guard count > 0 else { return }
        let next = max(0, min(count - 1, (table.selectedRow < 0 ? 0 : table.selectedRow) + delta))
        table.selectRowIndexes([next], byExtendingSelection: false)
        table.scrollRowToVisible(next)
    }

    @objc private func syncClicked(_ sender: NSButton) {
        onSync?(sender.tag == 0 ? .skills : .mcp)
    }
    @objc private func addClicked(_ sender: NSButton) {
        onAdd?(sender.tag == 0 ? .skills : .mcp)
    }
    @objc private func tableActivated(_ sender: NSTableView) { emitSelection(sender) }

    private func emitSelection(_ table: NSTableView) {
        let row = table.selectedRow
        guard row >= 0 else { return }
        if table === skillTable {
            activeBoard = .skills
            if skills.indices.contains(row) { onSelect?(.skill(skills[row])) }
            mcpTable.deselectAll(nil)
        } else {
            activeBoard = .mcp
            if mcps.indices.contains(row) { onSelect?(.mcp(mcps[row])) }
            skillTable.deselectAll(nil)
        }
    }
}

// MARK: - NSTableView

extension BoardsViewController: NSTableViewDataSource, NSTableViewDelegate {
    func numberOfRows(in tableView: NSTableView) -> Int {
        tableView === skillTable ? skills.count : mcps.count
    }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let cell = BoardCell()
        if tableView === skillTable, skills.indices.contains(row) {
            let s = skills[row]
            cell.configure(title: s.display_name ?? s.key,
                           icon: "shippingbox",
                           badge: "\(s.presence.count)",
                           warning: s.mismatch)
        } else if mcps.indices.contains(row) {
            let m = mcps[row]
            cell.configure(title: m.key,
                           icon: "terminal",
                           badge: "\(m.presence.count)",
                           warning: m.mismatch)
        }
        return cell
    }

    func tableViewSelectionDidChange(_ notification: Notification) {
        guard let table = notification.object as? NSTableView else { return }
        emitSelection(table)
    }
}

// MARK: - Row cell

final class BoardCell: NSTableCellView {
    private let icon = NSImageView()
    private let title = NSTextField(labelWithString: "")
    private let badge = NSTextField(labelWithString: "")
    private let warn = NSImageView()

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        icon.translatesAutoresizingMaskIntoConstraints = false
        icon.contentTintColor = .secondaryLabelColor
        title.translatesAutoresizingMaskIntoConstraints = false
        title.font = .systemFont(ofSize: 13)
        title.lineBreakMode = .byTruncatingTail
        badge.translatesAutoresizingMaskIntoConstraints = false
        badge.font = Theme.monoDigit
        badge.textColor = .tertiaryLabelColor
        warn.translatesAutoresizingMaskIntoConstraints = false
        warn.image = Theme.icon("exclamationmark.triangle.fill", size: 11,
                                color: .systemOrange).image
        warn.contentTintColor = .systemOrange
        warn.isHidden = true
        addSubview(icon); addSubview(title); addSubview(badge); addSubview(warn)
        NSLayoutConstraint.activate([
            icon.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 14),
            icon.centerYAnchor.constraint(equalTo: centerYAnchor),
            icon.widthAnchor.constraint(equalToConstant: 14),
            icon.heightAnchor.constraint(equalToConstant: 14),
            title.leadingAnchor.constraint(equalTo: icon.trailingAnchor, constant: 8),
            title.centerYAnchor.constraint(equalTo: centerYAnchor),
            warn.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -12),
            warn.centerYAnchor.constraint(equalTo: centerYAnchor),
            warn.widthAnchor.constraint(equalToConstant: 12),
            warn.heightAnchor.constraint(equalToConstant: 12),
            badge.trailingAnchor.constraint(equalTo: warn.leadingAnchor, constant: -6),
            badge.centerYAnchor.constraint(equalTo: centerYAnchor),
            title.trailingAnchor.constraint(lessThanOrEqualTo: badge.leadingAnchor, constant: -6),
        ])
    }
    required init?(coder: NSCoder) { fatalError() }

    func configure(title t: String, icon i: String, badge b: String, warning w: Bool) {
        title.stringValue = t
        icon.image = Theme.icon(i, size: 13).image
        badge.stringValue = b
        warn.isHidden = !w
    }
}
