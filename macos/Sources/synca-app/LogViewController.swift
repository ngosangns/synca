import AppKit

final class LogViewController: NSViewController {
    var onClear: (() -> Void)?

    private var entries: [LogEntry] = []   // newest first
    private var oldestLoaded: Int = .max

    private let table = NSTableView()
    private let output = NSTextView()
    private let loadMoreButton = NSButton(title: "Load older entries", target: nil, action: nil)
    private let emptyLabel = NSTextField(labelWithString: "No commands yet")

    override func loadView() {
        view = NSView()

        // header
        let title = NSTextField(labelWithString: "LOG")
        title.font = .systemFont(ofSize: 11, weight: .semibold)
        title.textColor = .secondaryLabelColor
        let clear = NSButton(image: NSImage(systemSymbolName: "trash", accessibilityDescription: "Clear log")!,
                             target: self, action: #selector(clearClicked))
        clear.bezelStyle = .accessoryBarAction
        clear.isBordered = false
        clear.toolTip = "Clear log"
        clear.setAccessibilityLabel("Clear log")
        let header = NSStackView(views: [title, NSView(), clear])
        header.edgeInsets = NSEdgeInsets(top: 8, left: 10, bottom: 6, right: 8)

        // table
        table.headerView = nil
        table.rowHeight = 34
        table.style = .plain
        table.intercellSpacing = NSSize(width: 0, height: 3)
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
        loadMoreButton.target = self
        loadMoreButton.action = #selector(loadOlder)
        loadMoreButton.isHidden = true

        // output detail for the selected entry
        output.isEditable = false
        output.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        output.textContainerInset = NSSize(width: 8, height: 8)
        output.isHidden = true
        let outputScroll = NSScrollView()
        outputScroll.documentView = output
        outputScroll.hasVerticalScroller = true
        outputScroll.borderType = .lineBorder
        outputScroll.isHidden = true
        self.outputScroll = outputScroll

        emptyLabel.font = .systemFont(ofSize: 12)
        emptyLabel.textColor = .tertiaryLabelColor
        emptyLabel.alignment = .center
        emptyLabel.isHidden = false

        let stack = NSStackView(views: [header, scroll, loadMoreButton, outputScroll])
        stack.orientation = .vertical
        stack.spacing = 0
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.setContentHuggingPriority(.defaultLow, for: .vertical)
        scroll.setContentCompressionResistancePriority(.defaultLow, for: .vertical)
        view.addSubview(stack)
        view.addSubview(emptyLabel)
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: view.topAnchor),
            stack.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            stack.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            outputScroll.heightAnchor.constraint(lessThanOrEqualToConstant: 180),
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

    private var outputScroll: NSScrollView!

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
            outputScroll.isHidden = true
            return
        }
        output.string = entries[row].output
        outputScroll.isHidden = false
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
        command.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
        command.lineBreakMode = .byTruncatingTail
        meta.font = .systemFont(ofSize: 10)
        meta.textColor = .tertiaryLabelColor
        addSubview(icon); addSubview(command); addSubview(meta)
        NSLayoutConstraint.activate([
            icon.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 10),
            icon.topAnchor.constraint(equalTo: topAnchor, constant: 8),
            icon.widthAnchor.constraint(equalToConstant: 12),
            icon.heightAnchor.constraint(equalToConstant: 12),
            command.leadingAnchor.constraint(equalTo: icon.trailingAnchor, constant: 8),
            command.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -8),
            command.centerYAnchor.constraint(equalTo: icon.centerYAnchor),
            meta.leadingAnchor.constraint(equalTo: command.leadingAnchor),
            meta.topAnchor.constraint(equalTo: command.bottomAnchor, constant: 1),
        ])
    }
    required init?(coder: NSCoder) { fatalError() }

    func configure(_ e: LogEntry) {
        let (symbol, color): (String, NSColor) = switch e.status {
        case "ok": ("checkmark.circle.fill", .systemGreen)
        case "dry-run": ("eye.fill", .systemBlue)
        default: ("xmark.circle.fill", .systemRed)
        }
        icon.image = NSImage(systemSymbolName: symbol, accessibilityDescription: e.status)
        icon.contentTintColor = color
        command.stringValue = e.command
        meta.stringValue = "\(e.scope) · \(e.timeString) · exit \(e.exit)"
    }
}
