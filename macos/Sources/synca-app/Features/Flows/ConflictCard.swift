import SwiftUI
import SyncaKit

private extension Icon {
    static let source = "tray.full"
    static let target = "scope"
    static let columns = "rectangle.split.2x1"
    static let unified = "text.alignleft"
    static let diff = "plus.forwardslash.minus"
    static let chevron = "chevron.right"
}

/// One conflict: both copies side by side, the diff between them, and the
/// resolution picker. Nothing is overwritten unless a resolution is chosen.
struct ConflictCard: View {
    @Binding var conflict: PlanConflict
    @Environment(AppModel.self) private var model

    @State private var comparison: ConflictComparison?
    @State private var loading = true
    @State private var sideBySide = false
    @State private var showIdentical = false

    private var loadKey: String { conflict.id + "|" + conflict.hashes.joined() + conflict.fingerprints.joined() }

    var body: some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Space.md) {
                header
                resolution
                if loading {
                    HStack(spacing: Theme.Space.sm) {
                        ProgressView().controlSize(.small)
                        Text("Comparing copies…").font(.callout).foregroundStyle(.secondary)
                    }
                } else if let c = comparison {
                    sides(c)
                    if !c.otherVersions.isEmpty {
                        Label("\(c.otherVersions.count) other version\(c.otherVersions.count == 1 ? "" : "s") exist (\(c.otherVersions.map(\.agent).joined(separator: ", "))). They are replaced by whichever copy you keep.",
                              systemImage: Icon.info)
                            .font(.caption).foregroundStyle(.secondary)
                    }
                    differences(c)
                } else {
                    Label("Couldn’t read both copies to compare them. You can still choose a resolution.",
                          systemImage: Icon.warning)
                        .font(.callout).foregroundStyle(.secondary)
                }
            }
        }
        .task(id: loadKey) { await load() }
    }

    // MARK: Loading

    private func load() async {
        loading = true
        let c = conflict
        let skill = model.skills.first { $0.key == c.key }
        let mcp = model.mcps.first { $0.key == c.key }
        let result = await Task.detached(priority: .userInitiated) {
            c.kind == .skill ? ConflictCompare.skill(c, entry: skill) : ConflictCompare.mcp(c, entry: mcp)
        }.value
        if Task.isCancelled { return }
        comparison = result
        loading = false
    }

    // MARK: Header & resolution

    private var header: some View {
        HStack(spacing: Theme.Space.sm) {
            Image(systemName: Icon.warning).foregroundStyle(.orange).accessibilityHidden(true)
            Tag(text: conflict.kind == .skill ? "skill" : "mcp")
            Text(conflict.key).font(.headline).lineLimit(1).truncationMode(.middle).textSelection(.enabled)
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var resolution: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Picker("Resolution for \(conflict.key)", selection: $conflict.policy) {
                ForEach(ConflictPolicy.allCases) { Text($0.title).tag($0) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .disabled(model.isBusy)
            .accessibilityLabel("Resolution for \(conflict.key)")
            Text(explanation).font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
        }
    }

    private var explanation: String {
        let src = comparison?.source.agent ?? "source", tgt = comparison?.target.agent ?? "target"
        switch conflict.policy {
        case .skip: return "Skip — leave every copy as it is. Nothing is overwritten."
        case .keepSource: return "Keep source — “\(src)” wins. The other copies are replaced by links to it."
        case .keepTarget: return "Keep target — “\(tgt)” wins and becomes the canonical copy. The other copies are replaced by links to it."
        }
    }

    // MARK: Both copies

    @ViewBuilder private func sides(_ c: ConflictComparison) -> some View {
        // Adaptive columns depend only on the available width (not on the intrinsic
        // width of long paths), so the boxes sit side by side whenever two fit.
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 280), spacing: Theme.Space.md, alignment: .top)],
                  alignment: .leading, spacing: Theme.Space.sm) {
            SideBox(role: "Source", icon: Icon.source, side: c.source, kept: conflict.policy == .keepSource,
                    discarded: conflict.policy == .keepTarget)
            SideBox(role: "Target", icon: Icon.target, side: c.target, kept: conflict.policy == .keepTarget,
                    discarded: conflict.policy == .keepSource)
        }
    }

    // MARK: Differences

    @ViewBuilder private func differences(_ c: ConflictComparison) -> some View {
        let changed = c.files.filter { $0.status != .identical }
        let identical = c.files.filter { $0.status == .identical }
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            HStack(spacing: Theme.Space.sm) {
                Label("Differences", systemImage: Icon.diff).font(.subheadline.weight(.semibold))
                Text("source → target").font(.caption).foregroundStyle(.secondary)
                Spacer(minLength: 0)
                Picker("Diff layout", selection: $sideBySide) {
                    Label("Unified", systemImage: Icon.unified).tag(false)
                    Label("Side by side", systemImage: Icon.columns).tag(true)
                }
                .pickerStyle(.segmented).labelsHidden().fixedSize()
                .help("Switch between a unified and a side-by-side diff")
                .accessibilityLabel("Diff layout")
            }
            if changed.isEmpty {
                Text("Contents match line for line; the copies differ only in metadata or file modes.")
                    .font(.callout).foregroundStyle(.secondary)
            }
            ForEach(Array(changed.enumerated()), id: \.element.id) { idx, f in
                FileDiffView(file: f, sideBySide: sideBySide, startsOpen: idx == 0)
            }
            if !identical.isEmpty {
                DisclosureGroup(isExpanded: $showIdentical) {
                    VStack(alignment: .leading, spacing: 2) {
                        ForEach(identical) { f in
                            Text(f.path).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.top, Theme.Space.xs)
                } label: {
                    Text("\(identical.count) identical file\(identical.count == 1 ? "" : "s")")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
}

// MARK: - One copy

private struct SideBox: View {
    let role: String
    let icon: String
    let side: ConflictSide
    let kept: Bool
    let discarded: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.xs) {
                Label(role.uppercased(), systemImage: icon).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                Spacer(minLength: 0)
                if kept { Tag(text: "will be kept", tint: .green, systemImage: Icon.success) }
                else if discarded { Tag(text: "will be replaced", tint: .orange) }
            }
            Text(side.agent).font(.headline)
            Text(side.path).font(.system(.caption, design: .monospaced))
                .lineLimit(2).truncationMode(.middle).textSelection(.enabled).help(side.path)
            HStack(spacing: Theme.Space.sm) {
                if !side.digest.isEmpty {
                    Text(side.digest).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                }
                Text("\(side.fileCount) file\(side.fileCount == 1 ? "" : "s")").font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(Theme.Space.md)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(kept ? Color.green.opacity(0.08) : Color.clear, in: RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(kept ? Color.green.opacity(0.5) : Color.secondary.opacity(0.25)))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(role) copy, \(side.agent), \(side.fileCount) files\(kept ? ", will be kept" : "")\(discarded ? ", will be replaced" : "")")
    }
}

// MARK: - One file's diff

private struct FileDiffView: View {
    let file: FileDiff
    let sideBySide: Bool
    @State private var open: Bool
    @State private var showAll = false

    private static let rowCap = 400

    init(file: FileDiff, sideBySide: Bool, startsOpen: Bool) {
        self.file = file
        self.sideBySide = sideBySide
        _open = State(initialValue: startsOpen)
    }

    private var statusTag: Tag {
        switch file.status {
        case .modified: Tag(text: "modified", tint: .orange)
        case .added: Tag(text: "only in target", tint: .green)
        case .removed: Tag(text: "only in source", tint: .red)
        case .identical: Tag(text: "identical")
        }
    }

    private var statusWord: String {
        switch file.status {
        case .modified: "modified"
        case .added: "only in target"
        case .removed: "only in source"
        case .identical: "identical"
        }
    }

    var body: some View {
        DisclosureGroup(isExpanded: $open) {
            content.padding(.top, Theme.Space.xs)
        } label: {
            HStack(spacing: Theme.Space.sm) {
                Text(file.path).font(.system(.callout, design: .monospaced)).lineLimit(1).truncationMode(.middle)
                statusTag
                Spacer(minLength: 0)
                if file.additions > 0 { Text("+\(file.additions)").foregroundStyle(.green).monospacedDigit() }
                if file.deletions > 0 { Text("−\(file.deletions)").foregroundStyle(.red).monospacedDigit() }
            }
            .font(.caption)
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(file.path), \(statusWord), \(file.additions) additions, \(file.deletions) deletions")
        }
    }

    @ViewBuilder private var content: some View {
        switch file.content {
        case .binary:
            note("Binary file — sizes: \(size(file.sourceSize)) → \(size(file.targetSize))")
        case .tooLarge:
            note("File is too large to diff here — sizes: \(size(file.sourceSize)) → \(size(file.targetSize))")
        case .none:
            note("No differences.")
        case .text(let rows):
            let shown = showAll ? rows : Array(rows.prefix(Self.rowCap))
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Group {
                    if sideBySide { SplitDiffTable(rows: TextDiff.split(shown)) }
                    else { UnifiedDiffTable(rows: shown) }
                }
                .textSelection(.enabled)
                if rows.count > Self.rowCap, !showAll {
                    Button("Show all \(rows.count) rows") { showAll = true }.controlSize(.small)
                        .help("Render the rest of this diff")
                }
            }
        }
    }

    private func size(_ n: Int64?) -> String {
        n.map { ByteCountFormatter.string(fromByteCount: $0, countStyle: .file) } ?? "—"
    }

    private func note(_ s: String) -> some View {
        Text(s).font(.callout).foregroundStyle(.secondary).frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Diff tables

private let mono = Font.system(.caption, design: .monospaced)

private func rowColor(_ k: DiffRow.Kind) -> Color {
    switch k { case .added: .green.opacity(0.16); case .removed: .red.opacity(0.16); default: .clear }
}

private struct CollapsedRow: View {
    let count: Int
    var body: some View {
        Text("⋯ \(count) unchanged line\(count == 1 ? "" : "s")")
            .font(.caption).foregroundStyle(.tertiary)
            .frame(maxWidth: .infinity).padding(.vertical, 3)
            .background(.quaternary.opacity(0.4))
    }
}

private struct UnifiedDiffTable: View {
    let rows: [DiffRow]
    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(Array(rows.enumerated()), id: \.offset) { _, r in
                if r.kind == .collapsed { CollapsedRow(count: r.hidden) }
                else {
                    HStack(alignment: .top, spacing: 0) {
                        num(r.oldNo); num(r.newNo)
                        Text(r.kind == .added ? "+" : r.kind == .removed ? "−" : " ")
                            .font(mono).frame(width: 14)
                            .foregroundStyle(r.kind == .added ? .green : r.kind == .removed ? .red : .secondary)
                        Text(r.text.isEmpty ? " " : r.text).font(mono)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .background(rowColor(r.kind))
                }
            }
        }
        .modifier(DiffFrame())
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Unified diff")
    }
    private func num(_ n: Int?) -> some View {
        Text(n.map(String.init) ?? "").font(mono).foregroundStyle(.tertiary)
            .frame(width: 34, alignment: .trailing).padding(.trailing, 4)
    }
}

private struct SplitDiffTable: View {
    let rows: [SplitRow]
    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(Array(rows.enumerated()), id: \.offset) { _, r in
                if let hidden = r.collapsed { CollapsedRow(count: hidden) }
                else {
                    HStack(alignment: .top, spacing: 1) {
                        cell(r.left, number: r.left?.oldNo)
                        cell(r.right, number: r.right?.newNo)
                    }
                }
            }
        }
        .modifier(DiffFrame())
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Side-by-side diff")
    }
    private func cell(_ row: DiffRow?, number: Int?) -> some View {
        HStack(alignment: .top, spacing: 0) {
            Text(number.map(String.init) ?? "").font(mono).foregroundStyle(.tertiary)
                .frame(width: 30, alignment: .trailing).padding(.trailing, 4)
            Text(row.map { $0.text.isEmpty ? " " : $0.text } ?? " ").font(mono)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(row.map { rowColor($0.kind) } ?? Color.secondary.opacity(0.06))
    }
}

private struct DiffFrame: ViewModifier {
    func body(content: Content) -> some View {
        content
            .background(.background)
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
    }
}
