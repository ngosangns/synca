import SwiftUI
import SyncaKit

struct PlanView: View {
    let plan: Plan
    @Environment(AppModel.self) private var model
    @State private var conflicts: [PlanConflict]
    @State private var showOutput = false

    init(plan: Plan) {
        self.plan = plan
        _conflicts = State(initialValue: plan.conflicts)
    }

    private var actionCount: Int { plan.chips.reduce(0) { $0 + $1.count } }
    private var isEmptyPlan: Bool { plan.ok && actionCount == 0 && conflicts.isEmpty }

    private var applyTitle: String {
        switch plan.apply {
        case .sync: "Apply sync"
        case .installSkill: "Install"
        case .addMcp: "Add server"
        }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.lg) {
                Text(plan.title).font(.title2.weight(.semibold)).textSelection(.enabled)
                    .accessibilityAddTraits(.isHeader)

                if !plan.ok {
                    banner(Icon.error, .red, "Command failed",
                           "The dry run didn't complete. See Activity for the full command output.")
                } else if isEmptyPlan {
                    banner(Icon.success, .green, "Everything is already in sync",
                           "Nothing to change.")
                }

                if !plan.chips.isEmpty {
                    HStack(spacing: Theme.Space.sm) {
                        ForEach(plan.chips, id: \.self) { chip in
                            Tag(text: "\(chip.count) \(chip.kind)", tint: .accentColor)
                        }
                    }
                    .accessibilityElement(children: .contain)
                }

                if !conflicts.isEmpty { conflictsCard }

                if !plan.output.isEmpty {
                    DisclosureGroup(isExpanded: $showOutput) {
                        ConsoleText(text: plan.output).padding(.top, Theme.Space.xs)
                    } label: {
                        Text("Raw output").font(.subheadline.weight(.medium))
                    }
                }

                actions
            }
            .padding(Theme.Space.lg)
            .frame(maxWidth: 760, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func banner(_ icon: String, _ tint: Color, _ title: String, _ message: String) -> some View {
        HStack(alignment: .top, spacing: Theme.Space.sm) {
            Image(systemName: icon).foregroundStyle(tint).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(message).font(.callout).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(Theme.Space.md)
        .background(tint.opacity(0.12), in: RoundedRectangle(cornerRadius: Theme.Radius.md))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.md).strokeBorder(tint.opacity(0.4)))
        .accessibilityElement(children: .combine)
    }

    private var conflictsCard: some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Space.sm) {
                Label("Conflicts", systemImage: Icon.warning)
                    .font(.headline).foregroundStyle(.orange)
                Text("Unresolved conflicts are skipped — nothing is overwritten unless you choose a resolution.")
                    .font(.callout).foregroundStyle(.secondary)
                Grid(alignment: .leading, horizontalSpacing: Theme.Space.lg, verticalSpacing: Theme.Space.sm) {
                    GridRow {
                        Text("Item").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                        Text("Resolution").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    }
                    Divider().gridCellUnsizedAxes(.horizontal)
                    ForEach($conflicts) { $c in
                        GridRow {
                            HStack(spacing: Theme.Space.sm) {
                                Tag(text: c.kind == .skill ? "skill" : "mcp")
                                Text(c.key).lineLimit(1).truncationMode(.middle)
                            }
                            Picker("Resolution for \(c.key)", selection: $c.policy) {
                                ForEach(ConflictPolicy.allCases) { Text($0.title).tag($0) }
                            }
                            .labelsHidden()
                            .fixedSize()
                            .disabled(model.isBusy)
                            .accessibilityLabel("Resolution for \(c.key)")
                        }
                    }
                }
            }
        }
    }

    private var actions: some View {
        HStack {
            Spacer()
            Button(isEmptyPlan || !plan.ok ? "Close" : "Dismiss") { model.route = .item }
                .keyboardShortcut(.cancelAction)
                .disabled(model.isBusy)
            if plan.ok && !isEmptyPlan {
                Button(applyTitle) {
                    var p = plan
                    p.conflicts = conflicts
                    model.apply(p)
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(model.isBusy)
            }
        }
    }
}
