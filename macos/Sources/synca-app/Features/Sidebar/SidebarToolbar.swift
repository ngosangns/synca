import SwiftUI
import AppKit
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
        if let dir = model.projectDir {
            HStack(spacing: Theme.Space.sm) {
                Image(systemName: Icon.folderFilled).foregroundStyle(.secondary).accessibilityHidden(true)
                Text(dir).font(.callout).lineLimit(1).truncationMode(.head).help(dir)
                    .accessibilityLabel("Project folder \(dir)")
                Spacer(minLength: 0)
                Button("Choose…", action: chooseProject)
                    .controlSize(.small)
                    .help("Choose a different project folder")
            }
        } else {
            Button(action: chooseProject) {
                Label("Choose project folder", systemImage: Icon.folder).frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .help("Choose the project folder to inspect")
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

            LazyVGrid(columns: [GridItem(.flexible(), spacing: Theme.Space.sm),
                                GridItem(.flexible())], spacing: Theme.Space.sm) {
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

    private func chooseProject() { Self.chooseProjectFolder(model) }

    static func chooseProjectFolder(_ model: AppModel) {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Choose"
        panel.message = "Choose a project folder"
        if panel.runModal() == .OK, let url = panel.url { model.setProject(url.path) }
    }
}
