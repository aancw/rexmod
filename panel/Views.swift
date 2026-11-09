import AppKit
import SwiftUI

private enum Palette {
    // The bright colors are for dark themes; on a light theme they wash out.
    static let added = adaptive(dark: (0.25, 0.85, 0.55), light: (0.10, 0.50, 0.25))
    static let modified = adaptive(dark: (1.0, 0.68, 0.10), light: (0.65, 0.42, 0.0))
    static let deleted = adaptive(dark: (1.0, 0.30, 0.40), light: (0.78, 0.15, 0.22))
    static let renamed = adaptive(dark: (0.20, 0.85, 1.0), light: (0.0, 0.45, 0.65))
    static let accent = Color(red: 0.73, green: 0.0, blue: 1.0)

    private typealias RGB = (CGFloat, CGFloat, CGFloat)

    private static func adaptive(dark: RGB, light: RGB) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let c = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? dark : light
            return NSColor(srgbRed: c.0, green: c.1, blue: c.2, alpha: 1)
        })
    }

    static func status(_ s: String) -> Color {
        switch s {
        case "A": return added
        case "M", "T": return modified
        case "D": return deleted
        case "R": return renamed
        default: return .secondary
        }
    }
}

private func fileIcon(_ name: String) -> (String, Color) {
    let ext = (name as NSString).pathExtension.lowercased()
    switch ext {
    case "go": return ("chevron.left.forwardslash.chevron.right", Color(red: 0.3, green: 0.8, blue: 0.95))
    case "swift": return ("swift", Color(red: 1.0, green: 0.45, blue: 0.2))
    case "ts", "tsx": return ("curlybraces", Color(red: 0.3, green: 0.6, blue: 1.0))
    case "js", "jsx", "mjs", "cjs": return ("curlybraces", Color(red: 0.95, green: 0.85, blue: 0.3))
    case "json", "jsonc": return ("curlybraces.square", Color(red: 0.95, green: 0.75, blue: 0.3))
    case "md", "mdx", "txt": return ("doc.text", .secondary)
    case "py": return ("chevron.left.forwardslash.chevron.right", Color(red: 0.4, green: 0.65, blue: 1.0))
    case "rs": return ("gearshape", Color(red: 0.9, green: 0.5, blue: 0.3))
    case "lua": return ("moon", Color(red: 0.4, green: 0.5, blue: 1.0))
    case "sh", "fish", "zsh", "bash": return ("terminal", Color(red: 0.5, green: 0.85, blue: 0.5))
    case "yml", "yaml", "toml", "ini", "conf": return ("slider.horizontal.3", .secondary)
    case "png", "jpg", "jpeg", "gif", "svg", "webp", "icns", "ico": return ("photo", Color(red: 0.8, green: 0.5, blue: 0.9))
    case "css", "scss": return ("paintbrush", Color(red: 0.4, green: 0.7, blue: 1.0))
    case "html", "htm": return ("globe", Color(red: 1.0, green: 0.55, blue: 0.3))
    case "kt", "kts", "gradle", "java": return ("cup.and.saucer", Color(red: 0.7, green: 0.5, blue: 1.0))
    case "lock", "sum": return ("lock", .secondary)
    default: return ("doc", .secondary)
    }
}

// MARK: - Motion

/// Timings, checked frame by frame in 60 fps recordings: the tab
/// row reflows in about 150 ms, decelerating, while the new tab's surface
/// fades in and the old one's fades out where they stand; the icons swap
/// colours at about 170 ms and the selected tab's name fades in at about
/// 200 ms, over about 130 ms. The content under the tabs switches at once.
/// Nothing moves with Reduce Motion on.
enum Motion {
    /// REXMOD_REDUCE_MOTION=1 turns motion off too, for testing that path.
    static let forcedOff = ProcessInfo.processInfo.environment["REXMOD_REDUCE_MOTION"] != nil
    static var reduced: Bool { forcedOff || NSWorkspace.shared.accessibilityDisplayShouldReduceMotion }
    static var reflow: Animation? { reduced ? nil : .easeOut(duration: 0.15) }
    static var nameIn: Animation? { reduced ? nil : .easeOut(duration: 0.13).delay(0.2) }
    /// Quick, so a badge moving in as the tab narrows doesn't cross the name.
    static var nameOut: Animation? { reduced ? nil : .easeOut(duration: 0.08) }
    static var fade: Animation? { reduced ? nil : .easeOut(duration: 0.15) }
    /// When a tab's icon swaps colour: once its row has settled.
    static var colorSwapDelay: Double? { reduced ? nil : 0.17 }
}

/// The dot that says an agent is working: it breathes like the empty state's
/// symbol, and holds still with Reduce Motion on.
struct WorkingDot: View {
    @State private var dim = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Circle().fill(Palette.accent).frame(width: 6, height: 6)
            .opacity(dim ? 0.35 : 1)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 0.9).repeatForever()) { dim = true }
            }
    }
}

// MARK: - Building blocks

/// Rex's icon button looks, measured on screen and matched to its theme tokens.
enum RexIconStyle {
    /// Sidebar header icons (ChromeIconButton): content at 0.7, content on a
    /// circle of content at 0.075 when hovered.
    case sidebar
    /// Pane title actions (split, float, zoom, close): content at 0.35, content
    /// at 0.5 on a controlHover circle when hovered.
    case paneAction

    func colors(_ theme: RexThemeColors) -> (rest: NSColor, hover: NSColor, circle: NSColor) {
        switch self {
        case .sidebar:
            return (theme.content.withAlphaComponent(0.7), theme.content, theme.content.withAlphaComponent(0.075))
        case .paneAction:
            return (theme.content.withAlphaComponent(0.35), theme.content.withAlphaComponent(0.5), theme.controlHover)
        }
    }
}

/// Rex's shortcut tip: a raised rounded box under the button with the title
/// and a key cap (e.g. "Split Right  ⌘D"), in the theme's colors, measured from Rex.
final class RexTip {
    static let shared = RexTip()
    private var panel: NSPanel?
    private weak var parentWindow: NSWindow?
    private weak var owner: NSView?
    private var watch: Timer?

