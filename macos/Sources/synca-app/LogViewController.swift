import AppKit

final class LogViewController: NSViewController {
    var onClear: (() -> Void)?

    private var entries: [LogEntry] = []   // newest first
    private var oldestLoaded: Int = .max

    private let table = NSTableView()
    private let output = NSTextView()
    private let loadMoreButton = NSButton(title: "Load older entries", target: nil, action: nil)
    private let emptyLabel = NSTextField(labelWithString: "No commands yet")
    private var outputCard: NSView!

    override func loadView() {
        view = NSView()

        // header
        let title = NSTextField(labelWithString: "Log")
        title.font = .systemFont(ofSize: 12, weight: .semibold)
        title.textColor = .secondaryLabelColor
        let clear = Theme.iconButton("trash", tip: "Clear log")
        clear.target = self
        clear.action = #selector(clearClicked)
        let header = NSStackView(views: [title, NSView(), clear])
        header.alignment = .centerY
        header.edgeInsets = NSEdgeInsets(top: 10, left: 12, bottom: 8, right: 10)

        // table
        table.headerView = nil
        table.rowHeight = 38
        table.style = .plain
        table.intercellSpacing = NSSize(width: 0, height: 4)
        table.addTableColumn(NSTableColumn(identifier: NSUserInterfaceItemIdentifier("main")))
        table.dataSource = self
        table.delegate = self
        table.setAccessibilityLabel("Command log")

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false

        // lazy-load button sits at the bottom of the list
        loadMoreButton.bezelStyle = .inline
        loadMoreButton.font = Theme.captionFont
        loadMoreButton.target = self
        loadMoreButton.action = #selector(loadOlder)
        loadMoreButton.isHidden = true
        loadMoreButton.contentTintColor = .secondaryLabelColor

        // output detail for the selected entry, as a rounded console card
        output.isEditable = false
        output.font = Theme.monoFont
        output.textColor = .labelColor
        output.backgroundColor = .clear
        output.drawsBackground = false
        output.textContainerInset = NSSize(width: 6, height: 6)
        let outputScroll = NSScrollView()
        outputScroll.documentView = output
        outputScroll.hasVerticalScroller = true
        outputScroll.drawsBackground = false
        outputScroll.translatesAutoresizingMaskIntoConstraints = false
        let card = Theme.card(outputScroll, padding: 4)
        card.isHidden = true
        outputCard = card

        emptyLabel.font = Theme.captionFont
        emptyLabel.textColor = .tertiaryLabelColor
        emptyLabel.alignment = .center

        let stack = NSStackView(views: [header, scroll, loadMoreButton, card])
        stack.orientation = .vertical
        stack.spacing = 0
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.setContentHuggingPriority(.defaultLow, for: .vertical)
        scroll.setContentCompressionResistancePriority(.defaultLow, for: .vertical)
        card.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)
        view.addSubview(emptyLabel)
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: view.topAnchor),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            stack.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            outputScroll.heightAnchor.constraint(lessThanOrEqualToConstant: 160),
            card.leadingAnchor.constraint(equalTo: stack.leadingAnchor, constant: 10),
            card.trailingAnchor.constraint(equalTo: stack.trailingAnchor, constant: -10),
        ])
        // Required center constraints would make the label's intrinsic height
        // the window's fitting size (constraint-driven window resize).
        let cx = emptyLabel.centerXAnchor.constraint(equalTo: view.centerXAnchor)
        let cy = emptyLabel.centerYAnchor.constraint(equalTo: view.centerYAnchor)
        cx.priority = .defaultLow
        cy.priority = .defaultLow
        cx.isActive = true
        cy.isActive = true

        reloadAll()
        NotificationCenter.default.addObserver(self, selector: #selector(logChanged),
                                               name: LogStore.changed, object: nil)
    }

    deinit { NotificationCenter.default.removeObserver(self) }

    private func reloadAll() {
        entries = LogStore.shared.recent()
        oldestLoaded = entries.last?.id ?? .max
        table.reloadData()
        updateChrome()
    }

    /// Lazy: fetch one older page when the user asks (scroll bottom / button).
    @objc private func loadOlder() {
        guard oldestLoaded > 1 else { return }
        let older = LogStore.shared.page(before: oldestLoaded)
        guard !older.isEmpty else { loadMoreButton.isHidden = true; return }
        let start = entries.count
        entries.append(contentsOf: older)
        oldestLoaded = older.last!.id
        table.insertRows(at: IndexSet(start..<entries.count), withAnimation: .slideDown)
        updateChrome()
    }

    @objc private func logChanged() {
        let newest = entries.first?.id ?? 0
        let fresh = LogStore.shared.page(after: newest)
        if !fresh.isEmpty {
            entries.insert(contentsOf: fresh, at: 0)
            table.insertRows(at: IndexSet(0..<fresh.count), withAnimation: .slideUp)
        } else if entries.isEmpty {
            reloadAll()
            return
        }
        updateChrome()
    }

    private func updateChrome() {
        emptyLabel.isHidden = !entries.isEmpty
        // offer lazy loading only if there may be more on disk
        loadMoreButton.isHidden = entries.isEmpty
    }

    @objc private func clearClicked() { onClear?() }
}

