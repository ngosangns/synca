import SwiftUI
import SyncaKit

struct AddMcpForm: View {
    @Environment(AppModel.self) private var model
    @State private var name = ""
    @State private var transport = "stdio"
    @State private var command = ""
    @State private var url = ""
    @State private var enabled = true
    @State private var submitted = false
    @FocusState private var focused: Bool

    private static let transports = ["stdio", "local", "http", "sse"]

    private var usesCommand: Bool { transport == "stdio" || transport == "local" }
    private var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var trimmedCommand: String { command.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var trimmedURL: String { url.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var nameError: String? {
        if trimmedName.isEmpty { return "Name is required." }
        if trimmedName.contains(where: \.isWhitespace) { return "Name can't contain spaces." }
        return nil
    }
    private var targetError: String? {
        if usesCommand { return trimmedCommand.isEmpty ? "Command is required for \(transport)." : nil }
        return trimmedURL.isEmpty ? "URL is required for \(transport)." : nil
    }
    private var isValid: Bool { nameError == nil && targetError == nil }
    private var canSubmit: Bool { isValid && !model.isBusy }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                Text("Add MCP server").font(.title2.weight(.semibold)).accessibilityAddTraits(.isHeader)
                Text("Register an MCP server for every agent in this scope. You'll see a preview first.")
                    .font(.callout).foregroundStyle(.secondary)

                Card {
                    VStack(alignment: .leading, spacing: Theme.Space.md) {
                        field("Name", error: submitted || !name.isEmpty ? nameError : nil) {
                            TextField("Name", text: $name, prompt: Text("my-server"))
                                .labelsHidden().focused($focused).onSubmit(submit)
                                .accessibilityLabel("Server name")
                        }
                        field("Transport", error: nil) {
                            Picker("Transport", selection: $transport) {
                                ForEach(Self.transports, id: \.self) { Text($0).tag($0) }
                            }
                            .labelsHidden().pickerStyle(.segmented)
                            .accessibilityLabel("Transport")
                        }
                        if usesCommand {
                            field("Command", error: submitted ? targetError : nil) {
                                TextField("Command", text: $command, prompt: Text("npx -y @scope/server"))
                                    .labelsHidden().onSubmit(submit)
                                    .accessibilityLabel("Command")
                            }
                        } else {
                            field("URL", error: submitted ? targetError : nil) {
                                TextField("URL", text: $url, prompt: Text("https://example.com/mcp"))
                                    .labelsHidden().onSubmit(submit)
                                    .accessibilityLabel("Server URL")
                            }
                        }
                        Toggle("Enabled", isOn: $enabled)
                            .accessibilityLabel("Enabled")
                    }
                }

                HStack {
                    Spacer()
                    Button("Cancel") { model.route = .item }
                        .keyboardShortcut(.cancelAction)
                    Button {
                        submit()
                    } label: {
                        Label("Preview add", systemImage: Icon.add)
                    }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(model.isBusy || (submitted && !isValid))
                }
            }
            .padding(Theme.Space.lg)
            .frame(maxWidth: 640, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task { focused = true }
    }

    @ViewBuilder
    private func field<C: View>(_ title: String, error: String?, @ViewBuilder content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(title).font(.subheadline.weight(.medium))
            content().textFieldStyle(.roundedBorder)
            if let error {
                Label(error, systemImage: Icon.warning).font(.caption).foregroundStyle(.red)
            }
        }
    }

    private func submit() {
        submitted = true
        guard canSubmit else { return }
        model.previewAddMcp(AddMcpPayload(
            name: trimmedName, transport: transport,
            command: usesCommand ? trimmedCommand : nil,
            url: usesCommand ? nil : trimmedURL,
            enabled: enabled))
    }
}