    /// Shows the tip under `anchor` while `stillHovered` holds. AppKit sends
    /// no mouseExited when a button is hidden or moved away from a resting
    /// pointer (the panel toggle does both), so the tip checks for itself and
    /// calls `lost` once the pointer is gone.
    func show(title: String, shortcut: String?, below anchor: NSView,
              stillHovered: @escaping () -> Bool, lost: @escaping () -> Void) {
        hide()
        guard let window = anchor.window else { return }
        let theme = RexTheme.colors(for: anchor)
        let border = theme.content.withAlphaComponent(0.16).cgColor
        let text = NSTextField(labelWithString: title)
        text.font = .systemFont(ofSize: 12.5)
        text.textColor = theme.contentSecondary
        text.sizeToFit()
        var width = 12 + text.frame.width + 12
        let height: CGFloat = 28
        var cap: NSView?
        if let shortcut = shortcut {
            let k = NSTextField(labelWithString: shortcut)
            k.font = .systemFont(ofSize: 11.5)
            k.textColor = theme.contentSecondary
            k.sizeToFit()
            let box = NSView(frame: NSRect(x: 0, y: 0, width: k.frame.width + 10, height: 18))
            box.wantsLayer = true
            box.layer?.cornerRadius = 5
            box.layer?.cornerCurve = .continuous
            box.layer?.borderWidth = 1
            box.layer?.borderColor = border
            k.frame.origin = NSPoint(x: 5, y: (18 - k.frame.height) / 2)
            box.addSubview(k)
            cap = box
            width = 12 + text.frame.width + 9 + box.frame.width + 7
        }
        let content = NSView(frame: NSRect(x: 0, y: 0, width: width, height: height))
        content.wantsLayer = true
        content.layer?.backgroundColor = theme.surfaceRaised.cgColor
        content.layer?.cornerRadius = 10
        content.layer?.cornerCurve = .continuous
        content.layer?.borderWidth = 0.5
        content.layer?.borderColor = border
        text.frame.origin = NSPoint(x: 12, y: (height - text.frame.height) / 2)
        content.addSubview(text)
        if let cap = cap {
            cap.frame.origin = NSPoint(x: width - 7 - cap.frame.width, y: (height - cap.frame.height) / 2)
            content.addSubview(cap)
        }
        let a = anchor.convert(anchor.bounds, to: nil)
        let screenRect = window.convertToScreen(a)
        // Centred under the button, but kept inside the window (the toggle is
        // the rightmost control, so a centred tip would hang past the edge).
        var limits = window.frame.insetBy(dx: 8, dy: 0)
        if let visible = window.screen?.visibleFrame { limits = limits.intersection(visible.insetBy(dx: 4, dy: 0)) }
        var x = screenRect.midX - width / 2
        if !limits.isEmpty { x = max(limits.minX, min(x, limits.maxX - width)) }
        let frame = NSRect(x: x, y: screenRect.minY - 5 - height, width: width, height: height)
        let p = NSPanel(contentRect: frame, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: true)
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = true
        p.ignoresMouseEvents = true
        p.contentView = content
        window.addChildWindow(p, ordered: .above)
        p.orderFront(nil)
        panel = p
        parentWindow = window
        owner = anchor
        watch = Timer.scheduledTimer(withTimeInterval: 0.15, repeats: true) { [weak self, weak anchor] _ in
            guard let self = self else { return }
            if anchor == nil || !stillHovered() {
                self.hide()
                lost()
            }
        }
    }

    func hide() {
        watch?.invalidate()
        watch = nil
        if let p = panel {
            parentWindow?.removeChildWindow(p)
            p.orderOut(nil)
        }
        panel = nil
        owner = nil
    }

    /// Hides the tip if `view` showed it.
    func hide(for view: NSView) {
        if owner === view { hide() }
    }
}

/// An icon button with Rex's look (one of RexIconStyle) and Rex's tip.
final class RexStyleIconButton: NSButton {
    var style = RexIconStyle.sidebar { didSet { updateLook() } }
    var tipTitle: String?
    var tipShortcut: String?

    private let circle = CALayer()
    private var tracking: NSTrackingArea?
    private var tipTimer: Timer?
    private var hovered = false { didSet { updateLook() } }
    private var themeObserver: NSObjectProtocol?

    override init(frame: NSRect) {
        super.init(frame: frame)
        isBordered = false
        bezelStyle = .regularSquare
        imagePosition = .imageOnly
        wantsLayer = true
        circle.cornerCurve = .continuous
        layer?.insertSublayer(circle, at: 0)
        updateLook()
    }

    required init?(coder: NSCoder) { fatalError() }

    override var intrinsicContentSize: NSSize { NSSize(width: frame.width, height: frame.width) }
    override var alignmentRectInsets: NSEdgeInsets { NSEdgeInsetsZero }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        circle.frame = bounds
        circle.cornerRadius = min(bounds.width, bounds.height) / 2
        CATransaction.commit()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if let o = themeObserver { NotificationCenter.default.removeObserver(o) }
        themeObserver = nil
        guard window != nil else { return }
        // A theme change within one appearance only shows in Rex's settings.
        themeObserver = NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: nil, queue: .main) { [weak self] _ in
            self?.updateLook()
        }
        updateLook()
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        updateLook()
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let t = tracking { removeTrackingArea(t) }
        let t = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeInActiveApp, .inVisibleRect], owner: self)
        addTrackingArea(t)
        tracking = t
    }

    /// Whether the pointer is over this button and the button can be seen.
    /// Hover state can't rely on mouseExited alone: hiding the button, or
    /// moving it out from under a resting pointer, sends none.
    private var pointerInside: Bool {
        guard let w = window, w.isVisible, !isHiddenOrHasHiddenAncestor, alphaValue > 0.01 else { return false }
        return bounds.contains(convert(w.mouseLocationOutsideOfEventStream, from: nil))
    }

    private func endHover() {
        tipTimer?.invalidate()
        tipTimer = nil
        if hovered { hovered = false }
        RexTip.shared.hide(for: self)
    }

    override func mouseEntered(with event: NSEvent) {
        guard pointerInside else { return }
        hovered = true
        tipTimer?.invalidate()
        if let title = tipTitle {
            tipTimer = Timer.scheduledTimer(withTimeInterval: 0.45, repeats: false) { [weak self] _ in
                guard let self = self else { return }
                guard self.hovered, self.pointerInside else { self.endHover(); return }
                RexTip.shared.show(title: title, shortcut: self.tipShortcut, below: self,
                                   stillHovered: { [weak self] in self?.pointerInside ?? false },
                                   lost: { [weak self] in self?.endHover() })
            }
        }
    }

    override func mouseExited(with event: NSEvent) { endHover() }

    override func mouseDown(with event: NSEvent) {
        endHover()
        super.mouseDown(with: event)
    }

    override func viewDidHide() {
        super.viewDidHide()
        endHover()
    }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if newWindow == nil { endHover() }
    }

    private func updateLook() {
        let look = style.colors(RexTheme.colors(for: self))
        contentTintColor = hovered ? look.hover : look.rest
        circle.backgroundColor = hovered ? look.circle.cgColor : nil
    }
}

/// Rex's own chrome icon button (the eye, ⌘ and + of its sidebar header), so
/// size, tint and hover are Rex's. Falls back to a borderless NSButton.
enum RexChrome {
    // Rex's ChromeIconButton traps in -initWithFrame: (Rex builds it with its
    // own Swift initializer), so the look is reproduced in RexStyleIconButton.
    static func makeIconButton(symbol: String, help: String, size: CGFloat, shortcut: String? = nil) -> NSButton {
        let b = RexStyleIconButton(frame: NSRect(x: 0, y: 0, width: size, height: size))
        b.tipTitle = help
        b.tipShortcut = shortcut
        b.imagePosition = .imageOnly
        b.image = NSImage(systemSymbolName: symbol, accessibilityDescription: help)
        return b
    }
}

struct RexIconButton: NSViewRepresentable {
    let symbol: String
    let help: String
    var shortcut: String? = nil
    let action: () -> Void

    final class Coordinator: NSObject {
        var action: () -> Void
        init(_ action: @escaping () -> Void) { self.action = action }
        @objc func fire() { action() }
    }

    func makeCoordinator() -> Coordinator { Coordinator(action) }

