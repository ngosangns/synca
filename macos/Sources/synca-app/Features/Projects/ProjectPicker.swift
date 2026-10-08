import AppKit

/// Folder chooser shared by every "add project" entry point.
@MainActor enum ProjectPicker {
    static func addProjects(to model: AppModel) {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Add"
        panel.message = "Choose one or more project folders"
        if panel.runModal() == .OK { model.addProjects(panel.urls.map(\.path)) }
    }
}
