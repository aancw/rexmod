import AppKit
import Combine
import Darwin
import ObjectiveC
import MachO
import SwiftUI

@_cdecl("rexmod_panel_start")
public func rexmodPanelStart() {
    PanelHost.shared.start()
}

/// Makes room for the panel inside Rex the way Rex makes room for its own
/// sidebar. Rex's root view keeps the whole window, so its window-wide
/// backdrop (the material its sidebar sits on, a sibling view) also runs under
/// the panel; only the SwiftUI host of Rex's chrome (sidebar + panes) gets
/// narrower. SwiftUI alone sizes that host, so a setFrameSize override on its
/// class trims the width without fighting a layout engine.
enum ChromeInset {
    static var inset: CGFloat = 0
    private static var installed = Set<ObjectIdentifier>()

    static func findChrome(in root: NSView) -> NSView? {
        var queue = [root]
        while !queue.isEmpty {
            let v = queue.removeFirst()
            if String(describing: type(of: v)) == "ChromeContainerView" { return v }
            queue.append(contentsOf: v.subviews)
        }
        return nil
    }

    /// The SwiftUI platform-view host that holds ChromeContainerView.
    static func host(of chrome: NSView) -> NSView? {
        guard let h = chrome.superview, String(describing: type(of: h)).hasPrefix("AppKitPlatformViewHost") else { return nil }
        return h
    }

    static func install(on host: NSView) -> Bool {
        guard let cls = object_getClass(host) else { return false }
        let key = ObjectIdentifier(cls)
        if installed.contains(key) { return true }
        let sel = #selector(NSView.setFrameSize(_:))
        guard let method = class_getInstanceMethod(cls, sel) else { return false }
        typealias SetFrameSize = @convention(c) (AnyObject, Selector, NSSize) -> Void
        let original = unsafeBitCast(method_getImplementation(method), to: SetFrameSize.self)
        let block: @convention(block) (AnyObject, NSSize) -> Void = { obj, size in
            var s = size
            if inset > 0, let v = obj as? NSView, let sup = v.superview {
                s.width = min(s.width, max(sup.bounds.width - inset, 300))
            }
            original(obj, sel, s)
        }
        let imp = imp_implementationWithBlock(block)
        if !class_addMethod(cls, sel, imp, method_getTypeEncoding(method)) {
            method_setImplementation(method, imp)
        }
        installed.insert(key)
        RM.log("chrome host inset installed on \(String(describing: cls).prefix(60))")
        return true
    }

    /// Gives the panel this much room now. An animation calls it on every
    /// frame, so the override above clamps Rex's own layout passes to the
    /// width the animation has reached, not to where it ends.
    static func set(host: NSView, inset newInset: CGFloat) {
        guard let sup = host.superview else { return }
        inset = newInset
        let target = NSSize(width: max(sup.bounds.width - newInset, 300), height: host.frame.height)
        guard abs(host.frame.width - target.width) > 0.25 else { return }
        host.setFrameSize(target)
    }

    /// Room the hidden panel's toggle takes at the right end of the title row.
    static private(set) var reserveTrailing: CGFloat = 0
    /// Runs on the next run loop turn after a pane title row changed size.
    static var onTitleFrameChange: (() -> Void)?
    private static var titleInstalled = Set<ObjectIdentifier>()
    private static var cutKey: UInt8 = 0
    static var isPrepared: Bool { !titleInstalled.isEmpty }

    /// How much narrower than Rex asked this row currently is.
    static func cut(of title: NSView) -> CGFloat {
        CGFloat((objc_getAssociatedObject(title, &cutKey) as? NSNumber)?.doubleValue ?? 0)
    }

    /// Whether a row of this width, at its current origin, ends the top row of
    /// panes at the window's right edge.
    static func isTopRight(_ v: NSView, width: CGFloat) -> Bool {
        guard let sup = v.superview, let root = v.window?.contentView else { return false }
        let r = sup.convert(NSRect(x: v.frame.minX, y: v.frame.minY, width: width, height: v.frame.height), to: root)
        return r.maxY > root.bounds.maxY - 64 && r.maxX > root.bounds.maxX - 60
    }

    /// Rex lays out a pane title row from the row's own width: in Rex 1726
    /// the actions end at bounds.width − base − presentedTrailingInset
    /// (0x10029846c), and every SwiftUI update rewrites trailingSpacing from
    /// its own parameters and lays the row out again without going through
    /// -layout (0x1002978f4 → 0x100297c78 → 0x100297e70). So the row's width
    /// is the one input that holds: giving the top-right row less width makes
    /// Rex place its title and actions with room for the toggle on every
    /// path. The width arrives through -setFrameSize:, which this overrides.
    /// Installs the override once per class and sizes the rows that existed
    /// before it, so they get the room too. Later rows go through it anyway.
    static func prepare(_ titles: [NSView]) {
        var fresh = false
        for t in titles { fresh = installTitleInset(on: t) || fresh }
        if fresh {
            for t in titles { t.setFrameSize(NSSize(width: t.frame.width + cut(of: t), height: t.frame.height)) }
        }
    }

    @discardableResult
    static func installTitleInset(on title: NSView) -> Bool {
        guard let cls = object_getClass(title), !titleInstalled.contains(ObjectIdentifier(cls)) else { return false }
        let sel = #selector(NSView.setFrameSize(_:))
        guard let method = class_getInstanceMethod(cls, sel) else { return false }
        typealias SetFrameSize = @convention(c) (AnyObject, Selector, NSSize) -> Void
        let original = unsafeBitCast(method_getImplementation(method), to: SetFrameSize.self)
        let block: @convention(block) (AnyObject, NSSize) -> Void = { obj, size in
            var s = size
            if let v = obj as? NSView {
                // AppKit can hand back the width this override produced;
                // anything else is Rex's own, full width.
                let previous = cut(of: v)
                let full = (previous > 0 && abs(size.width - v.frame.width) < 0.5) ? size.width + previous : size.width
                let c = (reserveTrailing > 0 && isTopRight(v, width: full)) ? reserveTrailing : 0
                s.width = max(full - c, 0)
                objc_setAssociatedObject(v, &cutKey, NSNumber(value: Double(c)), .OBJC_ASSOCIATION_RETAIN_NONATOMIC)
            }
            original(obj, sel, s)
            DispatchQueue.main.async { onTitleFrameChange?() }
        }
        let imp = imp_implementationWithBlock(block)
        if !class_addMethod(cls, sel, imp, method_getTypeEncoding(method)) {
            method_setImplementation(method, imp)
        }
        // A row is sized before it joins the window, when it can't tell
        // whether it is top-right; size it again once it is in the window.
        let moveSel = #selector(NSView.viewDidMoveToWindow)
        if let moveMethod = class_getInstanceMethod(cls, moveSel) {
            typealias DidMove = @convention(c) (AnyObject, Selector) -> Void
            let originalMove = unsafeBitCast(method_getImplementation(moveMethod), to: DidMove.self)
            let moveBlock: @convention(block) (AnyObject) -> Void = { obj in
                originalMove(obj, moveSel)
                guard let v = obj as? NSView, v.window != nil else { return }
                DispatchQueue.main.async {
                    v.setFrameSize(NSSize(width: v.frame.width + cut(of: v), height: v.frame.height))
                }
            }
            let moveImp = imp_implementationWithBlock(moveBlock)
            if !class_addMethod(cls, moveSel, moveImp, method_getTypeEncoding(moveMethod)) {
                method_setImplementation(moveMethod, moveImp)
            }
        }
        titleInstalled.insert(ObjectIdentifier(cls))
        return true
    }

