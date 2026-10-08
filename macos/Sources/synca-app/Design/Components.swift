import SwiftUI

/// Heading bar at the top of every column.
struct PaneHeader<Trailing: View>: View {
    let title: String
    var subtitle: String?
    var systemImage: String?
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            if let systemImage {
                Image(systemName: systemImage).foregroundStyle(.secondary).accessibilityHidden(true)
            }
            Text(title).font(.headline)
            if let subtitle {
                Text(subtitle).font(.subheadline).foregroundStyle(.secondary).monospacedDigit()
            }
            Spacer(minLength: 0)
            trailing()
        }
        .padding(.horizontal, Theme.Space.lg)
        .frame(height: 40)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isHeader)
    }
}

extension PaneHeader where Trailing == EmptyView {
    init(title: String, subtitle: String? = nil, systemImage: String? = nil) {
        self.init(title: title, subtitle: subtitle, systemImage: systemImage) { EmptyView() }
    }
}

/// Small section heading used inside a pane ("Skills  12").
struct SectionHeading<Trailing: View>: View {
    let title: String
    var count: Int?
    @ViewBuilder var trailing: () -> Trailing
    var body: some View {
        HStack {
            Text(title.uppercased()).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            if let count { Text("\(count)").font(.caption).monospacedDigit().foregroundStyle(.tertiary) }
            Spacer(minLength: 0)
            trailing()
        }
        .accessibilityAddTraits(.isHeader)
    }
}
extension SectionHeading where Trailing == EmptyView {
    init(title: String, count: Int? = nil) { self.init(title: title, count: count) { EmptyView() } }
}

/// Rounded grouped container.
struct Card<Content: View>: View {
    var padding: CGFloat = Theme.Space.md
    @ViewBuilder var content: () -> Content
    var body: some View {
        content()
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.background.secondary, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.md).strokeBorder(.separator))
    }
}

/// Capsule tag ("stdio", "mismatch" ...).
struct Tag: View {
    let text: String
    var tint: Color = .secondary
    var systemImage: String?
    var body: some View {
        HStack(spacing: 4) {
            if let systemImage { Image(systemName: systemImage).imageScale(.small) }
            Text(text).lineLimit(1)
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(tint)
        .padding(.horizontal, 8).padding(.vertical, 2)
        .background(tint.opacity(0.14), in: Capsule())
    }
}

/// Centered placeholder for empty / error / idle panes.
struct StateView<Actions: View>: View {
    let systemImage: String
    let title: String
    var message: String?
    var tint: Color = .secondary
    @ViewBuilder var actions: () -> Actions
    var body: some View {
        VStack(spacing: Theme.Space.md) {
            Image(systemName: systemImage).font(.system(size: 34, weight: .light)).foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(title).font(.headline)
            if let message {
                Text(message).font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center)
                    .textSelection(.enabled)
            }
            actions()
        }
        .padding(Theme.Space.xl)
        .frame(maxWidth: 420)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .contain)
    }
}
extension StateView where Actions == EmptyView {
    init(systemImage: String, title: String, message: String? = nil, tint: Color = .secondary) {
        self.init(systemImage: systemImage, title: title, message: message, tint: tint) { EmptyView() }
    }
}

/// Indeterminate "working" state with an optional cancel-free message.
struct WorkingView: View {
    let message: String
    var body: some View {
        VStack(spacing: Theme.Space.md) {
            ProgressView().controlSize(.large)
            Text(message).font(.callout).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(message)
    }
}

/// Shimmering placeholder rows shown during the first inventory load.
struct SkeletonList: View {
    var rows = 6
    @State private var phase = false
    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<rows, id: \.self) { i in
                HStack(spacing: Theme.Space.sm) {
                    RoundedRectangle(cornerRadius: 4).frame(width: 16, height: 16)
                    RoundedRectangle(cornerRadius: 4).frame(width: CGFloat(90 + (i * 37) % 80), height: 10)
                    Spacer()
                }
                .foregroundStyle(.quaternary)
                .padding(.horizontal, Theme.Space.md).padding(.vertical, 10)
            }
        }
        .opacity(phase ? 0.45 : 1)
        .animation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true), value: phase)
        .onAppear { phase = true }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

/// Floating status message; tap to dismiss.
struct ToastView: View {
    let toast: Toast
    let dismiss: () -> Void
    var body: some View {
        let (icon, tint): (String, Color) = switch toast.style {
        case .success: (Icon.success, .green)
        case .error: (Icon.error, .red)
        case .info: (Icon.info, .blue)
        }
        HStack(spacing: Theme.Space.sm) {
            Image(systemName: icon).foregroundStyle(tint)
            Text(toast.message).lineLimit(3).textSelection(.enabled)
            Button(action: dismiss) { Label("Dismiss", systemImage: Icon.close) }
                .buttonStyle(.plain).foregroundStyle(.secondary)
        }
        .font(.callout)
        .padding(.horizontal, Theme.Space.lg).padding(.vertical, Theme.Space.md)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.md).strokeBorder(.separator))
        .shadow(color: .black.opacity(0.15), radius: 12, y: 4)
        .frame(maxWidth: 520)
        .accessibilityElement(children: .combine)
    }
}

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
