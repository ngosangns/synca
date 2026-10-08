import SwiftUI
import SyncaKit

/// Monospaced, selectable preview of a file; loaded off the main thread.
struct FilePreview: View {
    let node: FileNode?

    private enum Content {
        case loading
        case text(String, truncated: Bool)
        case binary
        case unreadable
    }

    @State private var content: Content = .loading
    private let limit = 64 * 1024

    var body: some View {
        Group {
            if let node {
                VStack(alignment: .leading, spacing: 0) {
                    HStack {
                        Text(node.name).font(.caption.weight(.semibold)).lineLimit(1).truncationMode(.middle)
                        Spacer()
                        Button { Finder.copy(node.url.path) } label: { Label("Copy path", systemImage: Icon.copy) }
                            .buttonStyle(.borderless).help("Copy path to clipboard")
                        RevealButton(path: node.url.path)
                    }
                    .padding(.horizontal, Theme.Space.sm).padding(.vertical, Theme.Space.xs)
                    Divider()
                    body(for: node)
                }
                .task(id: node.id) { await load(node) }
            } else {
                StateView(systemImage: Icon.fileGeneric, title: "No file selected")
            }
        }
    }

    @ViewBuilder private func body(for node: FileNode) -> some View {
        switch content {
        case .loading:
            ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
        case .binary:
            note("Binary file", system: Icon.fileGeneric)
        case .unreadable:
            note("Unable to read file", system: Icon.warning)
        case .text(let s, let truncated):
            VStack(alignment: .leading, spacing: 0) {
                if truncated {
                    Label("Too large (showing first 64 KB)", systemImage: Icon.info)
                        .font(.caption).foregroundStyle(.secondary)
                        .padding(.horizontal, Theme.Space.sm).padding(.vertical, Theme.Space.xs)
                }
                ScrollView([.vertical, .horizontal]) {
                    Text(s).font(.system(.caption, design: .monospaced))
                        .textSelection(.enabled)
                        .padding(Theme.Space.sm)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    private func note(_ text: String, system: String) -> some View {
        Label(text, systemImage: system).font(.callout).foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func load(_ node: FileNode) async {
        content = .loading
        let url = node.url, size = node.size, limit = limit
        let result = await Task.detached(priority: .userInitiated) { () -> Content in
            guard let s = FileTree.preview(of: url, limit: limit) else {
                return FileManager.default.isReadableFile(atPath: url.path) ? .binary : .unreadable
            }
            return .text(s, truncated: size > Int64(limit))
        }.value
        if !Task.isCancelled { content = result }
    }
}
