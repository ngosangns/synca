import SwiftUI
import AppKit

@main
struct SyncaApp: App {
    @State private var model = AppModel()
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

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

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

struct AppCommands: Commands {
    let model: AppModel
    var body: some Commands {
        CommandGroup(after: .sidebar) {
            Button("Reload Inventory") { model.reload() }.keyboardShortcut("r")
            Button(model.showActivity ? "Hide Activity" : "Show Activity") {
                model.showActivity.toggle()
            }.keyboardShortcut("l", modifiers: [.command, .option])
        }
        CommandMenu("Sync") {
            Button("Sync Everything…") { model.planSync(target: .all) }.keyboardShortcut("s", modifiers: [.command, .shift])
            Button("Install Skill…") { model.route = .installSkill }.keyboardShortcut("i")
            Button("Add MCP Server…") { model.route = .addMcp }.keyboardShortcut("n")
            Divider()
            Button("Check for Updates…") { model.checkUpdate() }
            Divider()
            Button("User Scope") { model.scope = .user }.keyboardShortcut("1")
            Button("Project Scope") { model.scope = .project }.keyboardShortcut("2")
        }
    }
}
