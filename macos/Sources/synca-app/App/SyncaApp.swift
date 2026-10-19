import SwiftUI
import AppKit
import NSShell

@main
struct SyncaApp: App {
    @State private var model = AppModel()
    @NSApplicationDelegateAdaptor(ShellAppDelegate.self) private var delegate

    var body: some Scene {
        Window("synca", id: "main") {
            RootView()
                .environment(model)
                .task { model.start() }
                .frame(minWidth: 1020, minHeight: 600)
        }
        .defaultSize(width: 1320, height: 820)
        .commands { AppCommands(model: model) }
    }
}

struct AppCommands: Commands {
    let model: AppModel
    var body: some Commands {
        CommandGroup(after: .sidebar) {
            command(.reload)
            command(.toggleActivity)
            command(.toggleSettings)
        }
        CommandMenu("Sync") {
            command(.syncAll)
            command(.installSkill)
            command(.addMcp)
            Divider()
            command(.checkUpdate)
            Divider()
            command(.userScope)
            command(.projectScope)
        }
    }

    @ViewBuilder
    private func command(_ id: SyncaCommand) -> some View {
        if let shortcut = id.shortcut {
            Button(id.title(model)) { id.run(model) }
                .keyboardShortcut(shortcut, modifiers: id.modifiers)
        } else {
            Button(id.title(model)) { id.run(model) }
        }
    }
}