    /// Changes the room and resizes the rows, which Rex then lays out.
    static func setReserve(_ value: CGFloat, titles: [NSView]) {
        guard value != reserveTrailing else { return }
        reserveTrailing = value
        for t in titles {
            t.setFrameSize(NSSize(width: t.frame.width + cut(of: t), height: t.frame.height))
        }
    }

    static func titleViews(in root: NSView) -> [NSView] {
        var out: [NSView] = []
        var queue = [root]
        while !queue.isEmpty {
            let v = queue.removeFirst()
            if String(describing: type(of: v)) == "PaneTrafficLightTitleHostingView" { out.append(v); continue }
            queue.append(contentsOf: v.subviews)
        }
        return out
    }

    /// The label of the session this window shows, read from Rex's sidebar:
    /// the selected tab sits under TabSelectionSurfaceView, and the nearest
    /// SessionHeaderView above it names its session. Works with the sidebar
    /// collapsed too, and per window, so two Rex apps don't mix up.
    static func displayedSessionLabel(in root: NSView) -> String? {
        var queue = [root]
        var doc: NSView?
        while !queue.isEmpty {
            let v = queue.removeFirst()
            if String(describing: type(of: v)) == "SidebarScrollDocumentView" { doc = v; break }
            queue.append(contentsOf: v.subviews)
        }
        guard let doc = doc,
              let sel = doc.subviews.first(where: { String(describing: type(of: $0)) == "TabSelectionSurfaceView" })
        else { return nil }
        let row = sel.frame
        var best: (pos: CGFloat, label: String)?
        for h in doc.subviews where String(describing: type(of: h)) == "SessionHeaderView" {
            guard let label = h.subviews.compactMap({ $0 as? NSTextField }).first?.stringValue, !label.isEmpty else { continue }
            // Headers come before their tabs: above them on screen.
            let above = doc.isFlipped ? h.frame.minY <= row.minY : h.frame.minY >= row.maxY
            guard above else { continue }
            let pos = doc.isFlipped ? h.frame.minY : -h.frame.minY
            if best == nil || pos > best!.pos { best = (pos, label) }
        }
        return best?.label
    }

}

/// The hidden panel's toggle: Rex's own chrome icon button, sized like the
/// pane actions it joins, with a dot while review comments wait.
final class ToggleButton: NSObject {
    let button: NSButton
    private let dot = CALayer()
    private let onClick: () -> Void

    init(onClick: @escaping () -> Void) {
        self.onClick = onClick
        button = RexChrome.makeIconButton(symbol: "sidebar.right", help: "Show Changes Panel", size: 22, shortcut: "⌘⇧E")
        // It joins the pane title actions, so it takes their look: ~11pt
        // glyph, their rest/hover colours and their hover circle.
        (button as? RexStyleIconButton)?.style = .paneAction
        button.image = button.image?.withSymbolConfiguration(.init(pointSize: 11, weight: .regular))
        super.init()
        button.target = self
        button.action = #selector(fire)
        button.wantsLayer = true
        dot.backgroundColor = NSColor(red: 0.73, green: 0, blue: 1, alpha: 1).cgColor
        dot.cornerRadius = 3
        dot.frame = CGRect(x: 15, y: 15, width: 6, height: 6)
        dot.isHidden = true
        button.layer?.addSublayer(dot)
    }

    var showsDot: Bool {
        get { !dot.isHidden }
        set { dot.isHidden = !newValue }
    }

    @objc private func fire() { onClick() }
}

/// Wraps Rex's own content view and puts the Changes panel on its right.
final class RexModContainer: NSView {
    let content: NSView
    let panel: NSView
    let backdrop = BackdropMirror()
    let toggle: NSView
    let toggleControl: ToggleButton
    private let divider: DividerView
    private weak var chromeHost: NSView?
    private let edge = EdgeSpring()
    /// The room Rex's content gives the panel right now; mid-animation, less
    /// or more than the panel's width.
    private var shownInset: CGFloat = 0
    /// Shows the part of the panel Rex's content has uncovered. The panel
    /// sits above Rex's views (Rex's backdrop spans the whole window, so it
    /// can't go below them), and without this it would draw over Rex's
    /// content mid-animation. With it, the panel is revealed and covered by
    /// the moving edge, as Rex's sidebar is by its content card. The clip and
    /// the panel keep their frames while the edge moves; only the clip's mask
    /// layer does, so the panel isn't laid out again on every frame.
    private let panelClip = NSView()
    private let reveal = CALayer()

    var panelWidth: CGFloat {
        didSet {
            RM.defaults.set(Double(panelWidth), forKey: "rexmod.panelWidth")
            layoutPanels(animated: false)
        }
    }

    var panelVisible: Bool {
        didSet { if oldValue != panelVisible { layoutPanels(animated: true) } }
    }

    init(content: NSView, panel: NSView, toggle: ToggleButton) {
        self.content = content
        self.panel = panel
        self.toggleControl = toggle
        self.toggle = toggle.button
        divider = DividerView()
        let w = RM.defaults.double(forKey: "rexmod.panelWidth")
        panelWidth = w > 0 ? CGFloat(w) : 320
        panelVisible = PanelHost.shared.model.panelVisible
        super.init(frame: content.frame)
        autoresizingMask = [.width, .height]
        autoresizesSubviews = false
        content.autoresizingMask = []
        addSubview(backdrop)
        addSubview(content)
        panelClip.wantsLayer = true
        panelClip.layer?.masksToBounds = true
        reveal.backgroundColor = NSColor.black.cgColor
        panelClip.layer?.mask = reveal
        panelClip.autoresizesSubviews = false
        panel.autoresizingMask = []
        panelClip.addSubview(panel)
        addSubview(panelClip)
        addSubview(self.toggle)
        divider.container = self
        addSubview(divider)
        syncChrome()
        layoutPanels(animated: false)
    }

    var insetMode: Bool { chromeHost?.window != nil }
    var isAnimating: Bool { edge.isRunning }

