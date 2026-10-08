#!/usr/bin/env swift
// Renders the synca app icon (same artwork as Sources/synca-app/Design/LogoMark.swift)
// into an .iconset and converts it to Resources/AppIcon.icns with iconutil.
// Usage (from macos/): swift Tools/make-icon.swift

import AppKit
import CoreGraphics
import Foundation

// MARK: - Artwork (unit square, y-down; keep in sync with LogoMark.swift)

enum Art {
    static let tileRadius: CGFloat = 0.2237
    static let center = CGPoint(x: 0.5, y: 0.5)
    static let arcRadius: CGFloat = 0.255
    static let strokeWidth: CGFloat = 0.088
    static let arcSweep: CGFloat = 112          // degrees
    static let arc1Start: CGFloat = 214         // degrees, clockwise from +x on screen
    static let headLength: CGFloat = 0.135
    static let headHalfWidth: CGFloat = 0.112
    static let hubRadius: CGFloat = 0.074
    static let haloRadius: CGFloat = 0.135
    static let teal = (r: CGFloat(0.13), g: CGFloat(0.77), b: CGFloat(0.69))
    static let indigo = (r: CGFloat(0.36), g: CGFloat(0.36), b: CGFloat(0.93))
}

func rad(_ d: CGFloat) -> CGFloat { d * .pi / 180 }

/// Arc polyline (sampled so direction is unambiguous in a flipped context).
func arcPath(start: CGFloat, sweep: CGFloat, radius: CGFloat) -> CGPath {
    let p = CGMutablePath()
    let steps = max(2, Int(sweep / 2))
    for i in 0...steps {
        let a = rad(start + sweep * CGFloat(i) / CGFloat(steps))
        let pt = CGPoint(x: Art.center.x + radius * cos(a), y: Art.center.y + radius * sin(a))
        if i == 0 { p.move(to: pt) } else { p.addLine(to: pt) }
    }
    return p
}

func headPath(at angle: CGFloat, radius: CGFloat) -> CGPath {
    let a = rad(angle)
    let pt = CGPoint(x: Art.center.x + radius * cos(a), y: Art.center.y + radius * sin(a))
    let t = CGPoint(x: -sin(a), y: cos(a))   // clockwise tangent
    let n = CGPoint(x: cos(a), y: sin(a))
    let tip = CGPoint(x: pt.x + t.x * Art.headLength, y: pt.y + t.y * Art.headLength)
    let b1 = CGPoint(x: pt.x + n.x * Art.headHalfWidth, y: pt.y + n.y * Art.headHalfWidth)
    let b2 = CGPoint(x: pt.x - n.x * Art.headHalfWidth, y: pt.y - n.y * Art.headHalfWidth)
    let p = CGMutablePath()
    p.move(to: b1); p.addLine(to: tip); p.addLine(to: b2); p.closeSubpath()
    return p
}

func glyph(_ ctx: CGContext, scale s: CGFloat) {
    let white = CGColor(gray: 1, alpha: 1)
    ctx.setStrokeColor(white); ctx.setFillColor(white)
    ctx.setLineCap(.round); ctx.setLineJoin(.round)
    for k in 0..<2 {
        let start = Art.arc1Start + 180 * CGFloat(k)
        ctx.setLineWidth(Art.strokeWidth * s)
        ctx.addPath(arcPath(start: start, sweep: Art.arcSweep, radius: Art.arcRadius * s))
        ctx.strokePath()
        let head = headPath(at: start + Art.arcSweep, radius: Art.arcRadius * s)
        ctx.setLineWidth(0.022 * s)
        ctx.addPath(head); ctx.drawPath(using: .fillStroke)
    }
    // Hub with soft halo.
    let c = CGPoint(x: Art.center.x * s, y: Art.center.y * s)
    ctx.setFillColor(CGColor(gray: 1, alpha: 0.28))
    ctx.fillEllipse(in: CGRect(x: c.x - Art.haloRadius * s, y: c.y - Art.haloRadius * s,
                               width: Art.haloRadius * 2 * s, height: Art.haloRadius * 2 * s))
    ctx.setFillColor(white)
    ctx.fillEllipse(in: CGRect(x: c.x - Art.hubRadius * s, y: c.y - Art.hubRadius * s,
                               width: Art.hubRadius * 2 * s, height: Art.hubRadius * 2 * s))
}

