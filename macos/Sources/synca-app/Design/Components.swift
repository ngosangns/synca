import SwiftUI
import NSShell

/// Monospaced, selectable console output.
struct ConsoleText: View {
    let text: String
    var maxHeight: CGFloat = 240
    var body: some View {
        ScrollView {
            Text(text).font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(Theme.Space.sm)
        }
        .frame(maxHeight: maxHeight)
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.sm).strokeBorder(.separator))
    }
}