    /// Finds Rex's chrome constraint (the chrome appears a moment after the
    /// window). Without it, Rex is narrowed whole and a copy of its backdrop
    /// fills the panel area.
    func syncChrome() {
        if !insetMode, let c = ChromeInset.findChrome(in: content), let h = ChromeInset.host(of: c),
           ChromeInset.install(on: h) {
            chromeHost = h
            backdrop.isHidden = true
            layoutPanels(animated: false)
        }
        if !insetMode {
            backdrop.isHidden = false
            backdrop.sync(from: content)
        }
        // Title rows appear a moment after the window; hook them once.
        let titles = ChromeInset.titleViews(in: content)
        if !titles.isEmpty && !ChromeInset.isPrepared {
            ChromeInset.prepare(titles)
            if !panelVisible { positionToggle() }
        }
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { false }

    override func resizeSubviews(withOldSize oldSize: NSSize) { layoutPanels(animated: false) }
    override func layout() {
        super.layout()
        layoutPanels(animated: false)
    }

    private func clampedWidth() -> CGFloat {
        min(max(panelWidth, 220), max(bounds.width * 0.6, 220))
    }

    func layoutPanels(animated requested: Bool) {
        let animated = requested && !Motion.reduced
        let w = bounds.width, h = bounds.height
        let pw = panelVisible ? clampedWidth() : 0
        // Like Rex's sidebar, the panel stays put: Rex's content slides off
        // it and back over it (applyInset clips it to the uncovered part).
        if insetMode { content.frame = bounds } else { backdrop.frame = bounds }
        if animated && abs(shownInset - pw) > 0.5 {
            edge.run(on: self, from: shownInset, to: pw, step: { [weak self] v in self?.applyInset(v) },
                     done: { [weak self] in self?.positionToggle() })
        } else if !edge.isRunning || abs(edge.target - pw) > 0.5 {
            // A resize or a drag mid-animation: go straight to the new width.
            edge.stop()
            applyInset(pw)
        }
        divider.frame = NSRect(x: w - pw - 3, y: 0, width: 6, height: h)
        divider.isHidden = !panelVisible
        toggle.isHidden = panelVisible
        if panelVisible {
            ChromeInset.setReserve(0, titles: ChromeInset.titleViews(in: content))
        } else {
            positionToggle()
            if animated {
                toggle.alphaValue = 0
                // Once the spring has all but arrived (99% at ~230 ms).
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
                    guard let self = self, !self.panelVisible else { return }
                    self.positionToggle()
                    NSAnimationContext.runAnimationGroup { ctx in
                        ctx.duration = 0.15
                        self.toggle.animator().alphaValue = 1
                    }
                }
            } else {
                toggle.alphaValue = 1
            }
        }
    }

    /// Narrows Rex's content to leave `inset` points on the right.
    private func applyInset(_ inset: CGFloat) {
        shownInset = inset
        let w = bounds.width, h = bounds.height, cw = clampedWidth()
        let clip = NSRect(x: w - cw, y: 0, width: cw, height: h)
        if panelClip.frame != clip { panelClip.frame = clip }
        let full = NSRect(x: 0, y: 0, width: cw, height: h)
        if panel.frame != full { panel.frame = full }
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        reveal.frame = CGRect(x: cw - min(max(inset, 0), cw), y: 0, width: min(max(inset, 0), cw), height: h)
        CATransaction.commit()
        panelClip.isHidden = inset < 0.5
        // The spring overshoots by ~1%. Rex's edge does too, but a terminal
        // pushed past its final width rewraps (and its program redraws) twice
        // more once the edge settles, so the terminal stops at the target.
        let terminalInset = min(max(inset, 0), cw)
        if insetMode, let host = chromeHost {
            ChromeInset.set(host: host, inset: terminalInset)
        } else {
            content.frame = NSRect(x: 0, y: 0, width: max(bounds.width - terminalInset, 100), height: bounds.height)
        }
    }

    /// Puts the toggle in the room the top-right title row leaves.
    func positionToggle() {
        guard !panelVisible else { return }
        let size = NSSize(width: 22, height: 22)
        let titles = ChromeInset.titleViews(in: content)
        ChromeInset.setReserve(size.width + 2, titles: titles)
        ChromeInset.prepare(titles)
        guard let row = titles.first(where: { ChromeInset.isTopRight($0, width: $0.frame.width + ChromeInset.cut(of: $0)) }),
              let actions = row.subviews.first(where: { String(describing: type(of: $0)) == "ActionsHostingView" })
        else {
            toggle.frame = NSRect(x: bounds.maxX - size.width - 12, y: bounds.maxY - size.height - 13,
                                  width: size.width, height: size.height)
            return
        }
        let r = row.convert(row.bounds, to: self)
        let a = actions.convert(actions.bounds, to: self)
        var x = r.maxX + 2
        // While the chrome animates, aim at where the row ends up.
        if let host = chromeHost {
            x += (bounds.width - host.convert(host.bounds, to: self).maxX)
        }
        toggle.frame = NSRect(x: x, y: a.midY - size.height / 2, width: size.width, height: size.height)
    }
}

/// Rex draws no background for its sidebar: the sidebar is transparent over a
/// window-wide BackdropView (visual effect + tinted planes). The panel does
/// the same, over a copy of that backdrop that spans the whole window behind
/// Rex, so both sides look identical and Rex's rounded corners meet the copy.
final class BackdropMirror: NSView {
    private var signature = ""

    override var isFlipped: Bool { false }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    private static func findBackdrop(_ root: NSView) -> NSView? {
        var queue = [root]
        while !queue.isEmpty {
            let v = queue.removeFirst()
            if String(describing: type(of: v)) == "BackdropView", v.bounds.width >= root.bounds.width * 0.9 {
                return v
            }
            queue.append(contentsOf: v.subviews)
        }
        return nil
    }

    private static func describe(_ v: NSView, _ depth: Int = 0) -> String {
        var d = "\(type(of: v))|\(v.isHidden)|\(v.alphaValue)"
        if let e = v as? NSVisualEffectView {
            d += "|ve\(e.material.rawValue),\(e.blendingMode.rawValue),\(e.state.rawValue),\(e.appearance?.name.rawValue ?? "")"
        }
        if let l = v.layer {
            d += "|\(l.backgroundColor.map { "\($0)" } ?? "")|\(l.cornerRadius)|\(l.opacity)"
        }
        if depth < 6 { d += "[" + v.subviews.map { describe($0, depth + 1) }.joined(separator: ",") + "]" }
        return d
    }

    private func clone(_ src: NSView, into parent: NSView, depth: Int) {
        for sv in src.subviews {
            let copy: NSView
            if let ve = sv as? NSVisualEffectView {
                let c = NSVisualEffectView()
                c.material = ve.material
                c.blendingMode = ve.blendingMode
                c.state = ve.state
                c.isEmphasized = ve.isEmphasized
                c.appearance = ve.appearance
                copy = c
            } else {
                copy = NSView()
            }
            copy.wantsLayer = true
            if let l = sv.layer, let cl = copy.layer {
                cl.backgroundColor = l.backgroundColor
                cl.cornerRadius = l.cornerRadius
                cl.cornerCurve = l.cornerCurve
                cl.masksToBounds = l.masksToBounds || l.cornerRadius > 0
                cl.opacity = l.opacity
            }
            copy.isHidden = sv.isHidden
            copy.alphaValue = sv.alphaValue
            copy.frame = parent.bounds
            copy.autoresizingMask = [.width, .height]
            parent.addSubview(copy)
            if depth < 6 { clone(sv, into: copy, depth: depth + 1) }
        }
    }

