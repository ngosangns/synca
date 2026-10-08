import SwiftUI
import AppKit

struct InstallSkillForm: View {
    @Environment(AppModel.self) private var model
    @State private var source = ""
    @State private var touched = false
    @FocusState private var focused: Bool

    private var trimmed: String { source.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var isValid: Bool { !trimmed.isEmpty }
    private var canSubmit: Bool { isValid && !model.isBusy }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                Text("Install skill").font(.title2.weight(.semibold)).accessibilityAddTraits(.isHeader)
                Text("Install a skill from a local folder or a git repository. You'll see a preview before anything changes.")
                    .font(.callout).foregroundStyle(.secondary)

                Card {
                    VStack(alignment: .leading, spacing: Theme.Space.sm) {
                        Text("Source").font(.subheadline.weight(.medium))
                        HStack {
                            TextField("Source", text: $source, prompt: Text("Path or git URL"))
                                .textFieldStyle(.roundedBorder)
                                .labelsHidden()
                                .autocorrectionDisabled()
                                .focused($focused)
                                .onSubmit(submit)
                                .onChange(of: source) { _, _ in touched = true }
                                .accessibilityLabel("Skill source, path or git URL")
                            Button("Browse…", action: browse)
                                .disabled(model.isBusy)
                                .help("Choose a skill folder")
                                .accessibilityLabel("Browse for a skill folder")
                        }
                        if touched && !isValid {
                            Label("Enter a folder path or git URL.", systemImage: Icon.warning)
                                .font(.caption).foregroundStyle(.red)
                        } else {
                            Text("Example: ~/skills/my-skill or https://github.com/org/repo")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }

                HStack {
                    Spacer()
                    Button("Cancel") { model.route = .item }
                        .keyboardShortcut(.cancelAction)
                    Button {
                        submit()
                    } label: {
                        Label("Preview install", systemImage: Icon.install)
                    }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(!canSubmit)
                }
            }
            .padding(Theme.Space.lg)
            .frame(maxWidth: 640, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task { focused = true }
    }

    private func submit() {
        touched = true
        guard canSubmit else { return }
        model.previewInstallSkill(source: trimmed)
    }

    private func browse() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Choose"
        if panel.runModal() == .OK, let url = panel.url {
            source = url.path
        }
    }
}
