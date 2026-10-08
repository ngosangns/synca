import SwiftUI
import SyncaKit

struct McpDetailView: View {
    @Environment(AppModel.self) private var model
    let mcp: McpEntry

    /// Width of the scrolling content (padding excluded).
    @State private var contentSize: CGSize = .zero

    private var cfg: McpNormalized? { mcp.presence.first?.normalized }
    private var isCompact: Bool { contentSize.width > 0 && contentSize.width < DetailBreakpoint.compactTable }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                hero
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    SectionHeading(title: "Configuration")
                    configuration
                }
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    SectionHeading(title: "Presence", count: mcp.presence.count)
                    presenceTable
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .measureSize(into: $contentSize)
            .padding(Theme.Space.lg)
        }
    }

    private var hasTags: Bool { cfg?.transport != nil || mcp.mismatch || cfg?.enabled != nil }

    private var hero: some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Space.md) {
                HeroHeader(systemImage: Icon.mcp, title: mcp.key, showTags: hasTags) {
                    if let t = cfg?.transport { Tag(text: t, tint: .blue) }
                    if mcp.mismatch { Tag(text: "mismatch", tint: .orange, systemImage: Icon.warning) }
                    if let e = cfg?.enabled {
                        Tag(text: e ? "enabled" : "disabled", tint: e ? .green : .secondary)
                    }
                }
                if mcp.mismatch {
                    Label("Agent configs differ — sync to unify them", systemImage: Icon.warning)
                        .font(.caption).foregroundStyle(.orange)
                        .fixedSize(horizontal: false, vertical: true)
                }
                ActionBar(isBusy: model.isBusy) {
                    Button { model.planSync(target: .mcp, key: mcp.key) } label: {
                        Label("Sync this", systemImage: Icon.sync)
                    }
                    .buttonStyle(.borderedProminent)
                    .help("Plan a sync of this MCP server across agents")
                    Button(role: .destructive) { model.confirmRemoveMcp(mcp.key) } label: {
                        Label("Remove", systemImage: Icon.remove)
                    }
                    .help("Remove this MCP server from all agents")
                }
                .disabled(model.isBusy)
            }
        }
    }

    private var rows: [(String, String)] {
        guard let c = cfg else { return [] }
        var r: [(String, String)] = []
        if let cmd = c.command, !cmd.isEmpty { r.append(("Command", cmd.joined(separator: " "))) }
        if let u = c.url { r.append(("URL", u)) }
        if let a = c.args, !a.isEmpty { r.append(("Args", a.joined(separator: " "))) }
        if let e = c.enabled { r.append(("Enabled", e ? "Yes" : "No")) }
        if let k = c.envKeys, !k.isEmpty { r.append(("Env keys", k.joined(separator: ", "))) }
        return r
    }

    @ViewBuilder private var configuration: some View {
        Card {
            if rows.isEmpty {
                Text("No configuration details available.").font(.callout).foregroundStyle(.secondary)
            } else {
                // Label column sizes to its widest label; value column takes the rest and wraps.
                Grid(alignment: .topLeading, horizontalSpacing: Theme.Space.lg, verticalSpacing: Theme.Space.sm) {
                    ForEach(rows, id: \.0) { k, v in
                        GridRow {
                            Text(k).font(.callout).foregroundStyle(.secondary)
                                .lineLimit(1).fixedSize()
                                .gridColumnAlignment(.leading)
                            Text(v).font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                                .fixedSize(horizontal: false, vertical: true)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder private var presenceTable: some View {
        Group {
            if isCompact {
                Table(mcp.presence) {
                    TableColumn("Agent") { p in Text(p.agent).fontWeight(.semibold) }
                        .width(min: 60, ideal: 80, max: 140)
                    pathColumn
                    actionsColumn
                }
            } else {
                Table(mcp.presence) {
                    TableColumn("Agent") { p in Text(p.agent).fontWeight(.semibold) }
                        .width(min: 70, ideal: 100, max: 180)
                    pathColumn
                    TableColumn("Fingerprint") { p in
                        Text(shortHash(p.fingerprint)).font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                    }
                    .width(min: 80, ideal: 100, max: 140)
                    actionsColumn
                }
            }
        }
        .frame(height: tableHeight(rows: mcp.presence.count))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
    }

    /// Unbounded: takes whatever width the other columns leave.
    private var pathColumn: some TableColumnContent<McpPresence, Never> {
        TableColumn("Path") { (p: McpPresence) in
            Text(p.path).font(.system(.caption, design: .monospaced))
                .lineLimit(1).truncationMode(.middle).help(p.path).textSelection(.enabled)
        }
        .width(min: 100, ideal: 240)
    }

    private var actionsColumn: some TableColumnContent<McpPresence, Never> {
        TableColumn("Actions") { (p: McpPresence) in RevealButton(path: p.path) }
            .width(min: 80, ideal: 90, max: 110)
    }
}