    /// Copies Rex's backdrop again when the theme or appearance changed.
    func sync(from rexRoot: NSView) {
        guard let src = Self.findBackdrop(rexRoot) else { return }
        let sig = Self.describe(src)
        guard sig != signature else { return }
        signature = sig
        subviews.forEach { $0.removeFromSuperview() }
        wantsLayer = true
        appearance = src.appearance
        clone(src, into: self, depth: 0)
    }
}

/// The draggable edge between Rex and the panel.
final class DividerView: NSView {
    weak var container: RexModContainer?

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: .resizeLeftRight)
    }

    override var mouseDownCanMoveWindow: Bool { false }
    override func mouseDown(with event: NSEvent) {}

    override func mouseDragged(with event: NSEvent) {
        guard let c = container else { return }
        let p = c.convert(event.locationInWindow, from: nil)
        c.panelWidth = min(max(c.bounds.width - p.x, 220), c.bounds.width * 0.6)
    }

    override func mouseUp(with event: NSEvent) {
        window?.invalidateCursorRects(for: self)
    }
}

final class PanelHost: NSObject {
    static let shared = PanelHost()
    let model = PanelModel()
    private var containers: [ObjectIdentifier: RexModContainer] = [:]
    private var started = false
    private var keyMonitor: Any?
    private var stateObserver: AnyCancellable?

