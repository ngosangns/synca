import SwiftUI
import AppKit
import SyncaKit

/// How the explorer arranges the outline and the preview.
enum ExplorerLayout: Sendable {
    /// Outline on the left, preview on the right.
    case sideBySide
    /// Outline on top, preview below.
    case stacked
}

/// File explorer for a skill directory: outline + preview in the given layout.
struct SkillFileTree: View {
    let skillID: String
    let path: String?
    var layout: ExplorerLayout = .sideBySide

    private enum Phase {
        case loading
        case missing
        case ready(nodes: [TreeItem], files: [String: FileNode])
    }

    @State private var phase: Phase = .loading
    @State private var selection: String?
    /// Width the outline needs to show its rows untruncated; capped so a long
    /// name can't starve the preview.
    @State private var treeFitWidth: CGFloat = 220

    var body: some View {
        Group {
            switch phase {
            case .loading:
                VStack { ProgressView("Loading files…").controlSize(.small) }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            case .missing:
                StateView(systemImage: Icon.folder, title: "No files",
                          message: path.map { "\($0) is missing or empty." } ?? "This skill has no path on disk.")
            case .ready(let nodes, let files):
                switch layout {
                case .sideBySide:
                    HSplitView {
                        tree(nodes)
                            .frame(minWidth: 140, idealWidth: treeFitWidth,
                                   maxWidth: max(treeFitWidth, 320))
                        preview(files)
                            .frame(minWidth: 200, maxWidth: .infinity)
                    }
                case .stacked:
                    VSplitView {
                        tree(nodes)
                            .frame(minHeight: 100)
                        preview(files)
                            .frame(minHeight: 100)
                    }
                }
            }
        }
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .task(id: skillID + "|" + (path ?? "")) { await load() }
    }

    private func tree(_ nodes: [TreeItem]) -> some View {
        List(nodes, children: \.children, selection: $selection) { node in
            row(node)
        }
        .listStyle(.inset)
    }

    private func preview(_ files: [String: FileNode]) -> some View {
        FilePreview(node: selection.flatMap { files[$0] })
    }

    private func load() async {
        phase = .loading
        selection = nil
        guard let path else { phase = .missing; return }
        let result = await Task.detached(priority: .userInitiated) { () -> ([TreeItem], [String: FileNode], CGFloat) in
            let raw = FileTree.load(root: URL(fileURLWithPath: path))
            var files: [String: FileNode] = [:]
            let nodes = Self.prepare(raw, files: &files)
            return (nodes, files, Self.fitWidth(nodes))
        }.value
        if Task.isCancelled { return }
        if result.0.isEmpty { phase = .missing; return }
        treeFitWidth = result.2
        phase = .ready(nodes: result.0, files: result.1)
        selection = result.1.values.first { $0.name.caseInsensitiveCompare("SKILL.md") == .orderedSame
            && $0.url.deletingLastPathComponent().path == URL(fileURLWithPath: path).resolvingSymlinksInPath().path }?.id
    }

    /// Collects files by id and gives empty folders a placeholder child so they
    /// stay folders instead of rendering like leaf files.
    private nonisolated static func prepare(_ nodes: [FileNode], files: inout [String: FileNode]) -> [TreeItem] {
        nodes.map { n in
            if n.isDirectory {
                var kids = prepare(n.children ?? [], files: &files)
                if kids.isEmpty { kids = [TreeItem(id: n.id + "/·empty", node: nil, children: nil)] }
                return TreeItem(id: n.id, node: n, children: kids)
            }
            files[n.id] = n
            return TreeItem(id: n.id, node: n, children: nil)
        }
    }

    /// Widest row's natural width: indent per depth, disclosure, icon, name,
    /// and the trailing size label — plus the list's own insets. Clamped so the
    /// outline hugs its content without ever outgrowing the preview's share.
    private nonisolated static func fitWidth(_ items: [TreeItem]) -> CGFloat {
        min(max(widestRow(items, depth: 0) + Outline.padding, 160), 400)
    }

    private nonisolated static func widestRow(_ items: [TreeItem], depth: Int) -> CGFloat {
        var w: CGFloat = 0
        for item in items {
            var row = Outline.indent * CGFloat(depth) + Outline.icon + Theme.Space.sm
            if let node = item.node {
                row += textWidth(node.name, size: 13)
                if node.isDirectory {
                    row += Outline.disclosure
                } else {
                    row += Theme.Space.sm + textWidth(
                        ByteCountFormatter.string(fromByteCount: node.size, countStyle: .file), size: 12)
                }
            } else {
                row += textWidth("Empty folder", size: 13)
            }
            w = max(w, row)
            if let kids = item.children { w = max(w, widestRow(kids, depth: depth + 1)) }
        }
        return w
    }

    private nonisolated static func textWidth(_ s: String, size: CGFloat) -> CGFloat {
        (s as NSString).size(withAttributes: [.font: NSFont.systemFont(ofSize: size)]).width
    }

    /// Point measurements matching how `List` lays out an outline row.
    private enum Outline {
        /// Extra leading space per expanded depth level.
        static let indent: CGFloat = 14
        /// Chevron and its trailing gap on folders.
        static let disclosure: CGFloat = 14
        /// SF Symbol column.
        static let icon: CGFloat = 16
        /// `.inset` row padding plus room for the scroller.
        static let padding: CGFloat = 36
    }

    @ViewBuilder private func row(_ item: TreeItem) -> some View {
        if let node = item.node {
            rowContent(node)
        } else {
            Text("Empty folder").font(.callout).foregroundStyle(.tertiary).selectionDisabled()
        }
    }

    private func rowContent(_ node: FileNode) -> some View {
        Group {
            HStack(spacing: Theme.Space.sm) {
                Image(systemName: icon(for: node)).foregroundStyle(node.isDirectory ? Color.accentColor : .secondary)
                    .frame(width: 16).accessibilityHidden(true)
                Text(node.name).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 0)
                if !node.isDirectory {
                    Text(ByteCountFormatter.string(fromByteCount: node.size, countStyle: .file))
                        .font(.caption).monospacedDigit().foregroundStyle(.secondary)
                }
            }
            .contextMenu {
                Button { Finder.reveal(node.url.path) } label: { Label("Reveal in Finder", systemImage: Icon.reveal) }
                Button { Finder.copy(node.url.path) } label: { Label("Copy path", systemImage: Icon.copy) }
            }
            .accessibilityElement(children: .combine)
        }
    }

    private func icon(for node: FileNode) -> String {
        if node.isDirectory { return Icon.folderFilled }
        switch node.url.pathExtension.lowercased() {
        case "md", "markdown", "txt": return Icon.file
        case "png", "jpg", "jpeg", "gif", "svg", "webp", "heic", "ico": return Icon.image
        case "swift", "py", "js", "ts", "sh", "json", "toml", "yaml", "yml": return Icon.code
        default: return Icon.fileGeneric
        }
    }
}

struct TreeItem: Identifiable, Hashable, Sendable {
    let id: String
    let node: FileNode?
    var children: [TreeItem]?
}