    func makeNSView(context: Context) -> NSButton {
        let b = RexChrome.makeIconButton(symbol: symbol, help: help, size: 28, shortcut: shortcut)
        b.target = context.coordinator
        b.action = #selector(Coordinator.fire)
        return b
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView: NSButton, context: Context) -> CGSize? {
        CGSize(width: 28, height: 28)
    }

    func updateNSView(_ b: NSButton, context: Context) {
        context.coordinator.action = action
        if let icon = b as? RexStyleIconButton, icon.tipTitle != help {
            icon.tipTitle = help
            icon.image = NSImage(systemSymbolName: symbol, accessibilityDescription: help)
        }
    }
}

/// Rex's selection and hover look for sidebar rows and tabs: the selected item
/// sits on Rex's selected-tab surface, a hovered one on a faint capsule.
struct RexItemBackground: ViewModifier {
    let selected: Bool
    let hover: Bool

    func body(content: Content) -> some View {
        content.background {
            if selected {
                RexSelectedSurface()
            } else if hover {
                Capsule().fill(Color.primary.opacity(0.06))
            }
        }
    }
}

/// Where the selected item of a row is.
struct SelectionBounds: PreferenceKey {
    static let defaultValue: Anchor<CGRect>? = nil
    static func reduce(value: inout Anchor<CGRect>?, nextValue: () -> Anchor<CGRect>?) {
        value = value ?? nextValue()
    }
}

/// The surface under Rex's selected sidebar tab, read from its layers
/// (SurfaceStackView under TabSelectionSurfaceView) in every Interface
/// setting:
/// - Classic, light: an opaque fill (the theme's surfaceRaised, or white
///   untinted) with a soft shadow, a hairline edge and a faint inner light.
/// - Classic, dark: no shadow; the theme fill lifted by a white overlay, or
///   about 13% white untinted.
/// - Glass: a regular Liquid Glass shape, tinted with surfaceRaised at 25% (or
///   white) in light mode with a shadow under it, untinted in dark mode.
/// - In an inactive window, a flat wash of the secondary content color.
struct RexSelectedSurface<S: InsettableShape>: View {
    var shape: S
    @ObservedObject private var look = RexLook.shared
    @Environment(\.colorScheme) private var scheme
    @Environment(\.controlActiveState) private var activeState

    var body: some View {
        let dark = scheme == .dark
        let t = RexTheme.colors(dark: dark)
        let content = Color(nsColor: t.content), secondary = Color(nsColor: t.contentSecondary)
        let raised = Color(nsColor: t.surfaceRaised)
        if activeState == .inactive {
            shape.fill(secondary.opacity(0.05))
        } else if #available(macOS 26.0, *), look.glass {
            if dark {
                Color.clear.glassEffect(.regular, in: shape)
            } else {
                ZStack {
                    ShadowOnly(shape: shape, color: (look.tinted ? secondary : Color.black.opacity(0.4)).opacity(0.15),
                               radius: 3.5, y: look.tinted ? 1 : 1.5)
                    Color.clear.glassEffect(.regular.tint((look.tinted ? raised : Color.white).opacity(0.25)), in: shape)
                }
            }
        } else if dark {
            ZStack {
                // Untinted, Rex's fill and overlay come out as about 13% white
                // over its backdrop (measured on screen); SwiftUI blends the
                // overlay against the transparent host instead, so the
                // measured result is drawn directly.
                if look.tinted {
                    shape.fill(raised)
                    shape.fill(Color.white.opacity(0.5)).blendMode(.overlay)
                } else {
                    shape.fill(Color.white.opacity(0.13))
                }
                shape.inset(by: 0.4).stroke(Color.white.opacity(0.1), lineWidth: 0.8).blendMode(.plusLighter)
            }
        } else {
            ZStack {
                shape.fill(look.tinted ? raised : Color.white)
                    .shadow(color: (look.tinted ? content.opacity(0.65) : Color.black.opacity(0.5)).opacity(0.15),
                            radius: 3.5, x: 0, y: 1.5)
                shape.stroke((look.tinted ? content : Color.black).opacity(0.1), lineWidth: 0.5)
                shape.inset(by: 0.35).stroke(Color.white.opacity(0.05), lineWidth: 0.7).blendMode(.plusLighter)
            }
        }
    }
}

extension RexSelectedSurface where S == Capsule {
    init() { self.init(shape: Capsule()) }
}

/// A shape's drop shadow without the shape, for surfaces that let the
/// background show through (Rex draws its glass tab's shadow on its own plane).
private struct ShadowOnly<S: Shape>: View {
    let shape: S
    let color: Color
    let radius: CGFloat
    let y: CGFloat

    var body: some View {
        shape.fill(Color.black)
            .shadow(color: color, radius: radius, x: 0, y: y)
            .mask {
                ZStack {
                    Rectangle().padding(-2 * radius - abs(y))
                    shape.blendMode(.destinationOut)
                }
                .compositingGroup()
            }
    }
}

extension View {
    func rexItemBackground(selected: Bool, hover: Bool) -> some View {
        modifier(RexItemBackground(selected: selected, hover: hover))
    }

    /// Marks this item as where the row's selection capsule goes.
    func selectionAnchor(_ selected: Bool) -> some View {
        anchorPreference(key: SelectionBounds.self, value: .bounds) { selected ? $0 : nil }
    }

    /// One capsule behind a row of pills or tabs that moves to the selected
    /// one. Liquid Glass ignores opacity, so a cross-fade would
    /// pop at the end of the animation; moving one surface stays smooth.
    func selectionSurface() -> some View {
        backgroundPreferenceValue(SelectionBounds.self) { anchor in
            GeometryReader { g in
                if let a = anchor {
                    let r = g[a]
                    RexSelectedSurface()
                        .frame(width: r.width, height: r.height)
                        .offset(x: r.minX, y: r.minY)
                }
            }
        }
    }
}

/// The Changes modes. Natural widths while they fit, so no label is cut short
/// just because another one is shorter; a narrow panel shares the width and
/// shrinks them.
struct Pills: View {
    let items: [(id: String, title: String)]
    @Binding var selection: String

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 2) {
                ForEach(Array(items.enumerated()), id: \.element.id) { i, item in
                    if i > 0 { Spacer(minLength: 0) }
                    pill(item, compact: false)
                }
            }
            .selectionSurface()
            HStack(spacing: 2) {
                ForEach(items, id: \.id) { pill($0, compact: true) }
            }
            .selectionSurface()
        }
        .animation(Motion.reflow, value: selection)
    }

    private func pill(_ item: (id: String, title: String), compact: Bool) -> some View {
        PillButton(title: item.title, selected: item.id == selection, compact: compact) { selection = item.id }
    }
}

private struct PillButton: View {
    let title: String
    let selected: Bool
    let compact: Bool
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 13))
                .foregroundStyle(selected ? Color.primary : Color.secondary)
                .animation(nil, value: selected) // see PanelTabButton
                .lineLimit(1)
                .minimumScaleFactor(compact ? 0.75 : 1)
                .fixedSize(horizontal: !compact, vertical: false)
                .padding(.horizontal, compact ? 2 : 10)
                .frame(height: 26)
                .frame(maxWidth: compact ? .infinity : nil)
                .rexItemBackground(selected: false, hover: hover && !selected)
                .selectionAnchor(selected)
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}