    func start() {
        guard !started else { return }
        started = true
        RM.log("panel start, rexmod=\(RM.rexmodPath)")
        UpdateHook.install()
        let nc = NotificationCenter.default
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didBecomeMainNotification] {
            nc.addObserver(self, selector: #selector(windowChanged(_:)), name: name, object: nil)
        }
        nc.addObserver(self, selector: #selector(windowClosing(_:)), name: NSWindow.willCloseNotification, object: nil)
        // SwiftUI creates its windows a little after launch; look a few times.
        for delay in [0.0, 0.5, 1.5, 3.0, 6.0] {
            DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in self?.scan() }
        }
        Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in self?.scan() }
        Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            self.model.setSession(self.displayedSession())
        }
        stateObserver = model.$state.sink { [weak self] st in
            let pending = (st?.pendingComments ?? 0) > 0
            self?.containers.values.forEach { $0.toggleControl.showsDot = pending }
        }
        // Rex resizes the title rows on every frame of the edge animation;
        // placing the toggle walks the whole view tree, so it waits for the
        // edge to settle (the toggle is hidden until then anyway).
        ChromeInset.onTitleFrameChange = { [weak self] in
            self?.containers.values.forEach { if !$0.isAnimating { $0.positionToggle() } }
        }

        keyMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            let flags = e.modifierFlags.intersection([.command, .shift, .option, .control])
            if flags == [.command, .shift] && e.keyCode == 14 { // E
                self?.toggle(window: e.window)
                return nil
            }
            return e
        }
        model.isVisible = { [weak self] in
            self?.containers.values.contains { $0.window?.isVisible == true } ?? false
        }
        Shell.rexmod(["warm"]) { _, _, _ in } // cache the login env so the first editor opens fast
        // Reading the icon theme takes a moment; done on first use, it
        // stalls the first tree (and the tab animation showing it).
        DispatchQueue.global(qos: .utility).async { _ = FlowIcons.shared }
        model.start()
        registerNotify("dev.rexmod.toggle") { [weak self] in self?.toggle(window: NSApp.keyWindow) }
        registerNotify("dev.rexmod.refresh") { [weak self] in self?.model.refresh(reason: "notify") }
        registerNotify("dev.rexmod.snapshot") { [weak self] in self?.snapshot() }
        registerNotify("dev.rexmod.dumpviews") { [weak self] in self?.dumpViews() }
        registerNotify("dev.rexmod.dumplayers") { [weak self] in self?.dumpLayers() }
        registerNotify("dev.rexmod.dumpchrome") { [weak self] in self?.dumpChrome() }
        registerNotify("dev.rexmod.dumpheader") { [weak self] in self?.dumpHeader() }
        registerNotify("dev.rexmod.dumpsidebar") { [weak self] in self?.dumpSidebar() }
        registerNotify("dev.rexmod.dumptitle") { [weak self] in self?.dumpTitleClass() }
        registerNotify("dev.rexmod.dumpimps") { [weak self] in self?.dumpTitleIMPs() }
        registerNotify("dev.rexmod.hover.rex") { [weak self] in self?.hoverProbe(ours: false) }
        registerNotify("dev.rexmod.dumpicons") { [weak self] in self?.dumpChromeIcons() }
        registerNotify("dev.rexmod.dumpiconstyle") { [weak self] in self?.dumpIconStyle() }
        registerNotify("dev.rexmod.hover.ours") { [weak self] in self?.hoverProbe(ours: true) }
        registerNotify("dev.rexmod.hover.eye") { [weak self] in self?.hoverButton(rex: true) }
        registerNotify("dev.rexmod.hover.move") { [weak self] in self?.moveProbe() }
        registerNotify("dev.rexmod.hover.header") { [weak self] in self?.hoverButton(rex: false) }
        // Opens the first changed file, exactly as a click on its row does.
        registerNotify("dev.rexmod.debug.openfirst") { [weak self] in
            guard let m = self?.model, let row = m.changeRows().first(where: { !$0.node.isDir }) else { return }
            m.open(row.node.id)
        }
        for t in panelTabs {
            registerNotify("dev.rexmod.tab.\(t.tab.rawValue)") { [weak self] in self?.model.tab = t.tab }
        }
        for (i, m) in changeModes.enumerated() {
            registerNotify("dev.rexmod.mode.\(i)") { [weak self] in self?.model.mode = m.id }
        }
    }

    private func registerNotify(_ name: String, _ handler: @escaping () -> Void) {
        rexmod_notify_register(name, handler)
    }

    @objc private func windowChanged(_ n: Notification) {
        if let w = n.object as? NSWindow { attachIfEligible(w) }
    }

    @objc private func windowClosing(_ n: Notification) {
        if let w = n.object as? NSWindow { containers.removeValue(forKey: ObjectIdentifier(w)) }
    }

    private func scan() {
        for w in NSApp.windows { attachIfEligible(w) }
        for c in containers.values { c.syncChrome() }
        // SwiftUI may swap the content view; put the panel back if it did.
        for (key, c) in containers where c.window == nil || c.window?.contentView !== c {
            containers.removeValue(forKey: key)
        }
    }

    private func describe(_ w: NSWindow) -> String {
        "\(type(of: w)) id=\(w.identifier?.rawValue ?? "-") autosave=\(w.frameAutosaveName) title=\(w.title) "
            + "content=\(w.contentView.map { String(describing: type(of: $0)) } ?? "-") "
            + "vc=\(w.contentViewController.map { String(describing: type(of: $0)) } ?? "-") "
            + "style=\(w.styleMask.rawValue) frame=\(w.frame)"
    }

    private func eligible(_ w: NSWindow) -> Bool {
        if w is NSPanel || !w.styleMask.contains(.titled) { return false }
        let id = w.identifier?.rawValue ?? ""
        let name = w.frameAutosaveName
        if id.localizedCaseInsensitiveContains("settings") || name.localizedCaseInsensitiveContains("settings") { return false }
        if id.contains("AppWindow") || name.contains("AppWindow") || name == "Workspace" || id.contains("Workspace") { return true }
        return false
    }

    private var described = Set<ObjectIdentifier>()

    private func attachIfEligible(_ w: NSWindow) {
        let key = ObjectIdentifier(w)
        if !described.contains(key) {
            described.insert(key)
            RM.log("window: \(describe(w)) eligible=\(eligible(w))")
        }
        guard eligible(w), let original = w.contentView, !(original is RexModContainer) else { return }
        let topInset = max(w.frame.height - w.contentLayoutRect.height, 28)
        let hosting = NSHostingView(rootView: PanelView(model: model, topInset: topInset, onHide: { [weak self, weak w] in
            self?.toggle(window: w)
        }))
        hosting.translatesAutoresizingMaskIntoConstraints = true
        let toggleView = ToggleButton { [weak self, weak w] in self?.toggle(window: w) }
        let container = RexModContainer(content: original, panel: hosting, toggle: toggleView)
        w.contentView = container
        container.layoutPanels(animated: false)
        containers[key] = container
        RM.log("attached panel to \(describe(w)) topInset=\(topInset)")
    }

    /// The session the key (or only) Rex window shows.
    private func displayedSession() -> String? {
        let c = NSApp.keyWindow.flatMap { containers[ObjectIdentifier($0)] } ?? containers.values.first
        if let c = c, let label = ChromeInset.displayedSessionLabel(in: c.content) { return label }
        return RM.focusedSessionID()
    }

    func toggle(window: NSWindow?) {
        let c = window.flatMap { containers[ObjectIdentifier($0)] } ?? containers.values.first
        guard c != nil else { return }
        model.panelVisible.toggle()
        for c in containers.values { c.panelVisible = model.panelVisible }
        // Fresh state can redraw the panel; let the edge settle first (99%
        // of the way at ~230 ms).
        if model.panelVisible {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak self] in self?.model.refresh(reason: "shown") }
        }
    }

    /// Reads the look of a visible Rex ChromeIconButton at rest and hovered.
    private func dumpIconStyle() {
        var out: [String] = []
        func ivar(_ o: AnyObject, _ n: String) -> UnsafeMutableRawPointer? {
            guard let iv = class_getInstanceVariable(type(of: o), n) else { return nil }
            return Unmanaged.passUnretained(o).toOpaque().advanced(by: ivar_getOffset(iv))
        }
        func obj(_ o: AnyObject, _ n: String) -> AnyObject? {
            guard let iv = class_getInstanceVariable(type(of: o), n) else { return nil }
            return object_getIvar(o, iv) as AnyObject?
        }
        func describe(_ b: NSButton, _ tag: String) {
            var line = "\(tag): frame=\(b.frame) tint=\(b.contentTintColor?.description ?? "nil")"
            if let p = ivar(b, "restingForegroundOpacity") { line += " resting=\(p.load(as: Double.self))" }
            if let p = ivar(b, "showsHoverBackground") { line += " showsHover=\(p.load(as: UInt8.self))" }
            if let p = ivar(b, "isHovered") { line += " isHovered=\(p.load(as: UInt8.self))" }
            if let p = ivar(b, "backgroundInset") { line += " inset=\(p.load(as: Double.self))" }
            line += " hoverColor=\(obj(b, "hoverBackgroundColor").map { "\($0)" } ?? "nil")"
            line += " fill=\(obj(b, "backgroundFillColor").map { "\($0)" } ?? "nil")"
            if let l = obj(b, "insetBackgroundLayer") as? CALayer {
                line += " bgLayer(frame=\(l.frame) cr=\(l.cornerRadius) curve=\(l.cornerCurve.rawValue) bg=\(l.backgroundColor.map { "\($0)" } ?? "nil") op=\(l.opacity) hidden=\(l.isHidden))"
            }
            out.append(line)
        }
        for c in containers.values {
            var queue: [NSView] = [c.content]
            var found: NSButton?
            while !queue.isEmpty {
                let v = queue.removeFirst()
                if String(describing: type(of: v)) == "ChromeIconButton", let b = v as? NSButton, !b.isHidden,
                   b.convert(b.bounds, to: c).minX > 0 { found = b; break }
                queue.append(contentsOf: v.subviews)
            }
            guard let b = found else { continue }
            describe(b, "rest")
            if let w = b.window, let e = NSEvent.enterExitEvent(with: .mouseEntered, location: b.convert(NSPoint(x: b.bounds.midX, y: b.bounds.midY), to: nil),
                                                              modifierFlags: [], timestamp: 0, windowNumber: w.windowNumber, context: nil,
                                                              eventNumber: 0, trackingNumber: 0, userData: nil) {
                b.mouseEntered(with: e)
                b.layoutSubtreeIfNeeded()
                describe(b, "hover")
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) {
                    describe(b, "hover+0.4s")
                    try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("iconstyle.txt"), atomically: true, encoding: .utf8)
                    if let e2 = NSEvent.enterExitEvent(with: .mouseExited, location: .zero, modifierFlags: [], timestamp: 0, windowNumber: w.windowNumber,
                                                       context: nil, eventNumber: 0, trackingNumber: 0, userData: nil) { b.mouseExited(with: e2) }
                }
            }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("iconstyle.txt"), atomically: true, encoding: .utf8)
    }

    /// Describes Rex's ChromeIconButtons (sidebar header icons), for reuse.
    private func dumpChromeIcons() {
        var out: [String] = []
        for c in containers.values {
            var queue: [NSView] = [c.content]
            while !queue.isEmpty {
                let v = queue.removeFirst()
                if String(describing: type(of: v)) == "ChromeIconButton" {
                    var chain: [String] = []
                    var k: AnyClass? = object_getClass(v)
                    while let cls = k { chain.append(NSStringFromClass(cls)); k = class_getSuperclass(cls) }
                    var line = "ICON rect=\(v.convert(v.bounds, to: c).integral) hidden=\(v.isHidden) alpha=\(v.alphaValue) chain=\(chain.prefix(4).joined(separator: " < "))"
                    if let b = v as? NSButton {
                        line += " bordered=\(b.isBordered) bezel=\(b.bezelStyle.rawValue) imagePos=\(b.imagePosition.rawValue) tint=\(b.contentTintColor?.description ?? "nil")"
                        if let img = b.image { line += " image=\(img.name() ?? "?") size=\(img.size) symbolCfg=\(img.symbolConfiguration.description)" }
                        line += " action=\(b.action.map(NSStringFromSelector) ?? "nil") tooltip=\(b.toolTip ?? "")"
                    }
                    line += " subviews=\(v.subviews.map { String(describing: type(of: $0)) })"
                    out.append(line)
                    if let cls = object_getClass(v) {
                        var n: UInt32 = 0
                        if let ivars = class_copyIvarList(cls, &n) {
                            out.append("  ivars: " + (0..<Int(n)).compactMap { ivar_getName(ivars[$0]).map { String(cString: $0) } }.joined(separator: ", "))
                            free(ivars)
                        }
                        if let methods = class_copyMethodList(cls, &n) {
                            out.append("  methods: " + (0..<Int(n)).map { NSStringFromSelector(method_getName(methods[$0])) }.joined(separator: ", "))
                            free(methods)
                        }
                    }
                }
                queue.append(contentsOf: v.subviews)
            }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("icons.txt"), atomically: true, encoding: .utf8)
    }

    /// Sends a window-level mouse move over Rex's first pane action, the path
    /// real pointer movement takes, so SwiftUI hover state updates.
    private func moveProbe() {
        for c in containers.values {
            guard let w = c.window,
                  let row = ChromeInset.titleViews(in: c.content).first(where: { ChromeInset.isTopRight($0, width: $0.frame.width + ChromeInset.cut(of: $0)) }),
                  let a = row.subviews.first(where: { String(describing: type(of: $0)) == "ActionsHostingView" }),
                  let b = a.subviews.filter({ $0.frame.width >= 16 && $0.frame.width <= 32 }).sorted(by: { $0.frame.minX < $1.frame.minX }).first
            else { continue }
            let p = b.convert(NSPoint(x: b.bounds.midX, y: b.bounds.midY), to: nil)
            RM.log("moveProbe key=\(w.isKeyWindow) point=\(p)")
            for dx in stride(from: -30.0, through: 0.0, by: 10.0) {
                if let e = NSEvent.mouseEvent(with: .mouseMoved, location: NSPoint(x: p.x + dx, y: p.y), modifierFlags: [],
                                              timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: w.windowNumber,
                                              context: nil, eventNumber: 0, clickCount: 0, pressure: 0) {
                    w.sendEvent(e)
                }
            }
        }
    }

    /// Hovers Rex's first visible sidebar header icon, or our first header icon.
    private func hoverButton(rex: Bool) {
        for c in containers.values {
            var queue: [NSView] = [rex ? c.content : c.panel]
            while !queue.isEmpty {
                let v = queue.removeFirst()
                let name = String(describing: type(of: v))
                let match = rex ? name == "ChromeIconButton" : v is RexStyleIconButton
                if match, let b = v as? NSButton, !b.isHidden, b.convert(b.bounds, to: c).minX > 100 || !rex,
                   let w = b.window,
                   let e = NSEvent.enterExitEvent(with: .mouseEntered, location: b.convert(NSPoint(x: b.bounds.midX, y: b.bounds.midY), to: nil),
                                                  modifierFlags: [], timestamp: 0, windowNumber: w.windowNumber, context: nil,
                                                  eventNumber: 0, trackingNumber: 0, userData: nil) {
                    b.mouseEntered(with: e)
                    RM.log("hovered \(name) at \(b.convert(b.bounds, to: c))")
                    return
                }
                queue.append(contentsOf: v.subviews)
            }
        }
    }

    /// Sends a hover to Rex's first pane action, or to our toggle, so the
    /// hover look can be captured and compared.
    private func hoverProbe(ours: Bool) {
        for c in containers.values {
            guard let w = c.window else { continue }
            let target: NSView?
            var point = NSPoint.zero
            if ours {
                target = c.toggle
                point = c.toggle.convert(NSPoint(x: c.toggle.bounds.midX, y: c.toggle.bounds.midY), to: nil)
            } else {
                let row = ChromeInset.titleViews(in: c.content).first { ChromeInset.isTopRight($0, width: $0.frame.width + ChromeInset.cut(of: $0)) }
                target = row?.subviews.first { String(describing: type(of: $0)) == "ActionsHostingView" }
                if let a = target, let b = a.subviews.filter({ $0.frame.width >= 16 && $0.frame.width <= 32 }).sorted(by: { $0.frame.minX < $1.frame.minX }).first {
                    point = b.convert(NSPoint(x: b.bounds.midX, y: b.bounds.midY), to: nil)
                }
            }
            guard let view = target else { continue }
            for type in [NSEvent.EventType.mouseEntered, .mouseMoved] {
                let e: NSEvent?
                if type == .mouseEntered {
                    e = NSEvent.enterExitEvent(with: type, location: point, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime,
                                               windowNumber: w.windowNumber, context: nil, eventNumber: 0, trackingNumber: 0, userData: nil)
                } else {
                    e = NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: ProcessInfo.processInfo.systemUptime,
                                           windowNumber: w.windowNumber, context: nil, eventNumber: 0, clickCount: 0, pressure: 0)
                }
                guard let ev = e else { continue }
                for t in view.trackingAreas {
                    if type == .mouseEntered { (t.owner as? NSResponder)?.mouseEntered(with: ev) } else { (t.owner as? NSResponder)?.mouseMoved(with: ev) }
                }
                if type == .mouseEntered { view.mouseEntered(with: ev) } else { view.mouseMoved(with: ev) }
                for sv in view.subviews { for t in sv.trackingAreas { (t.owner as? NSResponder)?.mouseEntered(with: ev) } }
            }
        }
    }

    /// Writes the file addresses of the title view's methods and its ivar
    /// offsets, so the code can be read with llvm-objdump.
    private func dumpTitleIMPs() {
        var out: [String] = []
        var slide = 0
        for i in 0..<_dyld_image_count() where String(cString: _dyld_get_image_name(i)).hasSuffix("/MacOS/Rex") {
            slide = _dyld_get_image_vmaddr_slide(i)
        }
        out.append("slide=0x\(String(slide, radix: 16))")
        // Field offset globals the title layout reads (file addresses).
        for a in [0x1019ac778, 0x1019ac780, 0x1019ac788, 0x1019ac790, 0x1019ac798, 0x1019ac7a0, 0x1019ac7a8,
                  0x1019ac7b0, 0x1019ac7b8, 0x1019ac7c0, 0x1019ac7c8, 0x1019ac7d0, 0x1019ac7d8, 0x1019ac7e0, 0x1019ac7e8,
                  0x101add0f8] {
            if let p = UnsafeRawPointer(bitPattern: a + slide) {
                out.append("global 0x\(String(a, radix: 16)) = 0x\(String(p.load(as: Int.self), radix: 16))")
            }
        }
        for c in containers.values {
            guard let t = ChromeInset.titleViews(in: c.content).first, let cls = object_getClass(t) else { continue }
            var n: UInt32 = 0
            if let methods = class_copyMethodList(cls, &n) {
                for i in 0..<Int(n) {
                    let imp = unsafeBitCast(method_getImplementation(methods[i]), to: Int.self)
                    out.append("method \(NSStringFromSelector(method_getName(methods[i]))) file=0x\(String(imp - slide, radix: 16))")
                }
                free(methods)
            }
            if let ivars = class_copyIvarList(cls, &n) {
                for i in 0..<Int(n) {
                    out.append("ivar \(ivar_getName(ivars[i]).map { String(cString: $0) } ?? "?") offset=0x\(String(ivar_getOffset(ivars[i]), radix: 16))")
                }
                free(ivars)
            }
            break
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("imps.txt"), atomically: true, encoding: .utf8)
    }

    /// Lists ivars and methods of Rex's pane title view classes, for debugging.
    private func dumpTitleClass() {
        var out: [String] = []
        var seen = Set<String>()
        func dump(_ cls: AnyClass) {
            var c: AnyClass? = cls
            while let k = c, !String(cString: class_getName(k)).hasPrefix("NS") {
                let name = String(cString: class_getName(k))
                if seen.contains(name) { break }
                seen.insert(name)
                out.append("CLASS \(name)")
                var n: UInt32 = 0
                if let ivars = class_copyIvarList(k, &n) {
                    for i in 0..<Int(n) {
                        let iv = ivars[i]
                        out.append("  ivar \(ivar_getName(iv).map { String(cString: $0) } ?? "?") : \(ivar_getTypeEncoding(iv).map { String(cString: $0) } ?? "")")
                    }
                    free(ivars)
                }
                if let methods = class_copyMethodList(k, &n) {
                    for i in 0..<Int(n) { out.append("  method \(NSStringFromSelector(method_getName(methods[i])))") }
                    free(methods)
                }
                c = class_getSuperclass(k)
            }
        }
        for c in containers.values {
            var queue: [NSView] = [c.content]
            while !queue.isEmpty {
                let v = queue.removeFirst()
                let n = String(describing: type(of: v))
                if n == "PaneTrafficLightTitleHostingView" || n == "ActionsHostingView" { dump(type(of: v)) }
                queue.append(contentsOf: v.subviews)
            }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("titleclass.txt"), atomically: true, encoding: .utf8)
    }

    /// Describes how Rex draws sidebar rows (selected, hovered), for debugging.
    private func dumpSidebar() {
        var out: [String] = []
        func layers(_ l: CALayer, _ depth: Int) {
            guard depth < 8 else { return }
            var line = String(repeating: "  ", count: depth) + "L \(type(of: l)) \(l.name ?? "") hidden=\(l.isHidden) op=\(l.opacity) cr=\(l.cornerRadius)"
            if let c = l.backgroundColor { line += " bg=\(c.components ?? [])" }
            if let sh = l as? CAShapeLayer { line += " fill=\(sh.fillColor?.components ?? []) stroke=\(sh.strokeColor?.components ?? []) lw=\(sh.lineWidth) path=\(sh.path?.boundingBox ?? .zero)" }
            if let f = l.compositingFilter { line += " comp=\(f)" }
            if l.borderWidth > 0 { line += " border=\(l.borderWidth) \(l.borderColor?.components ?? [])" }
            if l.shadowOpacity > 0 { line += " shadow=\(l.shadowOpacity) r=\(l.shadowRadius) off=\(l.shadowOffset) color=\(l.shadowColor?.components ?? [])" }
            out.append(line)
            for sl in l.sublayers ?? [] { layers(sl, depth + 1) }
        }
        func views(_ v: NSView, _ depth: Int) {
            guard depth < 7 else { return }
            var line = String(repeating: "  ", count: depth) + "V \(type(of: v)) frame=\(v.frame.integral) hidden=\(v.isHidden) alpha=\(v.alphaValue)"
            if #available(macOS 26.0, *), let g = v as? NSGlassEffectView {
                line += " GLASS(cr=\(g.cornerRadius) tint=\(g.tintColor?.description ?? "nil") style=\(g.style.rawValue))"
            }
            if let t = v as? NSTextField { line += " text=\(t.stringValue) color=\(t.textColor?.description ?? "") font=\(t.font?.description ?? "")" }
            out.append(line)
            if let l = v.layer, String(describing: type(of: v)).contains("Surface") { layers(l, depth + 1) }
            for sv in v.subviews { views(sv, depth + 1) }
        }
        for c in containers.values {
            var queue: [NSView] = [c.content]
            var rows: [NSView] = []
            var header: NSView?
            while !queue.isEmpty {
                let v = queue.removeFirst()
                let name = String(describing: type(of: v))
                if name == "TabItemView" { rows.append(v); continue }
                if name == "SessionHeaderView", header == nil { header = v }
                queue.append(contentsOf: v.subviews)
            }
            out.append("rows: \(rows.count)")
            if let first = rows.first?.superview {
                out.append("== row container (selection surfaces live here)")
                for sv in first.subviews { views(sv, 1) }
            }
            if let h = header { out.append("== session header"); views(h, 1) }
            // Hover the second row and describe it again.
            if rows.count > 1, let w = c.window {
                let row = rows[1]
                let p = row.convert(NSPoint(x: row.bounds.midX, y: row.bounds.midY), to: nil)
                if let e = NSEvent.enterExitEvent(with: .mouseEntered, location: p, modifierFlags: [], timestamp: 0,
                                                  windowNumber: w.windowNumber, context: nil, eventNumber: 0,
                                                  trackingNumber: 0, userData: nil) {
                    row.mouseEntered(with: e)
                    for t in row.trackingAreas { _ = t }
                }
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    var hov: [String] = ["== after hovering row 2"]
                    let saved = out
                    out = []
                    if let parent = row.superview { for sv in parent.subviews { views(sv, 1) } }
                    hov += out
                    out = saved + hov
                    try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("sidebar.txt"), atomically: true, encoding: .utf8)
                    if let e2 = NSEvent.enterExitEvent(with: .mouseExited, location: p, modifierFlags: [], timestamp: 0,
                                                       windowNumber: w.windowNumber, context: nil, eventNumber: 0,
                                                       trackingNumber: 0, userData: nil) { row.mouseExited(with: e2) }
                }
            }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("sidebar.txt"), atomically: true, encoding: .utf8)
    }

    /// Lists Rex views in the top-right corner of the window, for debugging.
    private func dumpHeader() {
        var out: [String] = []
        for c in containers.values {
            let b = c.bounds
            func walk(_ v: NSView, _ depth: Int) {
                let r = v.convert(v.bounds, to: c)
                if r.maxY > b.maxY - 50, r.minX > b.maxX - 420, r.width < 300, r.height < 60, !v.isHidden {
                    out.append("\(String(repeating: " ", count: depth))\(type(of: v)) rect=\(r.integral) alpha=\(v.alphaValue) class=\(NSStringFromClass(type(of: v)))")
                }
                for sv in v.subviews { walk(sv, depth + 1) }
            }
            walk(c.content, 0)
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("header.txt"), atomically: true, encoding: .utf8)
    }

    /// Writes how Rex positions its chrome (frames, constraints), for debugging.
    private func dumpChrome() {
        var out: [String] = []
        for c in containers.values {
            guard let chrome = ChromeInset.findChrome(in: c.content) else { out.append("no chrome"); continue }
            var v: NSView? = chrome
            for _ in 0..<3 {
                guard let view = v else { break }
                out.append("VIEW \(type(of: view)) frame=\(view.frame) tamic=\(view.translatesAutoresizingMaskIntoConstraints) mask=\(view.autoresizingMask.rawValue)")
                for k in view.constraints { out.append("  own: \(k)") }
                v = view.superview
            }
            if let sup = chrome.superview {
                for k in sup.constraints {
                    let a = (k.firstItem as? NSView) === chrome ? "chrome" : ((k.firstItem as? NSView) === sup ? "host" : "\(k.firstItem.map { type(of: $0) } as Any)")
                    let b = (k.secondItem as? NSView) === chrome ? "chrome" : ((k.secondItem as? NSView) === sup ? "host" : "\(k.secondItem.map { type(of: $0) } as Any)")
                    out.append("  K \(a).\(k.firstAttribute.rawValue) \(k.relation.rawValue) \(b).\(k.secondAttribute.rawValue) * \(k.multiplier) + \(k.constant) prio=\(k.priority.rawValue) active=\(k.isActive) id=\(k.identifier ?? "")")
                }
                out.append("  host frame=\(sup.frame) chrome frame=\(chrome.frame) hasAmbiguous=\(chrome.hasAmbiguousLayout)")
            }
            for sv in chrome.subviews {
                out.append("CHILD \(type(of: sv)) frame=\(sv.frame) tamic=\(sv.translatesAutoresizingMaskIntoConstraints)")
            }
            for k in chrome.constraints { out.append("  chrome-own: \(type(of: k)) \(k)") }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("chrome.txt"), atomically: true, encoding: .utf8)
    }

    /// Writes the CALayer tree under Rex's backdrop, for debugging colours.
    private func dumpLayers() {
        var out: [String] = []
        func walk(_ l: CALayer, _ depth: Int) {
            guard depth < 12 else { return }
            var line = String(repeating: "  ", count: depth) + "\(type(of: l)) \(l.name ?? "") frame=\(l.frame.integral)"
            line += " hidden=\(l.isHidden) opacity=\(l.opacity)"
            if let c = l.backgroundColor { line += " bg=\(c)" }
            if let f = l.compositingFilter { line += " compositing=\(f)" }
            if let f = l.filters, !f.isEmpty { line += " filters=\(f)" }
            if let f = l.backgroundFilters, !f.isEmpty { line += " bgFilters=\(f)" }
            if let c = l.contents { line += " contents=\(type(of: c))" }
            if l.cornerRadius > 0 { line += " cr=\(l.cornerRadius)" }
            if let d = l.delegate { line += " delegate=\(type(of: d))" }
            out.append(line)
            for sl in l.sublayers ?? [] { walk(sl, depth + 1) }
        }
        for c in containers.values {
            var queue: [NSView] = [c.content]
            while !queue.isEmpty {
                let v = queue.removeFirst()
                if String(describing: type(of: v)) == "BackdropView" {
                    out.append("== Rex BackdropView")
                    if let l = v.layer { walk(l, 0) }
                    break
                }
                queue.append(contentsOf: v.subviews)
            }
            out.append("== Mirror")
            if let l = c.backdrop.layer { walk(l, 0) }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("layers.txt"), atomically: true, encoding: .utf8)
    }

    /// Writes Rex's view tree (classes, frames, materials, colours) for debugging.
    private func dumpViews() {
        var out: [String] = []
        func color(_ c: CGColor?) -> String {
            guard let c = c, let comps = c.converted(to: CGColorSpace(name: CGColorSpace.sRGB)!, intent: .defaultIntent, options: nil)?.components else { return "-" }
            return comps.map { String(format: "%.3f", $0) }.joined(separator: ",")
        }
        func walk(_ v: NSView, _ depth: Int) {
            guard depth < 14 else { return }
            var line = String(repeating: "  ", count: depth) + String(describing: type(of: v)).prefix(80)
            line += " frame=\(v.frame.integral) opaque=\(v.isOpaque)"
            if let l = v.layer {
                line += " layerBG=\(color(l.backgroundColor)) cr=\(l.cornerRadius)"
                if let f = l.filters, !f.isEmpty { line += " filters=\(f.count)" }
            }
            if let e = v as? NSVisualEffectView {
                line += " VE(material=\(e.material.rawValue) blend=\(e.blendingMode.rawValue) state=\(e.state.rawValue) appearance=\(e.appearance?.name.rawValue ?? "inherit"))"
            }
            if let b = v as? NSBox { line += " box(fill=\(color(b.fillColor.cgColor)))" }
            out.append(line)
            for sv in v.subviews where sv.frame.width >= 60 || depth < 3 { walk(sv, depth + 1) }
        }
        for w in NSApp.windows where containers[ObjectIdentifier(w)] != nil {
            out.append("WINDOW opaque=\(w.isOpaque) bg=\(color(w.backgroundColor.cgColor)) alpha=\(w.alphaValue) "
                + "titlebarTransparent=\(w.titlebarAppearsTransparent) appearance=\(w.effectiveAppearance.name.rawValue) "
                + "style=\(w.styleMask.rawValue)")
            if let frame = w.contentView?.superview { walk(frame, 0) }
        }
        try? out.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("views.txt"), atomically: true, encoding: .utf8)
    }

    /// Writes PNGs of the panel and window plus a description, for debugging.
    private func snapshot() {
        var lines: [String] = []
        for w in NSApp.windows { lines.append(describe(w)) }
        var i = 0
        for c in containers.values {
            for (name, view) in [("panel", c.panel), ("window", c)] {
                if let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) {
                    view.cacheDisplay(in: view.bounds, to: rep)
                    let url = RM.cacheDir.appendingPathComponent("\(name)-\(i).png")
                    try? rep.representation(using: .png, properties: [:])?.write(to: url)
                    lines.append("wrote \(url.path)")
                }
            }
            i += 1
        }
        lines.append("session=\(model.sessionID ?? "-") target=\(String(describing: model.target))")
        try? lines.joined(separator: "\n").write(to: RM.cacheDir.appendingPathComponent("snapshot.txt"),
                                                  atomically: true, encoding: .utf8)
    }
}
