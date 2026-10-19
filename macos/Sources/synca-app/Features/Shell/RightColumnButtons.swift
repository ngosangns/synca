import SwiftUI
import NSShell

/// Trailing rail that stays visible when a column is collapsed.
/// Panel triggers live here so Activity can be opened again from the window.
struct RightColumnButtons: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(spacing: 2) {
            RailButton(
                title: "Activity",
                systemImage: Icon.activity,
                isOn: model.showActivity,
                help: model.showActivity ? "Hide Activity" : "Show Activity",
                hint: model.showActivity ? "Hides the Activity column" : "Shows the Activity column"
            ) {
                model.showActivity.toggle()
            }
            Spacer(minLength: 0)
            RailButton(
                title: "Settings",
                systemImage: Icon.settings,
                isOn: model.showSettings,
                help: model.showSettings ? "Hide Settings" : "Settings",
                hint: model.showSettings ? "Hides Settings" : "Shows Settings"
            ) {
                model.showSettings.toggle()
            }
        }
        .padding(.top, 6)
        .padding(.bottom, Theme.Space.sm)
        .frame(width: 44)
        .frame(maxHeight: .infinity)
        .background(.bar)
        .overlay(alignment: .leading) { Divider() }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Right column")
    }
}

/// One icon in the right column. `isOn` means the panel it controls is open.
private struct RailButton: View {
    let title: String
    let systemImage: String
    let isOn: Bool
    let help: String
    let hint: String
    let action: () -> Void

    @State private var hovered = false

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.system(size: 15, weight: .medium))
                .frame(width: 28, height: 28)
                .foregroundStyle(isOn ? Color.accentColor : (hovered ? Color.primary : Color.secondary))
                .background {
                    RoundedRectangle(cornerRadius: Theme.Radius.sm, style: .continuous)
                        .fill(fill)
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovered = $0 }
        .help(help)
        .accessibilityLabel(title)
        .accessibilityValue(isOn ? "Shown" : "Hidden")
        .accessibilityHint(hint)
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }

    private var fill: Color {
        if isOn { return Color.accentColor.opacity(0.18) }
        if hovered { return Color.primary.opacity(0.06) }
        return .clear
    }
}
