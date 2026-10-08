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
        view = NSView()
        let split = NSSplitView()
        split.isVertical = false // top/bottom
        split.dividerStyle = .thin
        split.translatesAutoresizingMaskIntoConstraints = false

        let skillsSection = makeSection(title: "SKILLS", count: skillCount,
                                        table: skillTable, kind: .skills)
        let mcpsSection = makeSection(title: "MCP SERVERS", count: mcpCount,
                                      table: mcpTable, kind: .mcp)
        split.addArrangedSubview(skillsSection)
        split.addArrangedSubview(mcpsSection)

        view.addSubview(split)
        NSLayoutConstraint.activate([
            split.topAnchor.constraint(equalTo: view.topAnchor),
            split.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            split.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            split.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
    }

    private func makeSection(title: String, count: NSTextField, table: NSTableView,
                             kind: BoardKind) -> NSView {
        let section = NSView()
        section.setContentHuggingPriority(.defaultLow, for: .vertical)

        // header
        let titleLabel = NSTextField(labelWithString: title)
        titleLabel.font = .systemFont(ofSize: 11, weight: .semibold)
        titleLabel.textColor = .secondaryLabelColor
        count.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        count.textColor = .secondaryLabelColor

        let syncButton = iconButton("arrow.triangle.2.circlepath", "Sync all \(title.lowercased())")
        syncButton.tag = kind == .skills ? 0 : 1
        syncButton.target = self
        syncButton.action = #selector(syncClicked(_:))
        let addButton = iconButton("plus", kind == .skills ? "Install skill" : "Add MCP")
        addButton.tag = kind == .skills ? 0 : 1
        addButton.target = self
        addButton.action = #selector(addClicked(_:))

        let header = NSStackView(views: [titleLabel, count, NSView(), syncButton, addButton])
        header.orientation = .horizontal
        header.spacing = 6
        header.edgeInsets = NSEdgeInsets(top: 8, left: 10, bottom: 6, right: 8)
        header.translatesAutoresizingMaskIntoConstraints = false

        // table in scroll view
        table.headerView = nil
        table.rowHeight = 28
        table.usesAutomaticRowHeights = false
        table.style = .plain
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

        let sep = NSBox()
        sep.boxType = .separator
        sep.translatesAutoresizingMaskIntoConstraints = false

        section.addSubview(header)
        section.addSubview(sep)
        section.addSubview(scroll)
        NSLayoutConstraint.activate([
            header.topAnchor.constraint(equalTo: section.topAnchor),
            header.leadingAnchor.constraint(equalTo: section.leadingAnchor),
            header.trailingAnchor.constraint(equalTo: section.trailingAnchor),
            sep.topAnchor.constraint(equalTo: header.bottomAnchor),
            sep.leadingAnchor.constraint(equalTo: section.leadingAnchor),
            sep.trailingAnchor.constraint(equalTo: section.trailingAnchor),
            scroll.topAnchor.constraint(equalTo: sep.bottomAnchor),
            scroll.leadingAnchor.constraint(equalTo: section.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: section.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: section.bottomAnchor),
        ])
        return section
    }

    private func iconButton(_ symbol: String, _ tip: String) -> NSButton {
        let b = NSButton(image: NSImage(systemSymbolName: symbol, accessibilityDescription: tip)!,
                         target: nil, action: nil)
        b.bezelStyle = .accessoryBarAction
        b.isBordered = false
        b.imageScaling = .scaleProportionallyDown
        b.toolTip = tip
        b.setAccessibilityLabel(tip)
        return b
    }

    func set(scope: SyncaScope, skills: [SkillEntry], mcps: [McpEntry]) {
        self.scope = scope
        self.skills = skills
        self.mcps = mcps
        skillCount.stringValue = "\(skills.count)"
        mcpCount.stringValue = "\(mcps.count)"
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
        let cell = tableView.makeView(withIdentifier: NSUserInterfaceItemIdentifier("cell"), owner: nil) as? BoardCell
            ?? BoardCell.make(for: tableView)

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

    static func make(for table: NSTableView) -> BoardCell {
        let c = BoardCell()
        c.identifier = NSUserInterfaceItemIdentifier("cell")
        return c
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        icon.translatesAutoresizingMaskIntoConstraints = false
        icon.contentTintColor = .secondaryLabelColor
        title.translatesAutoresizingMaskIntoConstraints = false
        title.font = .systemFont(ofSize: 13)
        title.lineBreakMode = .byTruncatingTail
        badge.translatesAutoresizingMaskIntoConstraints = false
        badge.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        badge.textColor = .secondaryLabelColor
        warn.translatesAutoresizingMaskIntoConstraints = false
        warn.image = NSImage(systemSymbolName: "exclamationmark.triangle.fill",
                             accessibilityDescription: "mismatch")
        warn.contentTintColor = .systemOrange
        warn.isHidden = true
        addSubview(icon); addSubview(title); addSubview(badge); addSubview(warn)
        NSLayoutConstraint.activate([
            icon.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 10),
            icon.centerYAnchor.constraint(equalTo: centerYAnchor),
            icon.widthAnchor.constraint(equalToConstant: 15),
            icon.heightAnchor.constraint(equalToConstant: 15),
            title.leadingAnchor.constraint(equalTo: icon.trailingAnchor, constant: 8),
            title.centerYAnchor.constraint(equalTo: centerYAnchor),
            warn.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -10),
            warn.centerYAnchor.constraint(equalTo: centerYAnchor),
            warn.widthAnchor.constraint(equalToConstant: 13),
            warn.heightAnchor.constraint(equalToConstant: 13),
            badge.trailingAnchor.constraint(equalTo: warn.leadingAnchor, constant: -6),
            badge.centerYAnchor.constraint(equalTo: centerYAnchor),
            title.trailingAnchor.constraint(lessThanOrEqualTo: badge.leadingAnchor, constant: -6),
        ])
    }
    required init?(coder: NSCoder) { fatalError() }

    func configure(title t: String, icon i: String, badge b: String, warning w: Bool) {
        title.stringValue = t
        icon.image = NSImage(systemSymbolName: i, accessibilityDescription: nil)
        badge.stringValue = b
        warn.isHidden = !w
    }
}
