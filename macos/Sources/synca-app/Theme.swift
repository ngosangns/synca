import AppKit

/// Small design tokens + component factories shared across panes.
@MainActor enum Theme {
    static let pad: CGFloat = 20      // page padding
    static let gap: CGFloat = 14      // section spacing
    static let radius: CGFloat = 8    // card corner radius

    static let titleFont = NSFont.systemFont(ofSize: 20, weight: .semibold)
    static let sectionFont = NSFont.systemFont(ofSize: 11, weight: .semibold)
    static let bodyFont = NSFont.systemFont(ofSize: 13)
    static let captionFont = NSFont.systemFont(ofSize: 12)
    static let metaFont = NSFont.systemFont(ofSize: 10)
    static let monoFont = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    static let monoDigit = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)

    static func icon(_ name: String, size: CGFloat = 14,
                     color: NSColor = .secondaryLabelColor) -> NSImageView {
        let cfg = NSImage.SymbolConfiguration(pointSize: size, weight: .medium)
        let img = NSImage(systemSymbolName: name, accessibilityDescription: nil)?
            .withSymbolConfiguration(cfg)
        let v = NSImageView(image: img ?? NSImage())
        v.contentTintColor = color
        v.setContentHuggingPriority(.required, for: .horizontal)
        v.setContentHuggingPriority(.required, for: .vertical)
        return v
    }

    /// Settings-style rounded card that wraps content.
    static func card(_ content: NSView, padding: CGFloat = 12) -> NSView {
        let box = NSView()
        box.wantsLayer = true
        box.layer?.cornerRadius = radius
        box.layer?.backgroundColor = NSColor.controlBackgroundColor.cgColor
        box.layer?.borderWidth = 0.5
        box.layer?.borderColor = NSColor.separatorColor.withAlphaComponent(0.5).cgColor
        content.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(content)
        NSLayoutConstraint.activate([
            content.topAnchor.constraint(equalTo: box.topAnchor, constant: padding),
            content.leadingAnchor.constraint(equalTo: box.leadingAnchor, constant: padding),
            content.trailingAnchor.constraint(equalTo: box.trailingAnchor, constant: -padding),
            content.bottomAnchor.constraint(equalTo: box.bottomAnchor, constant: -padding),
        ])
        return box
    }

    /// Rounded pill label, e.g. plan summary chips.
    static func pill(_ text: String, color: NSColor = .secondaryLabelColor) -> NSView {
        let label = NSTextField(labelWithString: text)
        label.font = monoDigit
        label.textColor = color
        let v = NSView()
        v.wantsLayer = true
        v.layer?.cornerRadius = 9
        v.layer?.backgroundColor = NSColor.quaternaryLabelColor.cgColor
        label.translatesAutoresizingMaskIntoConstraints = false
        v.addSubview(label)
        NSLayoutConstraint.activate([
            label.topAnchor.constraint(equalTo: v.topAnchor, constant: 2),
            label.bottomAnchor.constraint(equalTo: v.bottomAnchor, constant: -2),
            label.leadingAnchor.constraint(equalTo: v.leadingAnchor, constant: 8),
            label.trailingAnchor.constraint(equalTo: v.trailingAnchor, constant: -8),
        ])
        return v
    }

    /// macOS push button; `.primary` fills with the system accent color.
    enum ButtonStyle { case regular, primary, destructive }
    static func button(_ title: String, symbol: String? = nil,
                       style: ButtonStyle = .regular) -> NSButton {
        let b = NSButton(title: title, target: nil, action: nil)
        b.bezelStyle = .push
        if let symbol, let base = NSImage(systemSymbolName: symbol,
                                          accessibilityDescription: title) {
            b.image = base.withSymbolConfiguration(.init(pointSize: 12, weight: .medium)) ?? base
            b.imagePosition = .imageLeading
            b.imageHugsTitle = true
        }
        switch style {
        case .primary:
            b.bezelColor = .controlAccentColor
            b.keyEquivalent = "\r"
        case .destructive:
            b.contentTintColor = .systemRed
        case .regular:
            break
        }
        return b
    }

    static func iconButton(_ symbol: String, tip: String) -> NSButton {
        let cfg = NSImage.SymbolConfiguration(pointSize: 12, weight: .medium)
        let base = NSImage(systemSymbolName: symbol, accessibilityDescription: tip) ?? NSImage()
        let b = NSButton(image: base.withSymbolConfiguration(cfg) ?? base,
                         target: nil, action: nil)
        b.bezelStyle = .accessoryBarAction
        b.isBordered = false
        b.imageScaling = .scaleProportionallyDown
        b.contentTintColor = .secondaryLabelColor
        b.toolTip = tip
        b.setAccessibilityLabel(tip)
        return b
    }

    static func title(_ text: String) -> NSTextField {
        let f = NSTextField(labelWithString: text)
        f.font = titleFont
        return f
    }

    static func body(_ text: String) -> NSTextField {
        let f = NSTextField(wrappingLabelWithString: text)
        f.font = bodyFont
        return f
    }

    static func caption(_ text: String, color: NSColor = .secondaryLabelColor) -> NSTextField {
        let f = NSTextField(labelWithString: text)
        f.font = captionFont
        f.textColor = color
        return f
    }

    static func separator() -> NSBox {
        let b = NSBox()
        b.boxType = .separator
        return b
    }
}

extension Notification.Name {
    static let reloadInventory = Notification.Name("synca.reloadInventory")
    static let busyChanged = Notification.Name("synca.busyChanged")
    static let flashStatus = Notification.Name("synca.flashStatus")
}