/// A panel tab: an icon, with its name only while selected, so all of them fit
/// a narrow panel. The narrowest panel has no room for the name at all.
///
/// Each tab draws its own surface: on a
/// click the new tab's surface fades in while the tab widens in place, and the
/// old one's fades out while it narrows; nothing slides across the row. Once
/// the row has settled (~170 ms) the icons swap colours and the new name fades
/// in.
private struct PanelTabButton: View {
    let title: String
    let symbol: String
    let selected: Bool
    var showsName = true
    var badge = 0
    let action: () -> Void
    @State private var hover = false
    /// Drawn as selected: follows `selected` once the row has settled.
    @State private var lit: Bool

    init(title: String, symbol: String, selected: Bool, showsName: Bool = true, badge: Int = 0, action: @escaping () -> Void) {
        self.title = title
        self.symbol = symbol
        self.selected = selected
        self.showsName = showsName
        self.badge = badge
        self.action = action
        _lit = State(initialValue: selected)
    }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 5) {
                Image(systemName: symbol).font(.system(size: 13))
                if selected && showsName {
                    // The name waits for the row to settle, then fades in; the
                    // old one fades while its tab narrows and clips it.
                    Text(title).font(.system(size: 13)).lineLimit(1).fixedSize()
                        .transition(.asymmetric(insertion: .opacity.animation(Motion.nameIn),
                                                removal: .opacity.animation(Motion.nameOut)))
                }
                if badge > 0 {
                    Text("\(badge)")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Palette.accent)
                        .contentTransition(.numericText())
                }
            }
            // Interpolating secondary into primary loses the vibrancy half
            // way and dims the text, so the colour switches in one step, after
            // the row has settled. Only the colour waits: the
            // tab widens with the row.
            .foregroundStyle(lit ? Color.primary : Color.secondary)
            .padding(.horizontal, selected && showsName ? 10 : 7)
            .frame(minWidth: 28)
            .frame(height: 26)
            .clipShape(Capsule())
            // Always there, faded in or out: a surface inserted on the click
            // would appear at the tab's final size; this one widens with it.
            .background {
                ZStack {
                    Capsule().fill(Color.primary.opacity(hover && !selected ? 0.06 : 0))
                    // Faded through a mask: Liquid Glass ignores opacity.
                    RexSelectedSurface().mask { Capsule().padding(-8).opacity(selected ? 1 : 0) }
                }
            }
            .contentShape(Capsule())
            .animation(Motion.fade, value: badge)
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help(title)
        .onHover { hover = $0 }
        .onChange(of: selected) { _, now in
            let swap = { var t = Transaction(); t.disablesAnimations = true; withTransaction(t) { lit = now } }
            guard let delay = Motion.colorSwapDelay else { return swap() }
            DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: swap)
        }
        .accessibilityLabel(badge > 0 ? "\(title), \(badge) pending" : title)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

struct IconButton: View {
    let symbol: String
    let help: String
    var spinning = false
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(hover ? Color.primary : Color.secondary)
                .rotationEffect(.degrees(spinning ? 180 : 0))
                .animation(spinning ? .linear(duration: 0.6).repeatForever(autoreverses: false) : .default, value: spinning)
                .frame(width: 24, height: 22)
                .background(RoundedRectangle(cornerRadius: 6).fill(hover ? Color.primary.opacity(0.08) : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .onHover { hover = $0 }
    }
}

struct RowView: View {
    let row: TreeRow
    @ObservedObject var model: PanelModel
    let isOpen: Bool
    let showPath: Bool
    @State private var hover = false
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let n = row.node
        let deleted = n.change?.status == "D"
        HStack(spacing: 6) {
            if row.depth > 0 { Spacer().frame(width: CGFloat(row.depth) * 12) }
            if n.isDir {
                Image(systemName: "chevron.right")
                    .font(.system(size: 8.5, weight: .bold))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(isOpen ? 90 : 0))
                    .animation(Motion.reflow, value: isOpen)
                    .frame(width: 10)
                if let img = FlowIcons.shared.folder(n.name, open: isOpen, dark: scheme == .dark) {
                    Image(nsImage: img).resizable().interpolation(.high).frame(width: 16, height: 16)
                } else {
                    Image(systemName: "folder.fill")
                        .font(.system(size: 12))
                        .foregroundStyle(Color.secondary.opacity(0.8))
                        .frame(width: 16)
                }
                Text(n.name)
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            } else {
                Spacer().frame(width: 10)
                let base = String(n.name.split(separator: "/").last ?? Substring(n.name))
                if let img = FlowIcons.shared.file(base, dark: scheme == .dark) {
                    Image(nsImage: img).resizable().interpolation(.high).frame(width: 16, height: 16)
                } else {
                    let icon = fileIcon(n.name)
                    Image(systemName: icon.0)
                        .font(.system(size: 11.5, weight: .medium))
                        .foregroundStyle(icon.1)
                        .frame(width: 16)
                }
                if showPath, let slash = n.name.lastIndex(of: "/") {
                    (Text(String(n.name[n.name.index(after: slash)...])).foregroundColor(.primary)
                        + Text("  " + String(n.name[..<slash])).foregroundColor(.secondary))
                        .font(.system(size: 13))
                        .lineLimit(1)
                        .truncationMode(.middle)
                } else {
                    Text(n.name)
                        .font(.system(size: 13))
                        .foregroundStyle(deleted ? Color.secondary : Color.primary)
                        .strikethrough(deleted, color: .secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            Spacer(minLength: 6)
            // Pending review comments: always on a file, on a folder only
            // while it's closed (an open one shows them on its files).
            if let k = model.commentCounts[n.id], k > 0, !n.isDir || !isOpen {
                CommentCount(count: k)
                    .help("\(k) pending review comment\(k == 1 ? "" : "s")")
            }
            if let c = n.change {
                if hover && !(c.binary ?? false) && (c.add > 0 || c.del > 0) {
                    HStack(spacing: 4) {
                        if c.add > 0 { Text("+\(c.add)").foregroundStyle(Palette.added) }
                        if c.del > 0 { Text("−\(c.del)").foregroundStyle(Palette.deleted) }
                    }
                    .font(.system(size: 10.5, design: .monospaced))
                }
                Text(c.status)
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(Palette.status(c.status))
                    .frame(width: 12)
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, 10)
        .frame(height: 24)
        .rexItemBackground(selected: model.selected == n.id && !n.isDir, hover: hover)
        .background(alignment: .leading) {
            // Indent guides: one line per level, through the chevron column.
            ZStack(alignment: .leading) {
                ForEach(0..<row.depth, id: \.self) { level in
                    Rectangle()
                        .fill(Color.primary.opacity(0.1))
                        .frame(width: 1)
                        .offset(x: 6 + 5 + CGFloat(level) * 12)
                }
            }
        }
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .onTapGesture {
            if n.isDir { model.toggleFolder(n.id) } else if !deleted { model.open(n.id) }
        }
        // One element per row for VoiceOver (and automation), acting like the tap.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(n.isDir ? "\(n.name) folder" : n.id)
        .accessibilityAddTraits(.isButton)
        .accessibilityAction {
            if n.isDir { model.toggleFolder(n.id) } else if !deleted { model.open(n.id) }
        }
        .help(n.change.map { c in
            c.oldPath.map { "\($0) → \(n.id)" } ?? "\(n.id)  +\(c.add) −\(c.del)"
        } ?? n.id)
        .contextMenu {
            if !n.isDir && !deleted {
                Button("Open in Editor") { model.open(n.id) }
            }
            Button("Copy Relative Path") { copyToPasteboard(n.id) }
            if let repo = model.target?.repo {
                Button("Reveal in Finder") {
                    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: repo + "/" + n.id)])
                }
            }
        }
    }
}

