import SwiftUI
import SyncaKit
import NSShell

/// Left dock, center pane, and one right dock (Activity or Settings).
/// Toast and confirm sit on `WindowShell`. The project sheet is a flow.
struct RootView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        @Bindable var model = model
        WindowShell(
            toast: model.toast,
            dismissToast: { model.dismissToast() },
            confirmation: model.confirmation,
            clearConfirmation: { model.confirmation = nil }
        ) {
            VStack(spacing: 0) {
                if model.cliMissing { CLIMissingBanner() }
                HStack(spacing: 0) {
                    HSplitView {
                        SidebarView()
                            .frame(minWidth: 260, idealWidth: 300, maxWidth: 420)
                        DetailView()
                            .frame(minWidth: 420, maxWidth: .infinity)
                        if model.showSettings || model.showActivity {
                            Group {
                                if model.showSettings { SettingsView() } else { ActivityView() }
                            }
                            .frame(minWidth: 260, idealWidth: 320, maxWidth: 460)
                            .transition(.move(edge: .trailing).combined(with: .opacity))
                        }
                    }
                    RightColumnButtons()
                }
                .animation(reduceMotion ? nil : .snappy(duration: 0.22), value: model.rightPanelID)
                StatusBar()
            }
        }
        .sheet(isPresented: $model.showProjects) { ProjectManagerSheet() }
    }
}

private struct CLIMissingBanner: View {
    @Environment(AppModel.self) private var model
    var body: some View {
        MissingToolBanner(message: CLIError.notFound.localizedDescription) { model.start() }
    }
}

/// Bottom status bar: what is running right now + CLI version.
struct StatusBar: View {
    @Environment(AppModel.self) private var model
    var body: some View {
        StatusStrip(accessibilityLabel: model.operations.last.map { "Working: \($0.title)" } ?? "Ready") {
            HStack(spacing: Theme.Space.sm) {
                if let op = model.operations.last {
                    ProgressView().controlSize(.small)
                    Text(op.title + (model.operations.count > 1 ? " (+\(model.operations.count - 1))" : "") + "…")
                        .lineLimit(1)
                } else {
                    Image(systemName: Icon.success).foregroundStyle(.green).imageScale(.small)
                    Text("Ready")
                }
                Spacer()
                if let v = model.cliVersion { Text(v).monospacedDigit() }
            }
        }
    }
}
