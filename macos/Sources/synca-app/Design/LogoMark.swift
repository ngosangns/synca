import SwiftUI

/// synca logo: two opposing sync arrows orbiting a central hub, on a
/// brand-gradient squircle tile. Geometry mirrors `Tools/make-icon.swift`
/// (which renders the same artwork into `AppIcon.icns`) — keep them in sync.
struct LogoMark: View {
    var size: CGFloat = 28

    var body: some View {
        Canvas { ctx, canvasSize in
            let s = min(canvasSize.width, canvasSize.height)
            LogoArt.draw(in: &ctx, side: s)
        }
        .frame(width: size, height: size)
        .accessibilityElement()
        .accessibilityLabel("synca")
    }
}

/// Mark plus the "synca" wordmark, used in the sidebar header.
struct WordmarkLogo: View {
    var markSize: CGFloat = 28

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            LogoMark(size: markSize)
            Text("synca")
                .font(.system(.title3, design: .rounded, weight: .semibold))
                .foregroundStyle(.primary)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("synca")
    }
}

private enum LogoArt {
    static let tileRadius: CGFloat = 0.2237
    static let arcRadius: CGFloat = 0.255
    static let strokeWidth: CGFloat = 0.088
    static let arcSweep: CGFloat = 112
    static let arc1Start: CGFloat = 214
    static let headLength: CGFloat = 0.135
    static let headHalfWidth: CGFloat = 0.112
    static let hubRadius: CGFloat = 0.074
    static let haloRadius: CGFloat = 0.135

    static func draw(in ctx: inout GraphicsContext, side s: CGFloat) {
        let tile = CGRect(x: 0, y: 0, width: s, height: s)
        let tilePath = Path(roundedRect: tile, cornerRadius: s * tileRadius, style: .continuous)

        // Brand gradient + soft top sheen.
        ctx.fill(tilePath, with: .linearGradient(
            Gradient(colors: [Theme.brandA, Theme.brandB]),
            startPoint: .zero, endPoint: CGPoint(x: s, y: s)))
        ctx.fill(tilePath, with: .linearGradient(
            Gradient(colors: [.white.opacity(0.28), .white.opacity(0)]),
            startPoint: .zero, endPoint: CGPoint(x: 0, y: s * 0.55)))

        // Inner highlight hairline (skipped at tiny sizes where it only adds noise).
        if s >= 24 {
            let inset = max(0.5, s * 0.0035)
            let rim = Path(roundedRect: tile.insetBy(dx: inset, dy: inset),
                           cornerRadius: s * tileRadius - inset, style: .continuous)
            ctx.stroke(rim, with: .linearGradient(
                Gradient(colors: [.white.opacity(0.55), .white.opacity(0.05)]),
                startPoint: .zero, endPoint: CGPoint(x: 0, y: s)), lineWidth: max(1, s * 0.006))
        }

        // Glyph with gentle depth shadow.
        ctx.drawLayer { layer in
            if s >= 24 {
                layer.addFilter(.shadow(color: .black.opacity(0.28), radius: s * 0.02, x: 0, y: s * 0.012))
            }
            drawGlyph(in: &layer, side: s)
        }
    }

    private static func drawGlyph(in ctx: inout GraphicsContext, side s: CGFloat) {
        let c = CGPoint(x: s / 2, y: s / 2)
        let r = arcRadius * s
        for k in 0..<2 {
            let start = arc1Start + 180 * CGFloat(k)
            var arc = Path()
            let steps = Int(arcSweep / 2)
            for i in 0...steps {
                let a = radians(start + arcSweep * CGFloat(i) / CGFloat(steps))
                let p = CGPoint(x: c.x + r * cos(a), y: c.y + r * sin(a))
                if i == 0 { arc.move(to: p) } else { arc.addLine(to: p) }
            }
            ctx.stroke(arc, with: .color(.white),
                       style: StrokeStyle(lineWidth: strokeWidth * s, lineCap: .round, lineJoin: .round))

            let a = radians(start + arcSweep)
            let pt = CGPoint(x: c.x + r * cos(a), y: c.y + r * sin(a))
            let t = CGPoint(x: -sin(a), y: cos(a))
            let n = CGPoint(x: cos(a), y: sin(a))
            var head = Path()
            head.move(to: CGPoint(x: pt.x + n.x * headHalfWidth * s, y: pt.y + n.y * headHalfWidth * s))
            head.addLine(to: CGPoint(x: pt.x + t.x * headLength * s, y: pt.y + t.y * headLength * s))
            head.addLine(to: CGPoint(x: pt.x - n.x * headHalfWidth * s, y: pt.y - n.y * headHalfWidth * s))
            head.closeSubpath()
            ctx.fill(head, with: .color(.white))
            ctx.stroke(head, with: .color(.white),
                       style: StrokeStyle(lineWidth: 0.022 * s, lineCap: .round, lineJoin: .round))
        }

        // Hub with soft halo.
        let halo = haloRadius * s, hub = hubRadius * s
        ctx.fill(Path(ellipseIn: CGRect(x: c.x - halo, y: c.y - halo, width: halo * 2, height: halo * 2)),
                 with: .color(.white.opacity(0.28)))
        ctx.fill(Path(ellipseIn: CGRect(x: c.x - hub, y: c.y - hub, width: hub * 2, height: hub * 2)),
                 with: .color(.white))
    }

    private static func radians(_ d: CGFloat) -> CGFloat { d * .pi / 180 }
}
