import SwiftUI
import AppKit
import SyncaKit

struct ActivityRow: View {
    let entry: LogEntry
    let isExpanded: Bool
    let toggle: () -> Void

    private var statusIcon: (String, Color, String) {
        switch entry.status {
        case "ok": (Icon.success, .green, "Succeeded")
        case "dry-run": (Icon.info, .blue, "Dry run")
        default: (Icon.error, .red, "Failed")
        }
    }

    var body: some View {
        let (icon, tint, label) = statusIcon
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Button(action: toggle) {
                HStack(alignment: .top, spacing: Theme.Space.sm) {
                    Image(systemName: icon).foregroundStyle(tint).accessibilityLabel(label)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(entry.command)
                            .font(.system(.caption, design: .monospaced))
                            .lineLimit(2).truncationMode(.middle)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        HStack(spacing: Theme.Space.sm) {
                            Text(entry.date, style: .relative).font(.caption2).foregroundStyle(.secondary)
                            Tag(text: entry.scope)
                        }
                    }
                    Image(systemName: Icon.disclosure)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                        .font(.caption).foregroundStyle(.tertiary).accessibilityHidden(true)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityHint(isExpanded ? "Hide output" : "Show output")

            if isExpanded {
                ConsoleText(text: entry.output.isEmpty ? "(no output)" : entry.output, maxHeight: 160)
            }
        }
        .padding(.vertical, Theme.Space.xs)
        .contextMenu {
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(entry.command, forType: .string)
            } label: {
                Label("Copy command", systemImage: Icon.copy)
            }
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(entry.output, forType: .string)
            } label: {
                Label("Copy output", systemImage: Icon.copy)
            }
        }
    }
}

private extension Icon {
    static let disclosure = "chevron.right"
}
