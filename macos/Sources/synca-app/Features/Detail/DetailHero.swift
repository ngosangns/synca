import SwiftUI
import AppKit

/// Brand-gradient rounded badge with an SF Symbol. Fixed size, never shrinks.
struct HeroBadge: View {
    let systemImage: String
    var body: some View {
        Image(systemName: systemImage)
            .font(.title2.weight(.semibold))
            .foregroundStyle(.white)
            .frame(width: 48, height: 48)
            .background(Theme.brandGradient, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
            .fixedSize()
            .accessibilityHidden(true)
    }
}

/// Width breakpoints (in points of the scrolling content, padding excluded)
/// shared by the skill and MCP detail panels.
enum DetailBreakpoint {
    /// Below this the presence tables drop their lower-priority columns.
    static let compactTable: CGFloat = 520
    /// At or above this the file explorer shows tree and preview side by side.
    static let sideBySide: CGFloat = 640
}

extension View {
    /// Reports the view's size whenever it changes (used for breakpoints).
    func measureSize(into size: Binding<CGSize>) -> some View {
        onGeometryChange(for: CGSize.self) { $0.size } action: { newValue in
            if size.wrappedValue != newValue { size.wrappedValue = newValue }
        }
    }
}

/// Wraps children onto new lines when they no longer fit the proposed width.
struct FlowLayout: Layout {
    var spacing: CGFloat = Theme.Space.xs
    var lineSpacing: CGFloat = Theme.Space.xs

    private struct Arrangement {
        var frames: [CGRect] = []
        var size: CGSize = .zero
    }

    private func arrange(maxWidth: CGFloat, subviews: Subviews) -> Arrangement {
        var result = Arrangement()
        var x: CGFloat = 0, y: CGFloat = 0, lineHeight: CGFloat = 0, usedWidth: CGFloat = 0
        var lineStart = 0
        func finishLine() {
            // Center items vertically inside the finished line.
            for i in lineStart..<result.frames.count {
                result.frames[i].origin.y = y + (lineHeight - result.frames[i].height) / 2
            }
            lineStart = result.frames.count
        }
        for sub in subviews {
            var size = sub.sizeThatFits(.unspecified)
            if size.width > maxWidth {
                size = sub.sizeThatFits(ProposedViewSize(width: maxWidth, height: nil))
            }
            if x > 0, x + size.width > maxWidth {
                finishLine()
                y += lineHeight + lineSpacing
                x = 0; lineHeight = 0
            }
            result.frames.append(CGRect(origin: CGPoint(x: x, y: y), size: size))
            x += size.width + spacing
            lineHeight = max(lineHeight, size.height)
            usedWidth = max(usedWidth, x - spacing)
        }
        if !result.frames.isEmpty {
            finishLine()
            result.size = CGSize(width: usedWidth, height: y + lineHeight)
        }
        return result
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        arrange(maxWidth: proposal.width ?? .infinity, subviews: subviews).size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let arrangement = arrange(maxWidth: bounds.width, subviews: subviews)
        for (sub, frame) in zip(subviews, arrangement.frames) {
            sub.place(at: CGPoint(x: bounds.minX + frame.minX, y: bounds.minY + frame.minY),
                      anchor: .topLeading, proposal: ProposedViewSize(frame.size))
        }
    }
}

/// Hero header: badge, wrapping title, optional subtitle and a wrapping tag row.
struct HeroHeader<Tags: View>: View {
    let systemImage: String
    let title: String
    var subtitle: String?
    /// Pass `false` when `tags` would be empty so no stray spacing is added.
    var showTags = true
    @ViewBuilder var tags: () -> Tags

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.md) {
            HeroBadge(systemImage: systemImage)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text(title).font(.title3.weight(.semibold)).textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
                if let subtitle {
                    Text(subtitle).font(.subheadline).foregroundStyle(.secondary).textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if showTags { FlowLayout { tags() } }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// Row of hero action buttons: one row when it fits, otherwise wrapped onto
/// several lines. Buttons keep their text labels. The busy spinner stays last.
struct ActionBar<Content: View>: View {
    var isBusy: Bool
    @ViewBuilder var content: () -> Content

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: Theme.Space.sm) {
                content()
                spinner
                Spacer(minLength: 0)
            }
            FlowLayout(spacing: Theme.Space.sm, lineSpacing: Theme.Space.sm) {
                content()
                spinner
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var spinner: some View {
        if isBusy { ProgressView().controlSize(.small) }
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
