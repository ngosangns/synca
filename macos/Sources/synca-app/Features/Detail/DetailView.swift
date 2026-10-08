import SwiftUI
import SyncaKit

/// Second column: heading + content for the current `DetailRoute`.
struct DetailView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(spacing: 0) {
            PaneHeader(title: headerTitle, systemImage: headerIcon)
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .id(routeID)
                .transition(.opacity)
        }
        .animation(.easeInOut(duration: 0.18), value: routeID)
    }

    @ViewBuilder private var content: some View {
        switch model.route {
        case .item:
            if let skill = model.selectedSkill {
                SkillDetailView(skill: skill)
            } else if let mcp = model.selectedMcp {
                McpDetailView(mcp: mcp)
            } else {
                StateView(systemImage: Icon.sparkles, title: "Select a skill or MCP server",
                          message: "Pick an item from the Library to inspect it.\nUse ⌘F to search the Library.") {
                    LogoMark(size: 56)
                }
            }
        case .installSkill: InstallSkillForm()
        case .addMcp: AddMcpForm()
        case .working(let msg): WorkingView(message: msg)
        case .plan(let p): PlanView(plan: p)
        case .update(let s): UpdateView(state: s)
        }
    }

    private var headerTitle: String {
        switch model.route {
        case .item:
            if let s = model.selectedSkill { return s.title }
            if let m = model.selectedMcp { return m.key }
            return "Details"
        case .installSkill: return "Install skill"
        case .addMcp: return "Add MCP server"
        case .working: return "Working"
        case .plan(let p): return p.title
        case .update: return "Update"
        }
    }

    private var headerIcon: String? {
        switch model.route {
        case .item:
            if model.selectedSkill != nil { return Icon.skill }
            if model.selectedMcp != nil { return Icon.mcp }
            return nil
        case .installSkill: return Icon.install
        case .addMcp: return Icon.add
        case .working: return Icon.sync
        case .plan: return Icon.sync
        case .update: return Icon.update
        }
    }

    /// Identity used for transitions; changes when route kind or selected item changes.
    private var routeID: String {
        switch model.route {
        case .item:
            if let s = model.selectedSkill { return "skill:" + s.id }
            if let m = model.selectedMcp { return "mcp:" + m.id }
            return "empty"
        case .installSkill: return "install"
        case .addMcp: return "addmcp"
        case .working(let m): return "working:" + m
        case .plan(let p): return "plan:" + p.id.uuidString
        case .update: return "update"
        }
    }
}