/// The arc paths above are in unit coordinates scaled by `s`; build them scaled.
/// (arcPath/headPath use unit center, so the context is scaled instead — see render.)
func render(pixels: Int) -> NSBitmapImageRep {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
                               bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                               colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    let gctx = NSGraphicsContext(bitmapImageRep: rep)!
    let ctx = gctx.cgContext
    let px = CGFloat(pixels)
    let space = CGColorSpaceCreateDeviceRGB()

    // y-down coordinates.
    ctx.translateBy(x: 0, y: px)
    ctx.scaleBy(x: 1, y: -1)

    // macOS icon grid: tile is 824/1024 of the canvas.
    let tileSize = px * 824 / 1024
    let origin = (px - tileSize) / 2
    let tile = CGRect(x: origin, y: origin, width: tileSize, height: tileSize)
    let radius = tileSize * Art.tileRadius
    let tilePath = CGPath(roundedRect: tile, cornerWidth: radius, cornerHeight: radius, transform: nil)

    // Drop shadow.
    ctx.saveGState()
    ctx.setShadow(offset: CGSize(width: 0, height: -px * 0.012), blur: px * 0.025,
                  color: CGColor(gray: 0, alpha: 0.35))
    ctx.addPath(tilePath)
    ctx.setFillColor(CGColor(red: Art.indigo.r, green: Art.indigo.g, blue: Art.indigo.b, alpha: 1))
    ctx.fillPath()
    ctx.restoreGState()

    // Brand gradient fill.
    ctx.saveGState()
    ctx.addPath(tilePath); ctx.clip()
    let grad = CGGradient(colorsSpace: space, colors: [
        CGColor(red: Art.teal.r, green: Art.teal.g, blue: Art.teal.b, alpha: 1),
        CGColor(red: Art.indigo.r, green: Art.indigo.g, blue: Art.indigo.b, alpha: 1),
    ] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(grad, start: CGPoint(x: tile.minX, y: tile.minY),
                           end: CGPoint(x: tile.maxX, y: tile.maxY), options: [])
    // Soft top sheen.
    let sheen = CGGradient(colorsSpace: space, colors: [
        CGColor(gray: 1, alpha: 0.28), CGColor(gray: 1, alpha: 0),
    ] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(sheen, start: CGPoint(x: 0, y: tile.minY),
                           end: CGPoint(x: 0, y: tile.minY + tileSize * 0.55), options: [])
    ctx.restoreGState()

    // Inner highlight hairline.
    ctx.saveGState()
    let inset = max(0.5, tileSize * 0.0035)
    let inner = CGPath(roundedRect: tile.insetBy(dx: inset, dy: inset),
                       cornerWidth: radius - inset, cornerHeight: radius - inset, transform: nil)
    ctx.addPath(inner)
    ctx.setLineWidth(max(1, tileSize * 0.006))
    ctx.replacePathWithStrokedPath()
    ctx.clip()
    let rim = CGGradient(colorsSpace: space, colors: [
        CGColor(gray: 1, alpha: 0.55), CGColor(gray: 1, alpha: 0.05),
    ] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(rim, start: CGPoint(x: 0, y: tile.minY), end: CGPoint(x: 0, y: tile.maxY), options: [])
    ctx.restoreGState()

    // Glyph, with a gentle shadow for depth.
    ctx.saveGState()
    ctx.translateBy(x: tile.minX, y: tile.minY)
    ctx.scaleBy(x: tileSize, y: tileSize)
    ctx.setShadow(offset: CGSize(width: 0, height: -0.012), blur: 0.02, color: CGColor(gray: 0, alpha: 0.28))
    ctx.beginTransparencyLayer(auxiliaryInfo: nil)
    glyph(ctx, scale: 1)
    ctx.endTransparencyLayer()
    ctx.restoreGState()

    return rep
}

// MARK: - Output

let fm = FileManager.default
let cwd = URL(fileURLWithPath: fm.currentDirectoryPath)
let resources = cwd.appendingPathComponent("Resources")
guard fm.fileExists(atPath: resources.path) else {
    FileHandle.standardError.write(Data("Run from the macos/ directory (Resources/ not found)\n".utf8))
    exit(1)
}
let tmp = fm.temporaryDirectory.appendingPathComponent("synca-icon-\(UUID().uuidString)")
let iconset = tmp.appendingPathComponent("AppIcon.iconset")
try fm.createDirectory(at: iconset, withIntermediateDirectories: true)
defer { try? fm.removeItem(at: tmp) }

let variants: [(name: String, px: Int)] = [
    ("icon_16x16", 16), ("icon_16x16@2x", 32),
    ("icon_32x32", 32), ("icon_32x32@2x", 64),
    ("icon_128x128", 128), ("icon_128x128@2x", 256),
    ("icon_256x256", 256), ("icon_256x256@2x", 512),
    ("icon_512x512", 512), ("icon_512x512@2x", 1024),
]
for v in variants {
    let rep = render(pixels: v.px)
    guard let png = rep.representation(using: .png, properties: [:]) else { fatalError("png encode failed") }
    try png.write(to: iconset.appendingPathComponent("\(v.name).png"))
}

let out = resources.appendingPathComponent("AppIcon.icns")
let p = Process()
p.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
p.arguments = ["-c", "icns", iconset.path, "-o", out.path]
try p.run()
p.waitUntilExit()
guard p.terminationStatus == 0 else { fatalError("iconutil failed (\(p.terminationStatus))") }
print("Wrote \(out.path)")
