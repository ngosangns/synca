import Foundation

/// Ordered, de-duplicated set of project folders plus the active one.
/// Pure value type so the rules are unit-testable; persistence lives in the app.
public struct ProjectList: Equatable, Sendable {
    public private(set) var paths: [String]
    public private(set) var active: String?

    public init(paths: [String] = [], active: String? = nil) {
        self.paths = []
        self.active = nil
        for p in paths { _ = insert(p) }
        if let active { add(active) }
    }

    public static func normalize(_ path: String) -> String {
        let p = URL(fileURLWithPath: (path as NSString).expandingTildeInPath).standardizedFileURL.path
        return p.count > 1 && p.hasSuffix("/") ? String(p.dropLast()) : p
    }

    public static func name(of path: String) -> String {
        let n = URL(fileURLWithPath: path).lastPathComponent
        return n.isEmpty ? path : n
    }

    @discardableResult
    private mutating func insert(_ path: String) -> String {
        let p = Self.normalize(path)
        if !paths.contains(p) { paths.append(p) }
        return p
    }

    /// Adds (or re-finds) a project and makes it active.
    public mutating func add(_ path: String) { active = insert(path) }

    /// Makes an existing project active; unknown paths are ignored.
    public mutating func select(_ path: String) {
        let p = Self.normalize(path)
        if paths.contains(p) { active = p }
    }

    /// Removes a project. If it was active, the neighbour (or nil) takes over.
    public mutating func remove(_ path: String) {
        let p = Self.normalize(path)
        guard let i = paths.firstIndex(of: p) else { return }
        paths.remove(at: i)
        if active == p { active = paths.isEmpty ? nil : paths[min(i, paths.count - 1)] }
    }
}