extension LogViewController: NSTableViewDataSource, NSTableViewDelegate {
    func numberOfRows(in tableView: NSTableView) -> Int { entries.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let cell = LogCell()
        cell.configure(entries[row])
        return cell
    }

    func tableViewSelectionDidChange(_ notification: Notification) {
        let row = table.selectedRow
        guard entries.indices.contains(row), !entries[row].output.isEmpty else {
            outputCard.isHidden = true
            return
        }
        output.string = entries[row].output
        outputCard.isHidden = false
    }

    /// Scrolled to the oldest visible rows → prefetch the next page (lazy load).
    func tableView(_ tableView: NSTableView, didAdd rowView: NSTableRowView, forRow row: Int) {
        if row >= entries.count - 2 { loadOlder() }
    }
}

// MARK: - Row cell

final class LogCell: NSView {
    private let icon = NSImageView()
    private let command = NSTextField(labelWithString: "")
    private let meta = NSTextField(labelWithString: "")

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        icon.translatesAutoresizingMaskIntoConstraints = false
        command.translatesAutoresizingMaskIntoConstraints = false
        meta.translatesAutoresizingMaskIntoConstraints = false
        command.font = Theme.monoFont
        command.lineBreakMode = .byTruncatingTail
        meta.font = Theme.metaFont
        meta.textColor = .tertiaryLabelColor
        addSubview(icon); addSubview(command); addSubview(meta)
        NSLayoutConstraint.activate([
            icon.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 12),
            icon.topAnchor.constraint(equalTo: topAnchor, constant: 9),
            icon.widthAnchor.constraint(equalToConstant: 11),
            icon.heightAnchor.constraint(equalToConstant: 11),
            command.leadingAnchor.constraint(equalTo: icon.trailingAnchor, constant: 8),
            command.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -8),
            command.centerYAnchor.constraint(equalTo: icon.centerYAnchor),
            meta.leadingAnchor.constraint(equalTo: command.leadingAnchor),
            meta.topAnchor.constraint(equalTo: command.bottomAnchor, constant: 2),
        ])
    }
    required init?(coder: NSCoder) { fatalError() }

    func configure(_ e: LogEntry) {
        let (symbol, color): (String, NSColor) = switch e.status {
        case "ok": ("checkmark.circle.fill", .systemGreen)
        case "dry-run": ("eye.fill", .controlAccentColor)
        default: ("xmark.circle.fill", .systemRed)
        }
        icon.image = Theme.icon(symbol, size: 11, color: color).image
        icon.contentTintColor = color
        command.stringValue = e.command
        meta.stringValue = "\(e.scope) · \(e.timeString) · exit \(e.exit)"
    }
}
