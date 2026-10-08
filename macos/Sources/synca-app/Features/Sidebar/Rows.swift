import SwiftUI
import SyncaKit

/// Shared column widths so headers and rows align exactly.
enum Col {
    static let icon: CGFloat = 16
    static let transport: CGFloat = 64
    static let status: CGFloat = 44
    static let agents: CGFloat = 52
}

/// Non-selectable column-heading row shown under a section heading.
struct ColumnHeaderRow: View {
    let name: String
    var showsTransport = false
    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Color.clear.frame(width: Col.icon, height: 1)
            Text(name).frame(maxWidth: .infinity, alignment: .leading)
            if showsTransport { Text("Transport").frame(width: Col.transport, alignment: .leading) }
            Text("Status").frame(width: Col.status, alignment: .center)
                .lineLimit(1).minimumScaleFactor(0.5)
            Text("Agents").frame(width: Col.agents, alignment: .trailing)
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
                Color.clear.accessibilityHidden(true)
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
