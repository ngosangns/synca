import SwiftUI
import AppKit

/// Brand-gradient rounded badge with an SF Symbol.
struct HeroBadge: View {
    let systemImage: String
    var body: some View {
        Image(systemName: systemImage)
            .font(.title2.weight(.semibold))
            .foregroundStyle(.white)
            .frame(width: 48, height: 48)
            .background(Theme.brandGradient, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
            .accessibilityHidden(true)
    }
}

enum Finder {
    static func reveal(_ path: String) {
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }
    static func copy(_ string: String) {
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(string, forType: .string)
    }
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