/// What a file row does when clicked, in VoiceOver and in its context menu.
private struct FileRowBehavior: ViewModifier {
    let path: String
    let deleted: Bool
    @ObservedObject var model: PanelModel

    func body(content: Content) -> some View {
        content
            .onTapGesture { if !deleted { model.open(path) } }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(path)
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { if !deleted { model.open(path) } }
            .contextMenu {
                if !deleted { Button("Open in Editor") { model.open(path) } }
                Button("Copy Relative Path") { copyToPasteboard(path) }
                if let repo = model.target?.repo {
                    Button("Reveal in Finder") {
                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: repo + "/" + path)])
                    }
                }
            }
    }
}

private func copyToPasteboard(_ s: String) {
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(s, forType: .string)
}

/// One changed file in the Changes list: status first, then the name and,
/// dimmer, its folder.
struct ChangeRow: View {
    let change: FileChangeDTO
    @ObservedObject var model: PanelModel
    @State private var hover = false

    var body: some View {
        let c = change
        let deleted = c.status == "D"
        let (name, folder) = splitPath(c.path)
        HStack(spacing: 8) {
            Text(c.status)
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(Palette.status(c.status))
                .frame(width: 12)
            (Text(name).foregroundColor(deleted ? .secondary : .primary).strikethrough(deleted, color: .secondary)
                + Text(folder.isEmpty ? "" : "  " + folder).foregroundColor(.secondary))
                .font(.system(size: 13))
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 6)
            if let k = model.commentCounts[c.path], k > 0 {
                CommentCount(count: k)
                    .help("\(k) pending review comment\(k == 1 ? "" : "s")")
            }
            if hover && !(c.binary ?? false) && (c.add > 0 || c.del > 0) {
                HStack(spacing: 4) {
                    if c.add > 0 { Text("+\(c.add)").foregroundStyle(Palette.added) }
                    if c.del > 0 { Text("−\(c.del)").foregroundStyle(Palette.deleted) }
                }
                .font(.system(size: 10.5, design: .monospaced))
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, 10)
        .frame(height: 24)
        .rexItemBackground(selected: model.selected == c.path, hover: hover)
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .modifier(FileRowBehavior(path: c.path, deleted: deleted, model: model))
        .help(c.oldPath.map { "\($0) → \(c.path)" } ?? "\(c.path)  +\(c.add) −\(c.del)")
    }

    private func splitPath(_ p: String) -> (name: String, folder: String) {
        guard let slash = p.lastIndex(of: "/") else { return (p, "") }
        return (String(p[p.index(after: slash)...]), String(p[..<slash]))
    }
}

// MARK: - Empty states

struct EmptyAction {
    let title: String
    var shortcut: String?
    let run: () -> Void
}

/// A centred tile (Rex's selected surface) with a symbol, a title, one line of help and an
/// optional action, in Rex's type scale.
struct EmptyState: View {
    let symbol: String?
    let title: String
    let message: String?
    var action: EmptyAction?
    var pulse = false
    @State private var hover = false
    @State private var dim = false

