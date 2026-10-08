import SwiftUI
import SyncaKit

/// Scope switcher, project folder row and action buttons.
struct SidebarToolbar: View {
    @Environment(AppModel.self) private var model

    private var noProject: Bool { model.scope == .project && model.projectDir == nil }
    private var actionsDisabled: Bool { model.cliMissing || noProject }

    var body: some View {
        @Bindable var model = model
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            Picker("Scope", selection: $model.scope) {
                Label("User", systemImage: Icon.user).tag(SyncaScope.user)
                Label("Project", systemImage: Icon.project).tag(SyncaScope.project)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .help("Switch between user-level and project-level inventory")
            .accessibilityLabel("Scope")

            if model.scope == .project { projectRow }

            actions
        }
        .padding(.horizontal, Theme.Space.md)
        .padding(.vertical, Theme.Space.sm)
    }

    @ViewBuilder private var projectRow: some View {
        if model.projects.paths.isEmpty {
            Button { ProjectPicker.addProjects(to: model) } label: {
                Label("Add project folder", systemImage: Icon.folder).frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .help("Add a project folder to inspect")
        } else {
            HStack(spacing: Theme.Space.sm) {
                Menu {
                    ForEach(model.projects.paths, id: \.self) { path in
                        Button {
                            model.selectProject(path)
                        } label: {
                            if path == model.projects.active {
                                Label(ProjectList.name(of: path), systemImage: Icon.check)
                            } else {
                                Text(ProjectList.name(of: path))
                            }
                        }
                    }
                    Divider()
                    Button("Add project…") { ProjectPicker.addProjects(to: model) }
                    Button("Manage projects…") { model.showProjects = true }
                } label: {
                    Label(model.projectDir.map { ProjectList.name(of: $0) } ?? "Select project",
                          systemImage: Icon.folderFilled)
                        .lineLimit(1)
                }
                .menuStyle(.borderlessButton)
                .help(model.projectDir ?? "Select a project")
                .frame(maxWidth: .infinity, alignment: .leading)
                Button("Manage") { model.showProjects = true }
                    .controlSize(.small)
                    .help("Add, remove or switch project folders")
            }
        }
    }

    private var actions: some View {
        VStack(spacing: Theme.Space.sm) {
            Button { model.planSync(target: .all) } label: {
                Label("Sync all", systemImage: Icon.sync).frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(actionsDisabled || model.isBusy)
            .help("Preview a sync of all skills and MCP servers")

            LazyVGrid(columns: [GridItem(.adaptive(minimum: 112), spacing: Theme.Space.sm)],
                      spacing: Theme.Space.sm) {
                labeledButton("Install skill", Icon.install, disabled: actionsDisabled) { model.route = .installSkill }
                labeledButton("Add MCP", Icon.add, disabled: actionsDisabled) { model.route = .addMcp }
                Button { model.reload() } label: {
                    Label {
                        Text(model.isRefreshing ? "Reloading…" : "Reload")
                    } icon: {
                        ZStack {
                            if model.isRefreshing { ProgressView().controlSize(.small) }
                            else { Image(systemName: Icon.reload) }
                        }
                        .frame(width: 16, height: 16)
                    }
                    .frame(maxWidth: .infinity)
                }
                .disabled(model.isRefreshing)
                .help("Reload inventory")
                labeledButton("Updates", Icon.update, disabled: false, help: "Check for updates") { model.checkUpdate() }
            }
        }
        .buttonStyle(.bordered)
        .controlSize(.regular)
    }

    private func labeledButton(_ title: String, _ symbol: String, disabled: Bool, help: String? = nil,
                               action: @escaping () -> Void) -> some View {
        Button(action: action) { Label(title, systemImage: symbol).frame(maxWidth: .infinity) }
            .disabled(disabled)
            .help(help ?? title)
    }
}

private extension Icon {
    static let check = "checkmark"
}
