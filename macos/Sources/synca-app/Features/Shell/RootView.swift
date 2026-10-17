import SwiftUI
import SyncaKit

/// Library | Detail, plus Activity or Settings when expanded. The right
/// column of buttons stays put so either panel can be opened again.
struct RootView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        @Bindable var model = model
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
        .overlay(alignment: .bottom) {
            if let toast = model.toast {
                ToastView(toast: toast) { model.dismissToast() }
                    .padding(.bottom, 40)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.snappy(duration: 0.25), value: model.toast)
        .sheet(isPresented: $model.showProjects) { ProjectManagerSheet() }
        .confirmationDialog(
            model.confirmation?.title ?? "",
            isPresented: Binding(get: { model.confirmation != nil }, set: { if !$0 { model.confirmation = nil } }),
            titleVisibility: .visible,
            presenting: model.confirmation
        ) { c in
            Button(c.confirmTitle, role: c.destructive ? .destructive : nil) { c.action() }
            Button("Cancel", role: .cancel) {}
        } message: { c in Text(c.message) }
    }
}

private struct CLIMissingBanner: View {
    @Environment(AppModel.self) private var model
    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Image(systemName: Icon.warning).foregroundStyle(.orange)
            Text(CLIError.notFound.localizedDescription).font(.callout)
            Spacer()
            Button("Retry") { model.start() }
        }
        .padding(.horizontal, Theme.Space.lg).padding(.vertical, Theme.Space.sm)
        .background(.orange.opacity(0.12))
        .overlay(alignment: .bottom) { Divider() }
    }
}

/// Bottom status bar: what is running right now + CLI version.
struct StatusBar: View {
    @Environment(AppModel.self) private var model
    var body: some View {
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
        .font(.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, Theme.Space.lg)
        .frame(height: 24)
        .background(.bar)
        .overlay(alignment: .top) { Divider() }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(model.operations.last.map { "Working: \($0.title)" } ?? "Ready")
    }
}
