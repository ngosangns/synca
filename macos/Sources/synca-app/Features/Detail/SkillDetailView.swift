import SwiftUI
import SyncaKit

struct SkillDetailView: View {
    @Environment(AppModel.self) private var model
    let skill: SkillEntry

    /// Width of the scrolling content (padding excluded) and height of the viewport.
    @State private var contentSize: CGSize = .zero
    @State private var viewport: CGSize = .zero

    /// Unknown (0) is treated as wide so the first frame doesn't flash a compact layout.
    private var isCompact: Bool { contentSize.width > 0 && contentSize.width < DetailBreakpoint.compactTable }
    private var explorerLayout: ExplorerLayout {
        contentSize.width > 0 && contentSize.width < DetailBreakpoint.sideBySide ? .stacked : .sideBySide
    }
    /// Explorer grows with the window when there is room.
    private var explorerIdealHeight: CGFloat { min(max(viewport.height * 0.55, 360), 640) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                hero
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    SectionHeading(title: "Files")
                    SkillFileTree(skillID: skill.id, path: skill.primaryPath, layout: explorerLayout)
                        .frame(minHeight: 240, idealHeight: explorerIdealHeight, maxHeight: 640)
                }
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    SectionHeading(title: "Presence", count: skill.presence.count)
                    presenceTable
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .measureSize(into: $contentSize)
            .padding(Theme.Space.lg)
        }
        .measureSize(into: $viewport)
    }

    private var hero: some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Space.md) {
                HeroHeader(
                    systemImage: Icon.skillFilled,
                    title: skill.title,
                    subtitle: "\(skill.key) · \(skill.presence.count) agent\(skill.presence.count == 1 ? "" : "s")",
                    showTags: skill.mismatch
                ) {
                    Tag(text: "mismatch", tint: .orange, systemImage: Icon.warning)
                }
                if let d = skill.description, !d.isEmpty {
                    Text(d).font(.body).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                }
                if skill.mismatch {
                    Label("Agent copies differ — sync to unify them", systemImage: Icon.warning)
                        .font(.caption).foregroundStyle(.orange)
                        .fixedSize(horizontal: false, vertical: true)
                }
                ActionBar(isBusy: model.isBusy) {
                    Button { model.planSync(target: .skills, key: skill.key) } label: {
                        Label("Sync this", systemImage: Icon.sync)
                    }
                    .buttonStyle(.borderedProminent)
                    .help("Plan a sync of this skill across agents")
                    Button { model.confirmRemoveSkill(skill.key, purge: false) } label: {
                        Label("Unlink", systemImage: Icon.unlink)
                    }
                    .help("Remove the agent links but keep the files")
                    Button(role: .destructive) { model.confirmRemoveSkill(skill.key, purge: true) } label: {
                        Label("Purge", systemImage: Icon.remove)
                    }
                    .help("Remove the skill and delete its files")
                }
                .disabled(model.isBusy)
            }
        }
    }

    @ViewBuilder private var presenceTable: some View {
        Group {
            if isCompact {
                Table(skill.presence) {
                    TableColumn("Agent") { p in Text(p.agent).fontWeight(.semibold) }
                        .width(min: 60, ideal: 80, max: 140)
                    pathColumn
                    actionsColumn
                }
            } else {
                Table(skill.presence) {
                    TableColumn("Agent") { p in Text(p.agent).fontWeight(.semibold) }
                        .width(min: 70, ideal: 100, max: 180)
                    TableColumn("Type") { p in
                        if p.isSymlink { Tag(text: "symlink", tint: .blue, systemImage: Icon.symlink) }
                        else { Tag(text: "copy") }
                    }
                    .width(min: 80, ideal: 100, max: 140)
                    pathColumn
                    TableColumn("Hash") { p in
                        Text(shortHash(p.contentHash)).font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                    }
                    .width(min: 60, ideal: 80, max: 120)
                    actionsColumn
                }
            }
        }
        .frame(height: tableHeight(rows: skill.presence.count))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
    }

    /// Unbounded: takes whatever width the other columns leave.
    private var pathColumn: some TableColumnContent<SkillPresence, Never> {
        TableColumn("Path") { (p: SkillPresence) in
            Text(p.path).font(.system(.caption, design: .monospaced))
                .lineLimit(1).truncationMode(.middle).help(p.path).textSelection(.enabled)
        }
        .width(min: 100, ideal: 240)
    }

    private var actionsColumn: some TableColumnContent<SkillPresence, Never> {
        TableColumn("Actions") { (p: SkillPresence) in RevealButton(path: p.path) }
            .width(min: 80, ideal: 90, max: 110)
    }
}
