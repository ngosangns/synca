import SwiftUI
import SyncaKit

struct SidebarView: View {
    @Environment(AppModel.self) private var model
    @FocusState private var searchFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            PaneHeader(title: "Library", systemImage: nil) {
                WordmarkLogo(markSize: 20)
            }
            SidebarToolbar()
            ZStack(alignment: .top) {
                Divider()
                if model.isRefreshing { ProgressView().progressViewStyle(.linear).accessibilityLabel("Refreshing") }
            }
            .frame(height: 3)
            searchField
            content
        }
        .background(.background)
    }

    private var searchField: some View {
        @Bindable var model = model
        return HStack(spacing: Theme.Space.xs) {
            Image(systemName: Icon.search).foregroundStyle(.secondary).accessibilityHidden(true)
            TextField("Search", text: $model.filter)
                .textFieldStyle(.plain)
                .focused($searchFocused)
                .accessibilityLabel("Search skills and MCP servers")
            if !model.filter.isEmpty {
                Button { model.filter = "" } label: { Label("Clear", systemImage: Icon.close) }
                    .buttonStyle(.plain).foregroundStyle(.secondary).font(.caption)
                    .help("Clear search")
            }
            // Hidden Cmd-F shortcut target.
            Button("Focus Search") { searchFocused = true }
                .keyboardShortcut("f", modifiers: .command)
                .frame(width: 0, height: 0).opacity(0).accessibilityHidden(true)
        }
        .padding(.horizontal, Theme.Space.sm).padding(.vertical, 6)
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
        .padding(.horizontal, Theme.Space.md).padding(.bottom, Theme.Space.sm)
    }

    @ViewBuilder private var content: some View {
        switch model.inventory {
        case .idle, .loading:
            if model.skills.isEmpty && model.mcps.isEmpty {
                SkeletonList().frame(maxHeight: .infinity, alignment: .top)
            } else { list }
        case .needsProject:
            StateView(systemImage: Icon.project, title: "Choose a project folder",
                      message: "Project scope needs a folder to read skills and MCP servers from.") {
                Button { SidebarToolbar.chooseProjectFolder(model) } label: {
                    Label("Choose project folder", systemImage: Icon.folder)
                }
                .buttonStyle(.borderedProminent)
                .help("Choose the project folder to inspect")
            }
        case .failed(let message):
            StateView(systemImage: Icon.error, title: "Couldn’t load inventory", message: message, tint: .red) {
                Button { model.reload() } label: { Label("Retry", systemImage: Icon.reload) }
                    .buttonStyle(.borderedProminent).help("Try loading again")
            }
        case .ready:
            if model.skills.isEmpty && model.mcps.isEmpty {
                StateView(systemImage: Icon.skill, title: "No skills or MCP servers",
                          message: "Install a skill or add an MCP server to get started.") {
                    Button { model.route = .installSkill } label: {
                        Label("Install skill", systemImage: Icon.install)
                    }
                    .buttonStyle(.borderedProminent).disabled(model.cliMissing)
                    .help("Install a skill")
                }
            } else if model.filteredSkills.isEmpty && model.filteredMcps.isEmpty {
                StateView(systemImage: Icon.search, title: "No matches for “\(model.filter)”")
            } else { list }
        }
    }

    private var list: some View {
        @Bindable var model = model
        return List(selection: $model.selection) {
            if !model.filteredSkills.isEmpty {
                Section {
                    ForEach(model.filteredSkills) { skill in
                        SkillRow(skill: skill)
                            .tag(Selection.skill(skill.key))
                            .contextMenu { skillMenu(skill) }
                    }
                } header: { SectionHeading(title: "Skills", count: model.filteredSkills.count) }
            }
            if !model.filteredMcps.isEmpty {
                Section {
                    ForEach(model.filteredMcps) { mcp in
                        McpRow(mcp: mcp)
                            .tag(Selection.mcp(mcp.key))
                            .contextMenu { mcpMenu(mcp) }
                    }
                } header: { SectionHeading(title: "MCP Servers", count: model.filteredMcps.count) }
            }
        }
        .listStyle(.sidebar)
    }

    @ViewBuilder private func skillMenu(_ skill: SkillEntry) -> some View {
        Button { model.planSync(target: .skills, key: skill.key) } label: {
            Label("Sync this", systemImage: Icon.sync)
        }
        Divider()
        Button { model.confirmRemoveSkill(skill.key, purge: false) } label: {
            Label("Unlink", systemImage: Icon.unlink)
        }
        Button(role: .destructive) { model.confirmRemoveSkill(skill.key, purge: true) } label: {
            Label("Purge", systemImage: Icon.remove)
        }
    }

    @ViewBuilder private func mcpMenu(_ mcp: McpEntry) -> some View {
        Button { model.planSync(target: .mcp, key: mcp.key) } label: {
            Label("Sync this", systemImage: Icon.sync)
        }
        Divider()
        Button(role: .destructive) { model.confirmRemoveMcp(mcp.key) } label: {
            Label("Remove", systemImage: Icon.remove)
        }
    }
}
