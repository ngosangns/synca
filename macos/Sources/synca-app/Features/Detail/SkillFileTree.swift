import SwiftUI
import SyncaKit

/// File explorer for a skill directory: outline on top, preview below.
struct SkillFileTree: View {
    let skillID: String
    let path: String?

    private enum Phase {
        case loading
        case missing
        case ready(nodes: [TreeItem], files: [String: FileNode])
    }

    @State private var phase: Phase = .loading
    @State private var selection: String?

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
                VSplitView {
                    List(nodes, children: \.children, selection: $selection) { node in
                        row(node)
                    }
                    .listStyle(.inset)
                    .frame(minHeight: 100)
                    FilePreview(node: selection.flatMap { files[$0] })
                        .frame(minHeight: 100)
                }
            }
        }
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .task(id: skillID + "|" + (path ?? "")) { await load() }
    }

    private func load() async {
        phase = .loading
        selection = nil
        guard let path else { phase = .missing; return }
        let result = await Task.detached(priority: .userInitiated) { () -> ([TreeItem], [String: FileNode]) in
            let raw = FileTree.load(root: URL(fileURLWithPath: path))
            var files: [String: FileNode] = [:]
            let nodes = Self.prepare(raw, files: &files)
            return (nodes, files)
        }.value
        if Task.isCancelled { return }
        if result.0.isEmpty { phase = .missing; return }
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
