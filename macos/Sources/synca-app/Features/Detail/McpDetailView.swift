import SwiftUI
import SyncaKit

struct McpDetailView: View {
    @Environment(AppModel.self) private var model
    let mcp: McpEntry

    private var cfg: McpNormalized? { mcp.presence.first?.normalized }

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
            .padding(Theme.Space.lg)
        }
    }

    private var hero: some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Space.md) {
                HStack(alignment: .top, spacing: Theme.Space.md) {
                    HeroBadge(systemImage: Icon.mcp)
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        Text(mcp.key).font(.title3.weight(.semibold)).textSelection(.enabled)
                        HStack(spacing: Theme.Space.xs) {
                            if let t = cfg?.transport { Tag(text: t, tint: .blue) }
                            if mcp.mismatch { Tag(text: "mismatch", tint: .orange, systemImage: Icon.warning) }
                            if let e = cfg?.enabled {
                                Tag(text: e ? "enabled" : "disabled", tint: e ? .green : .secondary)
                            }
                        }
                    }
                    Spacer(minLength: 0)
                }
                if mcp.mismatch {
                    Label("Agent configs differ — sync to unify them", systemImage: Icon.warning)
                        .font(.caption).foregroundStyle(.orange)
                }
                HStack(spacing: Theme.Space.sm) {
                    Button { model.planSync(target: .mcp, key: mcp.key) } label: {
                        Label("Sync this", systemImage: Icon.sync)
                    }
                    .buttonStyle(.borderedProminent)
                    Button(role: .destructive) { model.confirmRemoveMcp(mcp.key) } label: {
                        Label("Remove", systemImage: Icon.remove)
                    }
                    if model.isBusy { ProgressView().controlSize(.small) }
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
                Grid(alignment: .topLeading, horizontalSpacing: Theme.Space.lg, verticalSpacing: Theme.Space.sm) {
                    ForEach(rows, id: \.0) { k, v in
                        GridRow {
                            Text(k).font(.callout).foregroundStyle(.secondary)
                            Text(v).font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                }
            }
        }
    }

    private var presenceTable: some View {
        Table(mcp.presence) {
            TableColumn("Agent") { p in Text(p.agent).fontWeight(.semibold) }
                .width(min: 70, ideal: 100)
            TableColumn("Path") { p in
                Text(p.path).font(.system(.caption, design: .monospaced))
                    .lineLimit(1).truncationMode(.middle).help(p.path).textSelection(.enabled)
            }
            TableColumn("Fingerprint") { p in
                Text(shortHash(p.fingerprint)).font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
            .width(min: 80, ideal: 100)
            TableColumn("Actions") { p in RevealButton(path: p.path) }.width(80)
        }
        .frame(height: tableHeight(rows: mcp.presence.count))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
    }
}
