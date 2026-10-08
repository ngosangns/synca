import Foundation

public struct FileDiff: Identifiable, Sendable, Hashable {
    public enum Status: Sendable { case modified, added, removed, identical }
    public enum Content: Sendable, Hashable {
        case text([DiffRow])
        case binary
        case tooLarge
        case none           // identical files: nothing to show
    }
    public let path: String
    public let status: Status
    public let content: Content
    public let sourceSize: Int64?
    public let targetSize: Int64?
    public let additions: Int
    public let deletions: Int
    public var id: String { path }
}

public struct ConflictSide: Sendable, Hashable {
    public let agent: String
    public let path: String
    /// Content hash (skills) or fingerprint (MCP), shortened for display.
    public let digest: String
    public let fileCount: Int
    /// MCP only: the normalized config rendered as lines.
    public let summary: [String]
}

/// Source vs target copy of a conflicting skill / MCP server, plus their diff.
public struct ConflictComparison: Sendable {
    public let source: ConflictSide
    public let target: ConflictSide
    public let files: [FileDiff]
    /// Other distinct versions that are neither source nor target.
    public let otherVersions: [ConflictSide]
}

public enum ConflictCompare {
    static let maxFileBytes = 256 * 1024
    static let maxFiles = 2_000

    // MARK: Skills

    /// Mirrors the CLI: source = first (canonical) copy, target = first copy
    /// whose content hash differs from the source. Does file IO — call off-main.
    public static func skill(_ c: PlanConflict, entry: SkillEntry?) -> ConflictComparison? {
        guard c.kind == .skill, c.paths.count >= 2 else { return nil }
        let hashes = c.hashes.count == c.paths.count ? c.hashes : []
        let t = hashes.isEmpty ? 1 : (hashes.firstIndex { $0 != hashes[0] } ?? 1)
        func agent(_ path: String) -> String {
            entry?.presence.first { $0.path == path }?.agent
                ?? URL(fileURLWithPath: path).deletingLastPathComponent().deletingLastPathComponent().lastPathComponent
        }
        let srcURL = URL(fileURLWithPath: c.paths[0]).resolvingSymlinksInPath()
        let tgtURL = URL(fileURLWithPath: c.paths[t]).resolvingSymlinksInPath()
        let src = listFiles(srcURL), tgt = listFiles(tgtURL)

        var files: [FileDiff] = []
        for rel in Set(src.keys).union(tgt.keys).sorted(by: { $0.localizedStandardCompare($1) == .orderedAscending }) {
            files.append(compareFile(rel, source: src[rel], target: tgt[rel]))
        }
        files.sort { rank($0.status) < rank($1.status) }

        func side(_ i: Int, files n: Int) -> ConflictSide {
            ConflictSide(agent: agent(c.paths[i]), path: c.paths[i],
                         digest: hashes.isEmpty ? "" : String(hashes[i].prefix(8)), fileCount: n, summary: [])
        }
        var others: [ConflictSide] = []
        var seen: Set<String> = hashes.isEmpty ? [] : [hashes[0], hashes[t]]
        for i in c.paths.indices where !hashes.isEmpty && !seen.contains(hashes[i]) {
            seen.insert(hashes[i])
            others.append(side(i, files: listFiles(URL(fileURLWithPath: c.paths[i]).resolvingSymlinksInPath()).count))
        }
        return ConflictComparison(source: side(0, files: src.count), target: side(t, files: tgt.count),
                                  files: files, otherVersions: others)
    }

    private static func rank(_ s: FileDiff.Status) -> Int {
        switch s { case .modified: 0; case .added: 1; case .removed: 2; case .identical: 3 }
    }

    /// Relative path -> file URL. Relative names are built from path components
    /// below the root, because the enumerator may report an aliased prefix
    /// (`/private/var` vs `/var`) that defeats string prefix stripping.
    static func listFiles(_ root: URL) -> [String: URL] {
        var out: [String: URL] = [:]
        let rootCount = root.resolvingSymlinksInPath().pathComponents.count
        guard let en = FileManager.default.enumerator(
            at: root.resolvingSymlinksInPath(), includingPropertiesForKeys: [.isRegularFileKey], options: []) else { return out }
        for case let url as URL in en {
            if FileTree.ignored.contains(url.lastPathComponent) { en.skipDescendants(); continue }
            guard (try? url.resourceValues(forKeys: [.isRegularFileKey]).isRegularFile) == true,
                  out.count < maxFiles else { continue }
            let comps = url.resolvingSymlinksInPath().pathComponents
            guard comps.count > rootCount else { continue }
            out[comps[rootCount...].joined(separator: "/")] = url
        }
        return out
    }

