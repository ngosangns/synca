import SwiftUI
import SyncaKit

struct ActivityView: View {
    @Environment(AppModel.self) private var model
    @State private var expanded: Set<Int> = []

    var body: some View {
        VStack(spacing: 0) {
            PaneHeader(title: "Activity", subtitle: model.activity.isEmpty ? nil : "\(model.activity.count)",
                       systemImage: Icon.activity) {
                Button("Clear") { model.clearActivity(); expanded = [] }
                    .disabled(model.activity.isEmpty)
                    .help("Clear activity log")
                Button { model.showActivity = false } label: {
                    Label("Hide", systemImage: Icon.close)
                }
                .help("Hide Activity")
            }
            content
        }
    }

    @ViewBuilder
    private var content: some View {
        if model.activity.isEmpty {
            if model.activityLoading {
                WorkingView(message: "Loading activity…")
            } else {
                StateView(systemImage: Icon.activity, title: "No activity yet",
                          message: "Commands run by Synca will appear here.")
            }
        } else {
            List {
                ForEach(model.activity) { entry in
                    ActivityRow(entry: entry, isExpanded: expanded.contains(entry.id)) {
                        if !expanded.insert(entry.id).inserted { expanded.remove(entry.id) }
                    }
                    .onAppear {
                        if entry.id == model.activity.last?.id { model.loadMoreActivity() }
                    }
                }
                if model.activityLoading {
                    HStack {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Spacer()
                    }
                    .accessibilityLabel("Loading more activity")
                }
            }
            .listStyle(.inset)
        }
    }
}
