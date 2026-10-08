import Foundation

struct LogEntry: Codable {
    let id: Int
    let ts: TimeInterval
    let scope: String
    let command: String
    let status: String   // ok | dry-run | error
    let exit: Int
    let output: String

    var timeString: String {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .abbreviated
        return f.localizedString(for: Date(timeIntervalSince1970: ts), relativeTo: Date())
    }
}

/// Append-only JSONL store at ~/Library/Application Support/synca/command-log.jsonl.
/// The log pane pages older entries lazily — never loads the whole file.
final class LogStore: @unchecked Sendable {
    static let shared = LogStore()
    static let changed = Notification.Name("LogStore.changed")

    private let url: URL
    private var counter: Int
    private let pageSize = 30

    private init() {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("synca", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        url = dir.appendingPathComponent("command-log.jsonl")
        counter = (try? Self.lastId(in: url)) ?? 0
    }

    private static func lastId(in url: URL) throws -> Int {
        guard let fh = try? FileHandle(forReadingFrom: url) else { return 0 }
        defer { try? fh.close() }
        var last = 0
        let data = fh.readDataToEndOfFile()
        for line in data.split(separator: 0x0A).suffix(5) {
            if let e = try? JSONDecoder().decode(LogEntry.self, from: Data(line)) { last = e.id }
        }
        return last
    }

    /// Decode the whole file only when paging backwards; bounded by pageSize
    /// chunks appended from the end.
    private func allEntries() -> [LogEntry] {
        guard let data = try? Data(contentsOf: url) else { return [] }
        return data.split(separator: 0x0A).compactMap {
            try? JSONDecoder().decode(LogEntry.self, from: Data($0))
        }
    }

    /// Most recent page (newest first).
    func recent() -> [LogEntry] { Array(allEntries().suffix(pageSize).reversed()) }

    /// Older page before `id` (newest first), for lazy scroll loading.
    func page(before id: Int) -> [LogEntry] {
        Array(allEntries().filter { $0.id < id }.suffix(pageSize).reversed())
    }

    /// Entries newer than `id` (newest first), for live refresh.
    func page(after id: Int) -> [LogEntry] {
        allEntries().filter { $0.id > id }.reversed()
    }

    private let lock = NSLock()

    func append(command: String, scope: String, status: String, exit: Int, output: String) {
        lock.lock()
        defer { lock.unlock() }
        counter += 1
        let entry = LogEntry(id: counter, ts: Date().timeIntervalSince1970,
                             scope: scope, command: command, status: status,
                             exit: exit, output: output)
        guard var data = try? JSONEncoder().encode(entry) else { return }
        data.append(0x0A)
        if let fh = try? FileHandle(forWritingTo: url) {
            fh.seekToEndOfFile()
            fh.write(data)
            try? fh.close()
        } else {
            try? data.write(to: url)
        }
        NotificationCenter.default.post(name: Self.changed, object: nil)
    }

    func clear() {
        try? FileManager.default.removeItem(at: url)
        counter = 0
        NotificationCenter.default.post(name: Self.changed, object: nil)
    }
}
