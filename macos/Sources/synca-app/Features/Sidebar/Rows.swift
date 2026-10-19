import SwiftUI
import SyncaKit
import NSShell

/// Shared column widths so headers and rows align exactly.
enum Col {
    static let icon: CGFloat = 16
    static let transport: CGFloat = 64
    static let status: CGFloat = 44
    static let agents: CGFloat = 52
}

/// One clickable column heading: selects the sort key on first click, flips
/// the direction when it's already active. Shows a direction chevron while
/// active; inactive headers render like static labels.
struct SortHeader: View {
    let title: String
    let key: String
    @Binding var sortKey: String
    @Binding var ascending: Bool
    /// nil = flexible column that fills the space the fixed columns leave.
    var width: CGFloat? = nil
    var alignment: Alignment = .leading

    private var active: Bool { sortKey == key }

    var body: some View {
        Button {
            if active { ascending.toggle() } else { sortKey = key; ascending = true }
        } label: {
            Group {
                if let width {
                    label.frame(width: width, alignment: alignment)
                } else {
                    label.frame(maxWidth: .infinity, alignment: alignment)
                }
            }
            .foregroundStyle(active ? HierarchicalShapeStyle.secondary : .tertiary)
        }
        .buttonStyle(.plain)
        .help("Sort by \(title)")
        .accessibilityLabel("Sort by \(title)")
        .accessibilityValue(active ? (ascending ? "ascending" : "descending") : "not sorted")
    }

    private var label: some View {
        HStack(spacing: 2) {
            Text(title).lineLimit(1).minimumScaleFactor(0.5)
            if active {
                Image(systemName: ascending ? Icon.sortAsc : Icon.sortDesc)
                    .font(.system(size: 7, weight: .bold))
                    .accessibilityHidden(true)
            }
        }
    }
}

/// Non-selectable column-heading row shown under a section heading; every
/// title sorts the rows below it.
struct ColumnHeaderRow: View {
    let name: String
    var showsTransport = false
    @Binding var sortKey: String
    @Binding var ascending: Bool

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Color.clear.frame(width: Col.icon, height: 1)
            SortHeader(title: name, key: "name", sortKey: $sortKey, ascending: $ascending)
            if showsTransport {
                SortHeader(title: "Transport", key: "transport", sortKey: $sortKey,
                           ascending: $ascending, width: Col.transport)
            }
            SortHeader(title: "Status", key: "status", sortKey: $sortKey,
                       ascending: $ascending, width: Col.status, alignment: .center)
            SortHeader(title: "Agents", key: "agents", sortKey: $sortKey,
                       ascending: $ascending, width: Col.agents, alignment: .trailing)
        }
        .font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
        .listRowSeparator(.hidden)
        .selectionDisabled()
    }
}

private struct StatusSlot: View {
    let mismatch: Bool
    let message: String
    var body: some View {
        Group {
            if mismatch {
                Image(systemName: Icon.warning).foregroundStyle(.orange)
                    .help(message).accessibilityLabel(message)
            } else {
                Image(systemName: Icon.success).foregroundStyle(.green)
                    .help("Identical across agents").accessibilityLabel("In sync")
            }
        }
        .frame(width: Col.status)
    }
}

private struct AgentsCount: View {
    let count: Int
    var body: some View {
        Text("\(count)").font(.caption).monospacedDigit().foregroundStyle(.secondary)
            .frame(width: Col.agents, alignment: .trailing)
            .help("Present in \(count) agent(s)")
            .accessibilityLabel("\(count) agents")
    }
}

struct SkillRow: View {
    let skill: SkillEntry
    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Image(systemName: Icon.skill).foregroundStyle(.secondary).accessibilityHidden(true)
                .frame(width: Col.icon)
            Text(skill.title).lineLimit(1).truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
            StatusSlot(mismatch: skill.mismatch, message: "Content differs between agents")
            AgentsCount(count: skill.presence.count)
        }
        .contentShape(Rectangle())
    }
}

struct McpRow: View {
    let mcp: McpEntry
    private var transport: String? { mcp.presence.first?.normalized?.transport }
    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Image(systemName: Icon.mcp).foregroundStyle(.secondary).accessibilityHidden(true)
                .frame(width: Col.icon)
            Text(mcp.key).lineLimit(1).truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
            Group {
                if let transport { Tag(text: transport) } else { Color.clear }
            }
            .frame(width: Col.transport, alignment: .leading)
            StatusSlot(mismatch: mcp.mismatch, message: "Configuration differs between agents")
            AgentsCount(count: mcp.presence.count)
        }
        .contentShape(Rectangle())
    }
}
