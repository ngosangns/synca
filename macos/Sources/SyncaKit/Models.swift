import Foundation

public enum SyncaScope: String, CaseIterable, Sendable, Identifiable {
    case user, project
    public var id: String { rawValue }
    public var title: String { self == .user ? "User" : "Project" }
}

public enum SyncTarget: String, Sendable {
    case skills, mcp, all
}

// MARK: - Inventory (decoded from `synca ... list --json`, snake_case)

public struct SkillPresence: Decodable, Sendable, Hashable, Identifiable {
    public let agent: String
    public let path: String
    public let isSymlink: Bool
    public let symlinkTarget: String?
    public let contentHash: String?
    public var id: String { agent + "|" + path }
}

public struct SkillEntry: Decodable, Sendable, Hashable, Identifiable {
    public let key: String
    public let displayName: String?
    public let description: String?
    public let scope: String
    public let presence: [SkillPresence]
    public let mismatch: Bool
    public var id: String { key }
    public var title: String { displayName ?? key }

    /// Real directory to browse: prefer a non-symlink copy (the canonical one).
    public var primaryPath: String? {
        (presence.first { !$0.isSymlink } ?? presence.first)?.path
    }
}

public struct McpNormalized: Decodable, Sendable, Hashable {
    public let transport: String?
    public let command: [String]?
    public let url: String?
    public let args: [String]?
    public let enabled: Bool?
    public let envKeys: [String]?
}

public struct McpPresence: Decodable, Sendable, Hashable, Identifiable {
    public let agent: String
    public let path: String
    public let normalized: McpNormalized?
    public let fingerprint: String?
    public var id: String { agent + "|" + path }
}

public struct McpEntry: Decodable, Sendable, Hashable, Identifiable {
    public let key: String
    public let scope: String
    public let presence: [McpPresence]
    public let mismatch: Bool
    public var id: String { key }
}

public struct UpdateInfo: Decodable, Sendable, Equatable {
    public let ok: Bool?
    public let current: String?
    public let latest: String?
    public let updateAvailable: Bool?
    public let url: String?
    public let message: String?

    public init(ok: Bool?, current: String?, latest: String?, updateAvailable: Bool?,
                url: String?, message: String?) {
        self.ok = ok; self.current = current; self.latest = latest
        self.updateAvailable = updateAvailable; self.url = url; self.message = message
    }
}

// MARK: - Plans (dry-run results)

public enum ConflictPolicy: String, CaseIterable, Sendable, Identifiable {
    case skip
    case keepSource = "keep-source"
    case keepTarget = "keep-target"
    public var id: String { rawValue }
    public var title: String {
        switch self {
        case .skip: "Skip"
        case .keepSource: "Keep source"
        case .keepTarget: "Keep target"
        }
    }
}

public struct PlanConflict: Sendable, Identifiable, Hashable {
    public enum Kind: Sendable { case skill, mcp }
    public let kind: Kind
    public let key: String
    public var policy: ConflictPolicy = .skip
    public var id: String { "\(kind)|\(key)" }
    public init(kind: Kind, key: String, policy: ConflictPolicy = .skip) {
        self.kind = kind; self.key = key; self.policy = policy
    }
}

public struct AddMcpPayload: Sendable, Equatable {
    public let name: String
    public let transport: String
    public let command: String?
    public let url: String?
    public let enabled: Bool
    public init(name: String, transport: String, command: String?, url: String?, enabled: Bool) {
        self.name = name; self.transport = transport
        self.command = command; self.url = url; self.enabled = enabled
    }
}

public struct Plan: Sendable, Identifiable {
    public enum Apply: Sendable {
        case sync(target: SyncTarget, key: String?)
        case installSkill(source: String)
        case addMcp(AddMcpPayload)
    }
    public struct Chip: Sendable, Hashable { public let kind: String; public let count: Int }

    public let id = UUID()
    public let title: String
    public let output: String
    public let ok: Bool
    public let chips: [Chip]
    public var conflicts: [PlanConflict]
    public let apply: Apply

    public init(title: String, output: String, ok: Bool, apply: Apply) {
        self.title = title
        self.output = output
        self.ok = ok
        self.apply = apply
        let actions = PlanParser.actions(in: output)
        self.chips = PlanParser.chips(from: actions)
        self.conflicts = PlanParser.conflicts(from: actions)
    }
}

public enum PlanParser {
    /// Pulls `{...}` JSON objects out of numbered plan lines.
    public static func actions(in output: String) -> [[String: Any]] {
        output.split(whereSeparator: \.isNewline).compactMap { line in
            guard let open = line.firstIndex(of: "{"), let close = line.lastIndex(of: "}"),
                  open < close else { return nil }
            return try? JSONSerialization.jsonObject(with: Data(line[open...close].utf8)) as? [String: Any]
        }
    }

    public static func chips(from actions: [[String: Any]]) -> [Plan.Chip] {
        var counts: [String: Int] = [:]
        for a in actions { if let k = a["kind"] as? String { counts[k, default: 0] += 1 } }
        return counts.sorted { ($0.value, $1.key) > ($1.value, $0.key) }
            .map { Plan.Chip(kind: $0.key, count: $0.value) }
    }

    public static func conflicts(from actions: [[String: Any]]) -> [PlanConflict] {
        actions.compactMap { a in
            switch a["kind"] as? String {
            case "conflict_skill": (a["skill_key"] as? String).map { PlanConflict(kind: .skill, key: $0) }
            case "conflict_mcp": (a["server"] as? String).map { PlanConflict(kind: .mcp, key: $0) }
            default: nil
            }
        }
    }
}
