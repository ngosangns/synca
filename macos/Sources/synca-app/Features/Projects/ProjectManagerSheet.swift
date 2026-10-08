import AppKit
import SwiftUI
import SyncaKit

private extension Icon {
    static let drop = "square.and.arrow.down.on.square"
    static let use = "checkmark.circle"
    static let more = "ellipsis.circle"
}

/// Add, switch and remove multiple project folders.
struct ProjectManagerSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var missing: Set<String> = []
    @State private var selection: Set<String> = []
    @State private var dropTargeted = false

    private struct Row: Identifiable {
        let path: String
        var id: String { path }
        var name: String { ProjectList.name(of: path) }
    }

    private var rows: [Row] { model.projects.paths.map(Row.init) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            toolbarRow
            content
            Divider()
            footer
        }
        .frame(minWidth: 520, idealWidth: 720, minHeight: 380, idealHeight: 460)
        .dropDestination(for: URL.self) { urls, _ in
            let dirs = urls.filter(\.isFileURL).filter(Self.isDirectory).map(\.path)
            guard !dirs.isEmpty else { return false }
            model.addProjects(dirs)
            return true
        } isTargeted: { dropTargeted = $0 }
        .overlay { if dropTargeted { dropHint } }
        .onExitCommand { dismiss() }
        .task(id: model.projects.paths) {
            let paths = model.projects.paths
            let gone = await Task.detached(priority: .utility) {
                Set(paths.filter { !Self.isDirectory(path: $0) })
            }.value
            missing = gone
        }
    }

    // MARK: Sections

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.md) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Projects").font(.title2.weight(.semibold)).accessibilityAddTraits(.isHeader)
                Text(countText).font(.callout).foregroundStyle(.secondary)
            }
            Spacer(minLength: Theme.Space.md)
            Button("Done") { dismiss() }
                .keyboardShortcut(.defaultAction)
                .help("Close the project manager")
        }
        .padding(Theme.Space.lg)
    }

    private var countText: String {
        let n = model.projects.paths.count
        return n == 0 ? "No projects" : n == 1 ? "1 project" : "\(n) projects"
    }

    private var toolbarRow: some View {
        HStack(spacing: Theme.Space.md) {
            addButton.buttonStyle(.borderedProminent)
            Spacer(minLength: 0)
            ViewThatFits(in: .horizontal) {
                Label("or drag folders from Finder onto this window", systemImage: Icon.drop)
                Label("or drop folders here", systemImage: Icon.drop)
                Text("Drop folders here")
            }
            .font(.callout).foregroundStyle(.secondary)
            .lineLimit(1)
        }
        .padding(.horizontal, Theme.Space.lg).padding(.vertical, Theme.Space.md)
    }

    private var addButton: some View {
        Button { ProjectPicker.addProjects(to: model) } label: {
            Label("Add projects…", systemImage: Icon.add)
        }
        .help("Choose one or more project folders to add")
    }

    @ViewBuilder private var content: some View {
        if model.projects.paths.isEmpty {
            StateView(systemImage: Icon.project, title: "No projects yet",
                      message: "Add project folders to manage their project-scoped skills and MCP servers. You can also drag folders from Finder onto this window.") {
                addButton.buttonStyle(.borderedProminent).controlSize(.large)
            }
        } else {
            table
        }
    }

    private var table: some View {
        Table(rows, selection: $selection) {
            TableColumn("Name") { row in
                Label {
                    Text(row.name).fontWeight(.semibold).lineLimit(1).truncationMode(.middle)
                } icon: {
                    Image(systemName: missing.contains(row.path) ? Icon.warning : Icon.folderFilled)
                        .foregroundStyle(missing.contains(row.path) ? Color.orange : Color.accentColor)
                }
                .help(row.name)
            }
            .width(min: 100, ideal: 160)

            TableColumn("Path") { row in
                Text(row.path)
                    .font(.system(.callout, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
                    .help(row.path)
            }
            .width(min: 120, ideal: 240)

            TableColumn("Status") { row in
                status(for: row)
            }
            .width(min: 60, ideal: 90, max: 130)

            TableColumn("Actions") { row in
                actions(for: row)
            }
            .width(min: 90, ideal: 250, max: 270)
        }
        .contextMenu(forSelectionType: String.self) { paths in
            if let path = paths.first, paths.count == 1 {
                Button("Use") { use(path) }.disabled(isActive(path))
                Button("Reveal in Finder") { reveal(path) }
                Button("Remove", role: .destructive) { model.removeProject(path) }
            }
        } primaryAction: { paths in
            if let path = paths.first { use(path) }
        }
        .accessibilityLabel("Projects")
    }

    private var footer: some View {
        Text("Removing a project only forgets it here; files on disk are untouched.")
            .font(.caption).foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Theme.Space.lg).padding(.vertical, Theme.Space.sm)
    }

    private var dropHint: some View {
        RoundedRectangle(cornerRadius: Theme.Radius.md)
            .strokeBorder(Color.accentColor, style: StrokeStyle(lineWidth: 2, dash: [8, 5]))
            .background(Color.accentColor.opacity(0.10), in: RoundedRectangle(cornerRadius: Theme.Radius.md))
            .overlay {
                Label("Drop folders to add them as projects", systemImage: Icon.drop)
                    .font(.headline).foregroundStyle(Color.accentColor)
            }
            .padding(Theme.Space.sm)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    // MARK: Row pieces

    @ViewBuilder private func status(for row: Row) -> some View {
        HStack(spacing: Theme.Space.xs) {
            if isActive(row.path) { Tag(text: "Active", tint: .green) }
            if missing.contains(row.path) { Tag(text: "Missing", tint: .orange) }
        }
        .accessibilityElement(children: .combine)
    }

    private func actions(for row: Row) -> some View {
        let path = row.path
        return ViewThatFits(in: .horizontal) {
            HStack(spacing: Theme.Space.xs) {
                useButton(path); revealButton(path); removeButton(path)
            }
            Menu {
                Button("Use") { use(path) }.disabled(isActive(path))
                Button("Reveal") { reveal(path) }
                Button("Remove", role: .destructive) { model.removeProject(path) }
            } label: {
                Label("Actions", systemImage: Icon.more)
            }
            .menuStyle(.borderlessButton)
            .help("Actions for \(row.name)")
        }
        .controlSize(.small)
    }

    private func useButton(_ path: String) -> some View {
        Button { use(path) } label: { Label("Use", systemImage: Icon.use) }
            .disabled(isActive(path))
            .help(isActive(path) ? "This project is already active" : "Make this the active project")
            .accessibilityLabel("Use \(ProjectList.name(of: path))")
    }

    private func revealButton(_ path: String) -> some View {
        Button { reveal(path) } label: { Label("Reveal", systemImage: Icon.reveal) }
            .help("Reveal this folder in Finder")
            .accessibilityLabel("Reveal \(ProjectList.name(of: path)) in Finder")
    }

    private func removeButton(_ path: String) -> some View {
        Button(role: .destructive) { model.removeProject(path) } label: {
            Label("Remove", systemImage: Icon.remove)
        }
        .help("Forget this project. Files on disk are untouched.")
        .accessibilityLabel("Remove \(ProjectList.name(of: path)) from projects")
    }

    // MARK: Actions

    private func isActive(_ path: String) -> Bool { model.projects.active == path }

    private func use(_ path: String) {
        model.selectProject(path)
        model.scope = .project
    }

    private func reveal(_ path: String) {
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }

    nonisolated private static func isDirectory(path: String) -> Bool {
        var isDir: ObjCBool = false
        return FileManager.default.fileExists(atPath: path, isDirectory: &isDir) && isDir.boolValue
    }

    nonisolated private static func isDirectory(_ url: URL) -> Bool { isDirectory(path: url.path) }
}