    var body: some View {
        VStack(spacing: 12) {
            ZStack {
                RexSelectedSurface(shape: RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .frame(width: 46, height: 46)
                if let symbol = symbol {
                    Image(systemName: symbol)
                        .font(.system(size: 19, weight: .regular))
                        .foregroundStyle(.secondary)
                        .opacity(pulse && dim ? 0.35 : 1)
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            VStack(spacing: 4) {
                Text(title)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.primary)
                if let message = message {
                    Text(message)
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .lineSpacing(1.5)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let action = action {
                Button(action: action.run) {
                    HStack(spacing: 6) {
                        Text(action.title).font(.system(size: 12.5))
                        if let k = action.shortcut {
                            Text(k).font(.system(size: 11)).foregroundStyle(.tertiary)
                        }
                    }
                    .padding(.horizontal, 12)
                    .frame(height: 28)
                    .rexItemBackground(selected: true, hover: false)
                    .opacity(hover ? 1 : 0.9)
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .onHover { hover = $0 }
                .padding(.top, 2)
            }
        }
        .frame(maxWidth: 236)
        .frame(maxWidth: .infinity)
        .padding(.top, 64)
        .onAppear {
            if pulse { withAnimation(.easeInOut(duration: 0.9).repeatForever()) { dim = true } }
        }
    }
}

// MARK: - Panel

struct PanelView: View {
    @ObservedObject var model: PanelModel
    var topInset: CGFloat
    var onHide: () -> Void

    /// Tabs shown at least once. Building a tab's content takes longer than a
    /// frame (60-70 ms for Files or Info), which stalled the tab animation on
    /// every switch; a tab is built the first time it's shown and then kept,
    /// hidden while another one is selected.
    @State private var built: Set<PanelTab> = []

    // Always drawn: the container hides it once Rex's content has slid
    // back over it, so it doesn't vanish before the animation ends.
    var body: some View { full }

    private var full: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
                .frame(height: 50)
            ZStack(alignment: .topLeading) {
                ForEach(panelTabs, id: \.tab) { t in
                    if built.contains(t.tab) || model.tab == t.tab {
                        let shown = model.tab == t.tab
                        VStack(alignment: .leading, spacing: 0) {
                            content(t.tab)
                            Spacer(minLength: 0)
                        }
                        // The content switches at once.
                        .opacity(shown ? 1 : 0)
                        .animation(nil, value: model.tab)
                        .allowsHitTesting(shown)
                        .accessibilityHidden(!shown)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            footer
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .ignoresSafeArea()
        .onAppear { built.insert(model.tab) }
        .onChange(of: model.tab) { _, tab in built.insert(tab) }
    }

    @ViewBuilder private func content(_ tab: PanelTab) -> some View {
        switch tab {
        case .info: InfoView(model: model, noRepo: AnyView(noRepoState))
        case .changes: changes
        case .files: filesView
        }
    }

    /// One row of tabs and the hide button, centred 25pt below the top like
    /// Rex's title row. Actions live on the tab they act on.
    private var header: some View {
        HStack(spacing: 2) {
            ViewThatFits(in: .horizontal) {
                tabs(showNames: true)
                tabs(showNames: false)
            }
            Spacer(minLength: 4)
            RexIconButton(symbol: "sidebar.right", help: "Hide Changes Panel", shortcut: "⌘⇧E", action: onHide)
                .frame(width: 28, height: 28)
        }
        .padding(.leading, 6)
        .padding(.trailing, 10)
    }

    /// The whole row reflows together, so the tabs beside the ones that
    /// change move with them; the content below switches at once.
    private func tabs(showNames: Bool) -> some View {
        HStack(spacing: 2) {
            ForEach(panelTabs, id: \.tab) { t in
                PanelTabButton(title: t.title, symbol: t.symbol, selected: model.tab == t.tab, showsName: showNames,
                               badge: t.tab == .changes ? model.state?.pendingComments ?? 0 : 0) {
                    // Explicit: the row sits in a ViewThatFits, which doesn't
                    // pass an implicit animation on to its children's frames.
                    withAnimation(Motion.reflow) { model.tab = t.tab }
                }
            }
        }
    }

    @ViewBuilder private var repoLine: some View {
        if let t = model.target, let repo = t.repoName {
            HStack(spacing: 5) {
                if t.isGit ?? false {
                    Image(systemName: "arrow.triangle.branch").font(.system(size: 10.5))
                    Text(t.branch ?? "").font(.system(size: 11.5, weight: .medium))
                    Text("·").foregroundStyle(.tertiary)
                }
                Text(repo).font(.system(size: 11.5))
                if let agent = t.agent {
                    Spacer()
                    agentBadge(t, agent: agent)
                }
            }
            .foregroundStyle(.secondary)
            .lineLimit(1)
        }
    }

    /// The agent the panel follows. With more than one agent in the tab it
    /// is a menu: picking one focuses that pane and the panel follows it.
    @ViewBuilder private func agentBadge(_ t: TargetDTO, agent: String) -> some View {
        let agents = t.agents ?? []
        let current = agents.first { $0.blockId == t.agentBlockId }?.name ?? agent
        let label = HStack(spacing: 2) {
            Text(current).font(.system(size: 10, weight: .semibold))
            if agents.count > 1 {
                Image(systemName: "chevron.up.chevron.down").font(.system(size: 7, weight: .bold))
            }
        }
        .padding(.horizontal, 5).padding(.vertical, 1)
        .background(Capsule().fill(Color.primary.opacity(0.08)))
        if agents.count > 1 {
            Menu {
                ForEach(agents) { a in
                    Button { model.follow(a.blockId) } label: {
                        if a.blockId == t.agentBlockId { Label(a.name, systemImage: "checkmark") } else { Text(a.name) }
                    }
                }
            } label: { label }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .fixedSize()
            .help("Following \(current). The panel follows the agent you focused last; pick another here.")
        } else {
            label.help("Following the \(agent) pane of this tab")
        }
    }

    @ViewBuilder private var changes: some View {
        if model.target?.repo == nil {
            noRepoState
        } else if model.target?.isGit != true {
            EmptyState(symbol: "arrow.triangle.branch", title: "Not a git repository",
                       message: "Changes, branch and comments need git. The Files tab still lists this folder.")
        } else {
            branchLine
                .padding(.leading, 12)
                .padding(.trailing, 10)
                .padding(.bottom, 6)
            Pills(items: changeModes, selection: $model.mode)
                .padding(.leading, 6)
                .padding(.trailing, 10)
                .padding(.bottom, 8)
            let comments = (model.state?.comments ?? []).sorted { ($0.file, $0.line) < ($1.file, $1.line) }
            let noFiles = model.currentChanges?.files.isEmpty ?? true
            if noFiles && comments.isEmpty {
                noChangesState
            } else {
                ScrollView {
                    if !comments.isEmpty { CommentsSection(model: model, comments: comments) }
                    if noFiles {
                        groupHeader("Changed", count: 0, first: true)
                    } else if model.changesAsTree {
                        changesTree
                    } else {
                        changesList
                    }
                }
            }
        }
    }

    /// The branch and what changed in the selected mode.
    private var branchLine: some View {
        HStack(spacing: 6) {
            Text(model.target?.branch ?? "")
                .font(.system(size: 13.5, weight: .semibold))
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 6)
            if let c = model.currentChanges {
                HStack(spacing: 5) {
                    if c.add > 0 { Text("+\(c.add)").foregroundStyle(Palette.added) }
                    if c.del > 0 { Text("−\(c.del)").foregroundStyle(Palette.deleted) }
                }
                .font(.system(size: 11.5))
                .contentTransition(.numericText())
                .fixedSize()
            }
            HStack(spacing: 2) {
                RexIconButton(symbol: model.changesAsTree ? "list.bullet" : "list.bullet.indent",
                              help: model.changesAsTree ? "Show as List" : "Show as Tree") {
                    model.changesAsTree.toggle()
                }
                .frame(width: 28, height: 28)
                RexIconButton(symbol: "arrow.clockwise", help: "Refresh") { model.refresh(reason: "button") }
                    .frame(width: 28, height: 28)
            }
        }
    }

    private var changesList: some View {
        let groups = model.changeGroups()
        return LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(groups) { g in
                groupHeader(g.title, count: g.files.count, first: g.id == groups.first?.id)
                ForEach(g.files, id: \.path) { f in ChangeRow(change: f, model: model) }
                Spacer().frame(height: 8)
            }
        }
        .padding(.bottom, 2)
    }

    private var changesTree: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            groupHeader("Changed", count: model.currentChanges?.files.count ?? 0, first: true)
            ForEach(model.changeRows()) { r in
                RowView(row: r, model: model, isOpen: !model.collapsed.contains(r.id), showPath: false)
            }
        }
        .padding(.bottom, 2)
    }

    /// A group's title and count. The first one also says what the mode
    /// compares with, or that the agent is still working.
    private func groupHeader(_ title: String, count: Int, first: Bool) -> some View {
        HStack(spacing: 6) {
            SectionTitle("\(title) (\(count))")
            Spacer(minLength: 6)
            if first, let c = model.currentChanges {
                if c.mode == "last-turn", c.running == true {
                    HStack(spacing: 4) {
                        WorkingDot()
                        Text("in progress")
                    }
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .help("\(model.agentName) is working; changes since the turn started")
                } else if c.mode == "branch", let ref = c.baseRef {
                    Text("vs \(ref)").font(.system(size: 11.5)).foregroundStyle(.tertiary)
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 4)
    }

    @ViewBuilder private var filesView: some View {
        if model.target?.repo == nil {
            noRepoState
        } else {
            HStack(spacing: 2) {
                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass").font(.system(size: 11)).foregroundStyle(.secondary)
                    TextField("Find", text: $model.filter)
                        .textFieldStyle(.plain)
                        .font(.system(size: 12.5))
                        .accessibilityLabel("Find in files")
                    if !model.filter.isEmpty {
                        Button { model.filter = "" } label: {
                            Image(systemName: "xmark.circle.fill").foregroundStyle(.tertiary)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Clear filter")
                    }
                }
                .padding(.horizontal, 10)
                .frame(height: 28)
                .background(Capsule().fill(Color.primary.opacity(0.06)))
                .padding(.trailing, 4)
                RexIconButton(symbol: "doc.text.magnifyingglass", help: "Find File", shortcut: "⌘P") { model.openPicker() }
                    .frame(width: 28, height: 28)
                RexIconButton(symbol: "arrow.clockwise", help: "Refresh") {
                    model.refresh(reason: "button")
                    model.refreshFiles()
                }
                .frame(width: 28, height: 28)
            }
            .padding(.leading, 6)
            .padding(.trailing, 10)
            .padding(.bottom, 6)
            let rows = model.fileRows()
            if rows.isEmpty {
                if model.files.isEmpty {
                    EmptyState(symbol: nil, title: "Loading files…", message: nil)
                } else {
                    EmptyState(symbol: "magnifyingglass", title: "No matches",
                               message: "Nothing in this repository matches “\(model.filter)”.",
                               action: EmptyAction(title: "Clear filter") { model.filter = "" })
                }
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(rows) { r in
                            RowView(row: r, model: model, isOpen: model.expandedFiles.contains(r.id),
                                    showPath: !model.filter.isEmpty)
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        }
    }

    /// The bottom line: branch and repo, or a message for a few seconds.
    @ViewBuilder private var footer: some View {
        Group {
            if let f = model.flash {
                Text(f)
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                repoLine
            }
        }
        // The branch glyph lines up with the left edge of the tab pills.
        .padding(.leading, 8)
        .padding(.trailing, 10)
        .padding(.vertical, 10)
    }

    @ViewBuilder private var noRepoState: some View {
        if model.state == nil {
            EmptyState(symbol: nil, title: "Loading…", message: nil)
        } else if let err = model.state?.error {
            EmptyState(symbol: "rectangle.dashed", title: "Nothing to follow", message: err)
        } else {
            EmptyState(symbol: "folder.badge.questionmark", title: "No folder in this tab",
                       message: "The panel follows this tab's coding agent (Claude Code, Codex, OpenCode, Pi or Antigravity). Start one in a folder, or cd somewhere here.")
        }
    }

    @ViewBuilder private var noChangesState: some View {
        let c = model.currentChanges
        let branch = model.target?.branch ?? "this branch"
        let find = EmptyAction(title: "Find a file", shortcut: "⌘P") { model.openPicker() }
        switch c?.mode {
        case "last-turn" where c?.base == nil:
            EmptyState(symbol: "sparkles", title: "No turn yet",
                       message: "Files \(model.agentName) touches in its next turn show up here. This needs the rexmod hooks (scripts/install-hooks.sh).")
        case "last-turn" where c?.running == true:
            EmptyState(symbol: "ellipsis.circle", title: "\(model.agentName) is working",
                       message: "Nothing has changed in this turn yet.", pulse: true)
        case "last-turn":
            EmptyState(symbol: "checkmark.circle", title: "Last turn changed nothing",
                       message: "\(model.agentName)'s last reply didn't touch any files.", action: find)
        case "branch":
            EmptyState(symbol: "arrow.triangle.branch", title: "Nothing new on this branch",
                       message: "\(branch) has no changes against \(c?.baseRef ?? "its base").", action: find)
        default:
            EmptyState(symbol: "checkmark.seal", title: "Working tree is clean",
                       message: "No uncommitted changes on \(branch).", action: find)
        }
    }
}


/// The number of pending comments on a tree row.
struct CommentCount: View {
    let count: Int
    var body: some View {
        HStack(spacing: 2) {
            Image(systemName: "text.bubble.fill").font(.system(size: 9))
            Text("\(count)").font(.system(size: 10.5, weight: .semibold))
        }
        .foregroundStyle(Palette.accent)
    }
}

/// Pending review comments, at the top of the Changes tab: click one to open
/// the editor at it; Send pastes them all into the agent now (otherwise they
/// ride along with your next message).
struct CommentsSection: View {
    @ObservedObject var model: PanelModel
    let comments: [CommentDTO]
    @State private var hoverSend = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                SectionTitle("Comments (\(comments.count))")
                Spacer(minLength: 6)
                // No default-action shortcut: this isn't a dialog, and Return
                // belongs to the terminal next to it.
                Button { model.sendComments() } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "paperplane").font(.system(size: 10.5))
                        Text("Send").font(.system(size: 12))
                    }
                    .padding(.horizontal, 9)
                    .frame(height: 22)
                    .rexItemBackground(selected: true, hover: false)
                    .opacity(hoverSend ? 1 : 0.9)
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .onHover { hoverSend = $0 }
                .help("Send \(comments.count) comment\(comments.count == 1 ? "" : "s") to \(model.agentName) now; otherwise they go with your next message")
                .accessibilityLabel("Send \(comments.count) to \(model.agentName)")
            }
            .padding(.leading, 12)
            .padding(.trailing, 10)
            .padding(.bottom, 4)
            VStack(alignment: .leading, spacing: 2) {
                ForEach(comments) { c in
                    CommentRow(comment: c) { model.open(c.file, line: c.line) }
                }
            }
            .padding(.horizontal, 6)
            Spacer().frame(height: 10)
        }
    }
}

/// A section label, the same on every tab.
struct SectionTitle: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text)
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .contentTransition(.numericText())
    }
}

