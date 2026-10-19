import SwiftUI
import NSShell

/// One command, one title, one shortcut, one call.
/// Menus in `SyncaApp` only bind these. Same split as Zed's `actions!` list
/// and the menu code that references it.
enum SyncaCommand: String {
    case reload
    case toggleActivity
    case toggleSettings
    case syncAll
    case installSkill
    case addMcp
    case checkUpdate
    case userScope
    case projectScope

    @MainActor
    func title(_ model: AppModel) -> String {
        switch self {
        case .reload: "Reload Inventory"
        case .toggleActivity: model.showActivity ? "Hide Activity" : "Show Activity"
        case .toggleSettings: model.showSettings ? "Hide Settings" : "Settings"
        case .syncAll: "Sync Everything…"
        case .installSkill: "Install Skill…"
        case .addMcp: "Add MCP Server…"
        case .checkUpdate: "Check for Updates…"
        case .userScope: "User Scope"
        case .projectScope: "Project Scope"
        }
    }

    /// `nil` when the menu item has no key.
    var shortcut: KeyEquivalent? {
        switch self {
        case .reload: "r"
        case .toggleActivity: "l"
        case .toggleSettings: ","
        case .syncAll: "s"
        case .installSkill: "i"
        case .addMcp: "n"
        case .checkUpdate: nil
        case .userScope: "1"
        case .projectScope: "2"
        }
    }

    var modifiers: EventModifiers {
        switch self {
        case .toggleActivity: [.command, .option]
        case .syncAll: [.command, .shift]
        default: .command
        }
    }

    @MainActor
    func run(_ model: AppModel) {
        switch self {
        case .reload: model.reload()
        case .toggleActivity: model.showActivity.toggle()
        case .toggleSettings: model.showSettings.toggle()
        case .syncAll: model.planSync(target: .all)
        case .installSkill: model.route = .installSkill
        case .addMcp: model.route = .addMcp
        case .checkUpdate: model.checkUpdate()
        case .userScope: model.scope = .user
        case .projectScope: model.scope = .project
        }
    }
}