    static func read(_ url: URL) -> (data: Data?, size: Int64, tooLarge: Bool) {
        let size = Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
        if size > maxFileBytes { return (nil, size, true) }
        return (try? Data(contentsOf: url), size, false)
    }

    static func isBinary(_ d: Data) -> Bool { d.prefix(8192).contains(0) }

    static func compareFile(_ rel: String, source: URL?, target: URL?) -> FileDiff {
        let a = source.map(read), b = target.map(read)
        let status: FileDiff.Status = a == nil ? .added : b == nil ? .removed
            : (a!.data != nil && a!.data == b!.data ? .identical : .modified)
        if status == .identical {
            return FileDiff(path: rel, status: .identical, content: FileDiff.Content.none,
                            sourceSize: a?.size, targetSize: b?.size, additions: 0, deletions: 0)
        }
        if (a?.tooLarge ?? false) || (b?.tooLarge ?? false) {
            return FileDiff(path: rel, status: status, content: .tooLarge,
                            sourceSize: a?.size, targetSize: b?.size, additions: 0, deletions: 0)
        }
        if let d = a?.data, isBinary(d) { return binary(rel, status, a, b) }
        if let d = b?.data, isBinary(d) { return binary(rel, status, a, b) }
        let old = a?.data.map { String(decoding: $0, as: UTF8.self) } ?? ""
        let new = b?.data.map { String(decoding: $0, as: UTF8.self) } ?? ""
        let all = TextDiff.rows(old: TextDiff.lines(of: old), new: TextDiff.lines(of: new))
        return FileDiff(path: rel, status: status, content: .text(TextDiff.collapse(all)),
                        sourceSize: a?.size, targetSize: b?.size,
                        additions: all.filter { $0.kind == .added }.count,
                        deletions: all.filter { $0.kind == .removed }.count)
    }

    private static func binary(_ rel: String, _ s: FileDiff.Status,
                               _ a: (data: Data?, size: Int64, tooLarge: Bool)?,
                               _ b: (data: Data?, size: Int64, tooLarge: Bool)?) -> FileDiff {
        FileDiff(path: rel, status: s, content: .binary, sourceSize: a?.size, targetSize: b?.size, additions: 0, deletions: 0)
    }

    // MARK: MCP

    /// Same selection rule as the CLI, using the inventory entry for contents.
    public static func mcp(_ c: PlanConflict, entry: McpEntry?) -> ConflictComparison? {
        guard c.kind == .mcp, let entry, entry.presence.count >= 2 else { return nil }
        let fps = c.fingerprints.count == entry.presence.count ? c.fingerprints
            : entry.presence.map { $0.fingerprint ?? "" }
        let t = fps.firstIndex { $0 != fps[0] } ?? 1
        func side(_ i: Int) -> ConflictSide {
            let p = entry.presence[i]
            return ConflictSide(agent: p.agent, path: p.path, digest: String(fps[i].prefix(8)), fileCount: 1,
                                summary: p.normalized?.describeLines() ?? ["(unreadable)"])
        }
        let s = side(0), tg = side(t)
        let all = TextDiff.rows(old: s.summary, new: tg.summary)
        let status: FileDiff.Status = all.contains { $0.kind != .same } ? .modified : .identical
        let file = FileDiff(path: "configuration", status: status,
                            content: status == .identical ? FileDiff.Content.none : .text(TextDiff.collapse(all, context: 50)),
                            sourceSize: nil, targetSize: nil,
                            additions: all.filter { $0.kind == .added }.count,
                            deletions: all.filter { $0.kind == .removed }.count)
        var seen: Set<String> = [fps[0], fps[t]]
        var others: [ConflictSide] = []
        for i in entry.presence.indices where !seen.contains(fps[i]) { seen.insert(fps[i]); others.append(side(i)) }
        return ConflictComparison(source: s, target: tg, files: [file], otherVersions: others)
    }
}

extension McpNormalized {
    /// Stable, line-per-value rendering so config differences diff cleanly.
    public func describeLines() -> [String] {
        var out: [String] = []
        if let transport { out.append("transport: \(transport)") }
        if let command, !command.isEmpty { out.append("command: \(command.joined(separator: " "))") }
        if let args, !args.isEmpty { out.append("args:"); out += args.map { "  \($0)" } }
        if let url { out.append("url: \(url)") }
        if let enabled { out.append("enabled: \(enabled)") }
        if let envKeys, !envKeys.isEmpty { out.append("env keys: \(envKeys.sorted().joined(separator: ", "))") }
        return out
    }
}
