import SwiftUI
import SyncaKit

struct SkillRow: View {
    let skill: SkillEntry
    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Image(systemName: Icon.skill).foregroundStyle(.secondary).accessibilityHidden(true)
            Text(skill.title).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: Theme.Space.xs)
            if skill.mismatch {
                Image(systemName: Icon.warning).foregroundStyle(.orange)
                    .help("Content differs between agents")
                    .accessibilityLabel("Content differs between agents")
            }
            Text("\(skill.presence.count)").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                .help("Present in \(skill.presence.count) agent(s)")
                .accessibilityLabel("\(skill.presence.count) agents")
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
            Text(mcp.key).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: Theme.Space.xs)
            if let transport { Tag(text: transport) }
            if mcp.mismatch {
                Image(systemName: Icon.warning).foregroundStyle(.orange)
                    .help("Configuration differs between agents")
                    .accessibilityLabel("Configuration differs between agents")
            }
            Text("\(mcp.presence.count)").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                .help("Present in \(mcp.presence.count) agent(s)")
                .accessibilityLabel("\(mcp.presence.count) agents")
        }
        .contentShape(Rectangle())
    }
}
