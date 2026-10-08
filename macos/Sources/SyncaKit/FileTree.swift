import Foundation

public struct FileNode: Identifiable, Hashable, Sendable {
    public let url: URL
    public let name: String
    public let isDirectory: Bool
    public let size: Int64
    /// nil = leaf file; non-nil (possibly empty) = directory with loaded children.
    public var children: [FileNode]?
    public var id: String { url.path }
}

public enum FileTree {
    public static let ignored: Set<String> = [".git", ".DS_Store", "node_modules", ".build", "__pycache__"]

    /// Directory tree rooted at `root` (symlinks resolved), folders first then
    /// alphabetical. Bounded so a pathological skill can't stall the UI.
    public static func load(root: URL, maxDepth: Int = 6, maxNodes: Int = 2_000) -> [FileNode] {
        var budget = maxNodes
        return children(of: root.resolvingSymlinksInPath(), depth: 0, maxDepth: maxDepth, budget: &budget)
    }

    private static func children(of dir: URL, depth: Int, maxDepth: Int, budget: inout Int) -> [FileNode] {
        let keys: [URLResourceKey] = [.isDirectoryKey, .fileSizeKey]
        guard depth < maxDepth,
              let urls = try? FileManager.default.contentsOfDirectory(
                at: dir, includingPropertiesForKeys: keys, options: [.skipsPackageDescendants])
        else { return [] }

        var nodes: [FileNode] = []
        for url in urls where !ignored.contains(url.lastPathComponent) {
            guard budget > 0 else { break }
            budget -= 1
            let values = try? url.resourceValues(forKeys: Set(keys))
            let isDir = values?.isDirectory ?? false
            nodes.append(FileNode(
                url: url, name: url.lastPathComponent, isDirectory: isDir,
                size: Int64(values?.fileSize ?? 0),
                children: isDir ? children(of: url, depth: depth + 1, maxDepth: maxDepth, budget: &budget) : nil))
        }
        return nodes.sorted {
            if $0.isDirectory != $1.isDirectory { return $0.isDirectory }
            return $0.name.localizedStandardCompare($1.name) == .orderedAscending
        }
    }

    /// Reads at most `limit` bytes as UTF-8; returns nil for binary files.
    public static func preview(of url: URL, limit: Int = 64 * 1024) -> String? {
        guard let fh = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? fh.close() }
        guard let data = try? fh.read(upToCount: limit) else { return nil }
        if data.contains(0) { return nil }
        return String(data: data, encoding: .utf8) ?? String(decoding: data, as: UTF8.self)
    }
}
