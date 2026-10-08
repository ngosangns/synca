import SwiftUI
import SyncaKit

struct SkillDetailView: View {
    @Environment(AppModel.self) private var model
    let skill: SkillEntry

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                hero
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    SectionHeading(title: "Files")
                    SkillFileTree(skillID: skill.id, path: skill.primaryPath)
                        .frame(minHeight: 260, idealHeight: 340)
                }
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    SectionHeading(title: "Presence", count: skill.presence.count)
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
                    HeroBadge(systemImage: Icon.skillFilled)
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        HStack(spacing: Theme.Space.sm) {
                            Text(skill.title).font(.title3.weight(.semibold)).textSelection(.enabled)
                            if skill.mismatch { Tag(text: "mismatch", tint: .orange, systemImage: Icon.warning) }
                        }
                        Text("\(skill.key) · \(skill.presence.count) agent\(skill.presence.count == 1 ? "" : "s")")
                            .font(.subheadline).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                    Spacer(minLength: 0)
                }
                if let d = skill.description, !d.isEmpty {
                    Text(d).font(.body).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                }
                if skill.mismatch {
                    Label("Agent copies differ — sync to unify them", systemImage: Icon.warning)
                        .font(.caption).foregroundStyle(.orange)
                }
                HStack(spacing: Theme.Space.sm) {
                    Button { model.planSync(target: .skills, key: skill.key) } label: {
                        Label("Sync this", systemImage: Icon.sync)
                    }
                    .buttonStyle(.borderedProminent)
                    Button { model.confirmRemoveSkill(skill.key, purge: false) } label: {
                        Label("Unlink", systemImage: Icon.unlink)
                    }
                    Button(role: .destructive) { model.confirmRemoveSkill(skill.key, purge: true) } label: {
                        Label("Purge", systemImage: Icon.remove)
                    }
                    if model.isBusy { ProgressView().controlSize(.small) }
                }
                .disabled(model.isBusy)
            }
        }
    }

    private var presenceTable: some View {
        Table(skill.presence) {
            TableColumn("Agent") { p in Text(p.agent).fontWeight(.semibold) }
                .width(min: 70, ideal: 100)
            TableColumn("Type") { p in
                if p.isSymlink { Tag(text: "symlink", tint: .blue, systemImage: Icon.symlink) }
                else { Tag(text: "copy") }
            }
            .width(min: 80, ideal: 100)
            TableColumn("Path") { p in
                Text(p.path).font(.system(.caption, design: .monospaced))
                    .lineLimit(1).truncationMode(.middle).help(p.path).textSelection(.enabled)
            }
            TableColumn("Hash") { p in
                Text(shortHash(p.contentHash)).font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
            .width(min: 60, ideal: 80)
            TableColumn("Actions") { p in RevealButton(path: p.path) }.width(80)
        }
        .frame(height: tableHeight(rows: skill.presence.count))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
    }
}
