import SwiftUI
import NSShell

/// Width breakpoints (in points of the scrolling content, padding excluded)
/// shared by the skill and MCP detail panels.
enum DetailBreakpoint {
    /// Below this the presence tables drop their lower-priority columns.
    static let compactTable: CGFloat = 520
    /// At or above this the file explorer shows tree and preview side by side.
    static let sideBySide: CGFloat = 640
}

/// Reveal-in-Finder button used in presence tables and the file preview.
struct RevealButton: View {
    let path: String
    var body: some View {
        Button { Finder.reveal(path) } label: { Label("Reveal", systemImage: Icon.reveal) }
            .buttonStyle(.borderless)
            .help("Reveal \(path) in Finder")
    }
}

func shortHash(_ s: String?) -> String {
    guard let s, !s.isEmpty else { return "—" }
    return String(s.prefix(8))
}

/// Table height that fits `rows` rows plus the header without internal scrolling.
func tableHeight(rows: Int) -> CGFloat { CGFloat(max(rows, 1)) * 28 + 32 }
