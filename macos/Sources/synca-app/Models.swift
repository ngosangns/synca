import Foundation

enum SyncaScope: String, CaseIterable {
    case user, project
}

enum BoardKind: String {
    case skills, mcp
}

struct SkillPresence: Decodable {
    let agent: String
    let path: String
    let is_symlink: Bool
    let symlink_target: String?
    let content_hash: String?
}

struct SkillEntry: Decodable {
    let key: String
    let display_name: String?
    let description: String?
    let scope: String
    let presence: [SkillPresence]
    let mismatch: Bool
}

struct McpNormalized: Decodable {
    let transport: String?
    let command: [String]?
    let url: String?
    let args: [String]?
    let enabled: Bool?
    let env_keys: [String]?
}

struct McpPresence: Decodable {
    let agent: String
    let path: String
    let normalized: McpNormalized?
    let fingerprint: String?
}

struct McpEntry: Decodable {
    let key: String
    let scope: String
    let presence: [McpPresence]
    let mismatch: Bool
}

enum Selection {
    case skill(SkillEntry)
    case mcp(McpEntry)
}

/// One row in the conflict-resolution UI.
struct PlanConflict {
    enum Kind { case skill, mcp }
    let kind: Kind
    let key: String
    var policy: String = "skip" // skip | keep-source | keep-target
}

/// Parsed result of a `sync ... --dry-run` or install/add dry-run.
struct Plan {
    let title: String
    let output: String
    let ok: Bool
    var summary: [(kind: String, count: Int)] = []
    var conflicts: [PlanConflict] = []

    /// Sync plans can be applied through sync.apply; install/add plans apply
    /// through their own commands.
    enum Apply { case sync(target: String, key: String?), installSkill(source: String), addMcp(AddMcpPayload) }
    var apply: Apply?

    struct AddMcpPayload {
        let name, transport: String
        let command, url: String?
        let enabled: Bool
    }
}

struct PlanParser {
    /// Pull `{...}` JSON objects out of numbered plan lines.
    static func actions(in output: String) -> [[String: Any]] {
        output.split(whereSeparator: \.isNewline).compactMap { line in
            guard let range = line.range(of: #"\{.*\}"#, options: .regularExpression) else { return nil }
            return try? JSONSerialization.jsonObject(with: Data(line[range].utf8)) as? [String: Any]
        }
    }

    static func summary(of output: String) -> [(kind: String, count: Int)] {
        var counts: [String: Int] = [:]
        for a in actions(in: output) {
            if let k = a["kind"] as? String { counts[k, default: 0] += 1 }
        }
        return counts.sorted { $0.value > $1.value }.map { ($0.key, $0.value) }
    }

    static func conflicts(in output: String) -> [PlanConflict] {
        actions(in: output).compactMap { a in
            switch a["kind"] as? String {
            case "conflict_skill":
                guard let k = a["skill_key"] as? String else { return nil }
                return PlanConflict(kind: .skill, key: k)
            case "conflict_mcp":
                guard let k = a["server"] as? String else { return nil }
                return PlanConflict(kind: .mcp, key: k)
            default:
                return nil
            }
        }
    }
}

struct UpdateInfo: Decodable {
    let ok: Bool?
    let current: String?
    let latest: String?
    let update_available: Bool?
    let url: String?
    let message: String?
}