/// Who and what the panel is following, and what you can do with it.
struct InfoView: View {
    @ObservedObject var model: PanelModel
    let noRepo: AnyView

    var body: some View {
        if let t = model.target, let path = t.cwd ?? t.repo {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    directory(path)
                    if t.agent != nil { agent(t) }
                    if let u = model.info?.usage { UsageSection(usage: u) }
                    if let p = model.info?.processes, !p.isEmpty { processes(p) }
                    if let p = model.info?.ports, !p.isEmpty { ports(p) }
                    review
                }
                .padding(.vertical, 6)
            }
            .onAppear { model.refreshInfo() }
        } else {
            noRepo
        }
    }

    private func directory(_ path: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            SectionTitle("Working Directory").padding(.horizontal, 12)
            Text((path as NSString).abbreviatingWithTildeInPath)
                .font(.system(size: 12.5))
                .lineLimit(3)
                .truncationMode(.middle)
                .textSelection(.enabled)
                .padding(.horizontal, 12)
                .padding(.bottom, 2)
            InfoAction(title: "Copy Path", symbol: "doc.on.doc") { copyToPasteboard(path); model.show("Copied \((path as NSString).abbreviatingWithTildeInPath)") }
            InfoAction(title: "Reveal in Finder", symbol: "folder") {
                NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
            }
            if model.target?.repo != nil {
                InfoAction(title: "Find File", symbol: "doc.text.magnifyingglass", shortcut: "⌘P") { model.openPicker() }
            }
        }
    }

    /// The followed agent: what it is doing, its session, and the other
    /// agents of the tab, any of which the panel can follow instead.
    private func agent(_ t: TargetDTO) -> some View {
        let list = t.agents ?? []
        let info = model.info?.agent?.blockId == t.agentBlockId ? model.info?.agent : nil
        return VStack(alignment: .leading, spacing: 4) {
            SectionTitle(t.agent.map { $0 == "Claude" ? "Claude Code" : $0 } ?? "Agent").padding(.horizontal, 12)
            AgentStatusLine(status: info?.status, message: info?.message)
                .padding(.horizontal, 12)
                .padding(.bottom, 2)
            if let id = info?.sessionId {
                InfoAction(title: "Copy Session ID", symbol: "doc.on.doc") { copyToPasteboard(id); model.show("Copied the session ID") }
            }
            if let resume = info?.resume {
                InfoAction(title: "Copy Resume Command", symbol: "terminal") { copyToPasteboard(resume); model.show("Copied “\(resume)”") }
            }
            if list.count > 1 {
                ForEach(list) { a in
                    let current = a.blockId == t.agentBlockId
                    InfoAction(title: a.name, symbol: current ? "checkmark" : "circle", detail: current ? "following" : nil) {
                        model.follow(a.blockId)
                    }
                    .help(current ? "The panel follows \(a.name)" : "Follow \(a.name)")
                }
            }
        }
    }

    /// What runs in the agent's pane: its shell's jobs.
    private func processes(_ list: [InfoDTO.Proc]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            SectionTitle("Process").padding(.horizontal, 12)
            ForEach(list) { p in
                HStack(spacing: 8) {
                    Circle().fill(Palette.added).frame(width: 7, height: 7).frame(width: 16)
                    Text(p.name).font(.system(size: 12.5)).lineLimit(1)
                    Text(String(p.pid)).font(.system(size: 11.5, design: .monospaced)).foregroundStyle(.tertiary)
                    Spacer(minLength: 6)
                    Text(Elapsed.text(p.seconds)).font(.system(size: 11.5)).foregroundStyle(.secondary)
                        .contentTransition(.numericText())
                }
                .padding(.horizontal, 12)
                .frame(height: 24)
                .accessibilityElement(children: .combine)
            }
        }
    }

    /// Ports the tab's panes listen on; a click opens one in the browser.
    private func ports(_ list: [InfoDTO.Port]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            SectionTitle("Ports").padding(.horizontal, 12)
            ForEach(list) { p in
                InfoAction(title: "localhost:\(p.port)", symbol: "globe", detail: p.name) {
                    if let url = URL(string: "http://localhost:\(p.port)") { NSWorkspace.shared.open(url) }
                }
                .help("Open http://localhost:\(p.port) (\(p.name), pid \(p.pid))")
            }
        }
    }

    private var review: some View {
        let n = model.state?.pendingComments ?? 0
        return VStack(alignment: .leading, spacing: 4) {
            SectionTitle("Review").padding(.horizontal, 12)
            if n > 0 {
                InfoAction(title: "\(n) pending comment\(n == 1 ? "" : "s")", symbol: "text.bubble") {
                    model.tab = .changes
                }
            } else {
                Text("No pending comments")
                    .font(.system(size: 12.5))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
            }
        }
    }
}

