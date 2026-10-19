import SwiftUI
import SyncaKit
import NSShell

/// Right-column settings: which providers Synca talks to, and wiping one provider's copies.
struct SettingsView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(spacing: 0) {
            PaneHeader(title: "Settings", systemImage: Icon.settings)
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Space.lg) {
                    providers
                    cli
                }
                .padding(Theme.Space.md)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private var providers: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            SectionHeading(title: "Providers")
            Text("Turn a provider off to skip it during sync, install, and remove. Delete clears that provider’s skills and MCP entries in the \(model.scope.title) scope. The canonical hub is kept.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            VStack(spacing: 0) {
                ForEach(Provider.all) { provider in
                    ProviderRow(provider: provider)
                    if provider.id != Provider.all.last?.id { Divider().padding(.leading, Theme.Space.sm) }
                }
            }
            .background(.background.secondary, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.md).strokeBorder(.separator))
        }
    }

    private var cli: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            SectionHeading(title: "CLI")
            Card(padding: Theme.Space.md) {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    LabeledContent("Version") {
                        Text(model.cliVersion ?? (model.cliChecked ? "Not found" : "Checking…"))
                            .foregroundStyle(model.cliMissing ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
                    }
                    LabeledContent("Path") {
                        Text(cliPath)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                            .truncationMode(.middle)
                            .textSelection(.enabled)
                    }
                    .font(.caption)
                    if model.cliMissing {
                        Button("Retry") { model.start() }
                            .help("Look for the synca CLI again")
                    } else {
                        Button("Check for updates") { model.checkUpdate() }
                            .disabled(model.isBusy)
                            .help("Check whether a newer synca is available")
                    }
                }
                .font(.callout)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private var cliPath: String {
        if model.cli.baseArgs.isEmpty { return model.cli.executable.path }
        return ([model.cli.executable.path] + model.cli.baseArgs).joined(separator: " ")
    }
}

private struct ProviderRow: View {
    @Environment(AppModel.self) private var model
    let provider: Provider

    private var usage: (skills: Int, mcps: Int) { model.providerUsage(provider.id) }
    private var enabled: Binding<Bool> {
        Binding(
            get: { model.isProviderEnabled(provider.id) },
            set: { model.setProviderEnabled(provider.id, $0) }
        )
    }
    private var canDelete: Bool {
        !model.cliMissing && !model.isBusy && (model.scope == .user || model.projectDir != nil)
    }
    private var deleteHelp: String {
        if model.cliMissing { return "The synca CLI was not found" }
        if model.isBusy { return "Wait for the current operation to finish" }
        if model.scope == .project, model.projectDir == nil {
            return "Choose a project folder before deleting project data"
        }
        return "Delete \(provider.title) data in the \(model.scope.title) scope"
    }

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            VStack(alignment: .leading, spacing: 2) {
                Text(provider.title)
                    .font(.callout.weight(.medium))
                    .foregroundStyle(enabled.wrappedValue ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                Text(usageText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button { model.confirmClearProvider(provider.id) } label: {
                Label("Delete \(provider.title) data", systemImage: Icon.remove)
                    .labelStyle(.iconOnly)
            }
            .buttonStyle(.borderless)
            .foregroundStyle(canDelete ? Color.red : Color.secondary)
            .disabled(!canDelete)
            .help(deleteHelp)
            Toggle("Enable \(provider.title)", isOn: enabled)
                .toggleStyle(.switch)
                .controlSize(.small)
                .labelsHidden()
                .help(enabled.wrappedValue ? "\(provider.title) is included" : "\(provider.title) is skipped")
        }
        .padding(.horizontal, Theme.Space.sm)
        .padding(.vertical, Theme.Space.sm)
        .accessibilityElement(children: .contain)
    }

    private var usageText: String {
        let (skills, mcps) = usage
        return "\(skills) \(skills == 1 ? "skill" : "skills") · \(mcps) MCP"
    }
}
