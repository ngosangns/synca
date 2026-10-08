import SwiftUI
import AppKit
import SyncaKit

struct UpdateView: View {
    let state: UpdateState
    @Environment(AppModel.self) private var model

    var body: some View {
        switch state {
        case .checking:
            WorkingView(message: "Checking for updates…")
        case .installing:
            WorkingView(message: "Installing update…")
        case .failed(let message):
            StateView(systemImage: Icon.error, title: "Update failed", message: message, tint: .red) {
                HStack {
                    Button("Close") { model.route = .item }
                    Button("Retry") { model.checkUpdate() }
                        .buttonStyle(.borderedProminent)
                        .disabled(model.isBusy)
                }
            }
        case .result(let info):
            result(info)
        }
    }

    private func result(_ info: UpdateInfo) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                Text("Software update").font(.title2.weight(.semibold)).accessibilityAddTraits(.isHeader)

                Card {
                    Grid(alignment: .leading, horizontalSpacing: Theme.Space.xl, verticalSpacing: Theme.Space.sm) {
                        GridRow {
                            Text("Current").foregroundStyle(.secondary)
                            Text(info.current ?? "—").monospacedDigit().textSelection(.enabled)
                        }
                        GridRow {
                            Text("Latest").foregroundStyle(.secondary)
                            Text(info.latest ?? "—").monospacedDigit().textSelection(.enabled)
                        }
                    }
                    .font(.body)
                }

                if info.updateAvailable == true {
                    banner(Icon.update, .accentColor, "Update available",
                           info.latest.map { "Version \($0) is ready to install." } ?? "A newer version is ready to install.")
                } else if info.ok == true {
                    banner(Icon.success, .green, "You're up to date", "No newer version is available.")
                }

                if info.ok != true, let message = info.message, !message.isEmpty {
                    banner(Icon.error, .red, "Couldn't check for updates", message)
                }

                HStack {
                    Button("Check again") { model.checkUpdate() }
                        .disabled(model.isBusy)
                    if let s = info.url, let url = URL(string: s) {
                        Button("Open release page") { NSWorkspace.shared.open(url) }
                    }
                    Spacer()
                    Button("Close") { model.route = .item }
                        .keyboardShortcut(.cancelAction)
                    if info.updateAvailable == true {
                        Button {
                            model.installUpdate()
                        } label: {
                            Label("Install update", systemImage: Icon.install)
                        }
                        .buttonStyle(.borderedProminent)
                        .keyboardShortcut(.defaultAction)
                        .disabled(model.isBusy)
                    }
                }
            }
            .padding(Theme.Space.lg)
            .frame(maxWidth: 640, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func banner(_ icon: String, _ tint: Color, _ title: String, _ message: String) -> some View {
        HStack(alignment: .top, spacing: Theme.Space.sm) {
            Image(systemName: icon).foregroundStyle(tint).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(message).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
            }
            Spacer(minLength: 0)
        }
        .padding(Theme.Space.md)
        .background(tint.opacity(0.12), in: RoundedRectangle(cornerRadius: Theme.Radius.md))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.md).strokeBorder(tint.opacity(0.4)))
        .accessibilityElement(children: .combine)
    }
}
