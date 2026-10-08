import Foundation

public struct LogEntry: Codable, Sendable, Identifiable, Hashable {
    public let id: Int
    public let ts: TimeInterval
    public let scope: String
    public let command: String
    public let status: String   // ok | dry-run | error
    public let exit: Int
    public let output: String
    public var date: Date { Date(timeIntervalSince1970: ts) }
}

/// Append-only JSONL command log at ~/Library/Application Support/synca/command-log.jsonl.
/// Actor-isolated; never blocks the main thread. Reads only the tail of the
/// file for the newest page and walks backwards in chunks for older pages.
public actor LogStore {
    public static let shared = LogStore()

    private let url: URL
    private var counter: Int?

    public init(directory: URL? = nil) {
        let dir = directory ?? FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("synca", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        url = dir.appendingPathComponent("command-log.jsonl")
    }

    @discardableResult
    public func append(command: String, scope: String, status: String, exit: Int, output: String) -> LogEntry {
        let id = nextId()
        let entry = LogEntry(id: id, ts: Date().timeIntervalSince1970, scope: scope,
                             command: command, status: status, exit: exit, output: output)
        guard var data = try? JSONEncoder().encode(entry) else { return entry }
        data.append(0x0A)
        if let fh = try? FileHandle(forWritingTo: url) {
            defer { try? fh.close() }
            _ = try? fh.seekToEnd()
            try? fh.write(contentsOf: data)
        } else {
            try? data.write(to: url)
        }
        return entry
    }

    /// Newest-first page of entries with `id < before` (or the newest page).
    public func page(before: Int? = nil, limit: Int = 40) -> [LogEntry] {
        guard let fh = try? FileHandle(forReadingFrom: url) else { return [] }
        defer { try? fh.close() }
        guard let size = try? fh.seekToEnd(), size > 0 else { return [] }

        var window: UInt64 = 64 * 1024
        while true {
            let start = size > window ? size - window : 0
            try? fh.seek(toOffset: start)
            let data = (try? fh.readToEnd()) ?? Data()
            var lines = data.split(separator: 0x0A, omittingEmptySubsequences: true)
            if start > 0 { lines = Array(lines.dropFirst()) } // possibly cut mid-line
            let entries = lines.compactMap { try? JSONDecoder().decode(LogEntry.self, from: Data($0)) }
            let eligible = entries.filter { before == nil || $0.id < before! }
            if eligible.count >= limit || start == 0 {
                return Array(eligible.suffix(limit).reversed())
            }
            window *= 4
        }
    }

    public func clear() {
        try? FileManager.default.removeItem(at: url)
        counter = 0
    }

    private func nextId() -> Int {
        if counter == nil { counter = page(limit: 1).first?.id ?? 0 }
        counter! += 1
        return counter!
    }
}