/// "Working", "Waiting for you" or "Idle", as the agent reports it to Rex.
struct AgentStatusLine: View {
    let status: String?
    let message: String?

    var body: some View {
        let (label, color): (String, Color) = {
            switch status {
            case "working": return ("Working", Palette.accent)
            case "blocked": return ("Waiting for you", Palette.modified)
            case nil, "": return ("No status reported", .secondary)
            default: return ("Idle", .secondary)
            }
        }()
        HStack(spacing: 8) {
            Group {
                if status == "working" { WorkingDot() } else { Circle().fill(color).frame(width: 6, height: 6) }
            }
            .frame(width: 16)
            Text(label).font(.system(size: 12.5)).foregroundStyle(status == "working" || status == "blocked" ? Color.primary : Color.secondary)
            if let m = message, !m.isEmpty, status == "working" || status == "blocked" {
                Text(m).font(.system(size: 11.5)).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}

/// The plan's rate-limit windows: how much is used and when each resets.
struct UsageSection: View {
    let usage: InfoDTO.Usage

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            SectionTitle("Usage").padding(.horizontal, 12)
            ForEach(usage.windows, id: \.label) { w in
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        Text(w.label == "5h" ? "Session (5h)" : w.label == "7d" ? "Week (7d)" : w.label)
                            .font(.system(size: 12.5))
                        Spacer(minLength: 6)
                        Text("\(Int(w.used.rounded()))%")
                            .font(.system(size: 12, weight: .medium).monospacedDigit())
                            .contentTransition(.numericText())
                    }
                    UsageBar(fraction: w.used / 100)
                    if let r = w.resetsAt.flatMap(Elapsed.date), r > Date() {
                        Text("Resets in \(Elapsed.text(Int(r.timeIntervalSinceNow), coarse: true))")
                            .font(.system(size: 11)).foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, 12)
                .accessibilityElement(children: .combine)
            }
            if let f = usage.fetchedAt.flatMap(Elapsed.date) {
                Text("\(usage.source == "Claude" ? "Claude Code" : usage.source) · updated \(Elapsed.text(max(Int(-f.timeIntervalSinceNow), 0), coarse: true)) ago")
                    .font(.system(size: 11)).foregroundStyle(.tertiary)
                    .padding(.horizontal, 12)
            }
        }
    }
}

/// A thin capsule filled to `fraction`; amber past 80%, red past 95%.
struct UsageBar: View {
    let fraction: Double

    var body: some View {
        let f = min(max(fraction, 0), 1)
        let color = f >= 0.95 ? Palette.deleted : f >= 0.8 ? Palette.modified : Palette.added
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.primary.opacity(0.08))
                Capsule().fill(color).frame(width: max(g.size.width * f, f > 0 ? 4 : 0))
            }
        }
        .frame(height: 4)
        .animation(Motion.fade, value: f)
    }
}

/// Short durations and ISO dates, the way the Info tab writes them.
enum Elapsed {
    static func text(_ seconds: Int, coarse: Bool = false) -> String {
        let s = max(seconds, 0)
        let d = s / 86400, h = s % 86400 / 3600, m = s % 3600 / 60
        if d > 0 { return h > 0 ? "\(d)d \(h)h" : "\(d)d" }
        if h > 0 { return m > 0 ? "\(h)h \(m)m" : "\(h)h" }
        if m > 0 { return coarse ? "\(m)m" : "\(m)m \(s % 60)s" }
        return coarse ? "<1m" : "\(s)s"
    }

    private static let iso = ISO8601DateFormatter()

    /// Microseconds are more than the formatter takes, so they go.
    static func date(_ s: String) -> Date? {
        iso.date(from: s.replacingOccurrences(of: #"\.\d+"#, with: "", options: .regularExpression))
    }
}

/// A row in the Info tab: an icon and what it does.
private struct InfoAction: View {
    let title: String
    let symbol: String
    var shortcut: String? = nil
    var detail: String? = nil
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Image(systemName: symbol)
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .frame(width: 16)
                Text(title).font(.system(size: 12.5)).lineLimit(1)
                Spacer(minLength: 6)
                if let detail = detail {
                    Text(detail).font(.system(size: 11.5)).foregroundStyle(.secondary)
                }
                if let shortcut = shortcut {
                    Text(shortcut).font(.system(size: 11)).foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 6)
            .frame(height: 26)
            .rexItemBackground(selected: false, hover: hover)
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
        .padding(.horizontal, 6)
    }
}

struct CommentRow: View {
    let comment: CommentDTO
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        let c = comment
        let range = c.endLine > c.line ? "\(c.line)-\(c.endLine)" : "\(c.line)"
        let name = String(c.file.split(separator: "/").last ?? Substring(c.file))
        Button(action: action) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(name).font(.system(size: 12, weight: .medium)).foregroundStyle(.primary)
                    Text(":" + range).font(.system(size: 11, design: .monospaced)).foregroundStyle(.secondary)
                    Spacer(minLength: 0)
                }
                .lineLimit(1)
                Text(c.text.split(separator: "\n").first.map(String.init) ?? "")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 5)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 6).fill(Color.primary.opacity(hover ? 0.08 : 0)))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(c.file + ":" + range + "\n\n" + c.text)
        .onHover { hover = $0 }
    }
}
