// Runs the panel's views without Rex, on a fixed state, behind your other
// windows: `make preview` screenshots each tab into build/preview and presses
// its controls through the accessibility API. Actions reach a stand-in for the
// rexmod CLI that only records them, so nothing goes to a real agent.
import AppKit
import ApplicationServices
import SwiftUI

let outDir = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first { !$0.hasPrefix("--") } ?? "build/preview", isDirectory: true)
try? FileManager.default.createDirectory(at: outDir, withIntermediateDirectories: true)

// MARK: - Stand-in CLI

let callLog = outDir.appendingPathComponent("calls.log")
let fakeCLI = outDir.appendingPathComponent("fake-rexmod")
let fileList = ["README.md", "cmd/rexmod/git.go", "cmd/rexmod/main.go", "panel/Model.swift", "panel/Views.swift", "scripts/patch.sh"]
try? """
#!/bin/sh
printf '%s\\n' "$*" >> '\(callLog.path)'
[ "$1" = files ] && printf '%s\\n' \(fileList.joined(separator: " "))
exit 0

""".write(to: fakeCLI, atomically: true, encoding: .utf8)
try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: fakeCLI.path)
RM.rexmodPath = fakeCLI.path

func calls() -> String { (try? String(contentsOf: callLog, encoding: .utf8)) ?? "" }

// MARK: - Fixture

func file(_ path: String, _ status: String, _ add: Int, _ del: Int, staged: Bool? = nil, old: String? = nil) -> FileChangeDTO {
    FileChangeDTO(path: path, oldPath: old, status: status, add: add, del: del, binary: nil, staged: staged)
}

func changes(_ mode: String, _ files: [FileChangeDTO], base: String? = "abc", ref: String? = nil, running: Bool? = nil) -> ChangesDTO {
    ChangesDTO(mode: mode, base: base, baseRef: ref, running: running, since: nil, note: nil, files: files,
               add: files.map(\.add).reduce(0, +), del: files.map(\.del).reduce(0, +))
}

let target = TargetDTO(
    sessionId: "session:preview", sessionLabel: "preview", windowId: "window:preview", focusedBlockId: "block:claude",
    agentBlockId: "block:claude", agent: "Claude",
    agents: [AgentPaneDTO(blockId: "block:claude", agent: "claude", name: "Claude"),
             AgentPaneDTO(blockId: "block:codex", agent: "codex", name: "Codex")],
    editorBlockId: nil, cwd: NSHomeDirectory() + "/rexmod/panel", repo: NSHomeDirectory() + "/rexmod", repoName: "rexmod",
    branch: "feature/details-panel", gitDir: NSHomeDirectory() + "/rexmod/.git")

let uncommitted = [
    file("panel/Views.swift", "M", 412, 138, staged: true),
    file("cmd/rexmod/git_test.go", "A", 92, 0, staged: true),
    file("panel/Model.swift", "M", 38, 2),
    file("README.md", "M", 6, 8),
    file("docs/old-guide.md", "D", 0, 40),
    file("panel/preview/main.swift", "A", 180, 0),
    file("scripts/new-name.sh", "R", 2, 2, old: "scripts/old-name.sh"),
]

let comments = [
    CommentDTO(id: "1", file: "panel/Views.swift", line: 120, endLine: 126, text: "This tab row should stay on one line at the minimum width."),
    CommentDTO(id: "2", file: "panel/Model.swift", line: 44, endLine: 44, text: "Why is this optional?\nThe CLI always sends it."),
]

let fullState = StateDTO(
    target: target,
    changes: ["uncommitted": changes("uncommitted", uncommitted, ref: "HEAD"),
              "last-turn": changes("last-turn", Array(uncommitted[2...3]), running: true),
              "branch": changes("branch", uncommitted.map { var f = $0; f.staged = nil; return f }, ref: "origin/main")],
    pendingComments: comments.count, comments: comments, error: nil)

let fullInfo = InfoDTO(
    agent: .init(name: "Claude", blockId: "block:claude", status: "working", message: "Running the panel checks",
                 sessionId: "2e121b34-ca64-4616-b3e8-7bf4a25147d8", resume: "claude --resume 2e121b34-ca64-4616-b3e8-7bf4a25147d8"),
    usage: .init(source: "Claude", fetchedAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(-90)),
                 windows: [.init(label: "5h", used: 62, resetsAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(9600)).replacingOccurrences(of: "Z", with: ".462836+00:00")),
                           .init(label: "7d", used: 87, resetsAt: ISO8601DateFormatter().string(from: Date().addingTimeInterval(530000)))]),
    processes: [.init(pid: 1730, name: "claude", seconds: 3495), .init(pid: 20486, name: "Python", seconds: 1252)],
    ports: [.init(port: 8765, pid: 20486, name: "Python", pane: "block:shell")])

let emptyState = StateDTO(
    target: target,
    changes: ["uncommitted": changes("uncommitted", [], ref: "HEAD"),
              "last-turn": changes("last-turn", [], base: nil),
              "branch": changes("branch", [], ref: "origin/main")],
    pendingComments: 0, comments: [], error: nil)

let noRepoStateDTO = StateDTO(target: TargetDTO(sessionId: "s", windowId: "w", cwd: "/tmp", repo: nil), changes: nil, pendingComments: 0, error: nil)

// MARK: - Harness

let app = NSApplication.shared
DispatchQueue.global(qos: .utility).async { _ = FlowIcons.shared } // as the panel does at start
app.setActivationPolicy(.accessory)
let model = PanelModel()
model.isVisible = { false }
// With a session, a refresh asks the stand-in CLI, whose empty answer keeps the
// state; without one it would blank the panel.
model.setSession("session:preview")
model.state = fullState
model.info = fullInfo
model.files = fileList

let window = NSWindow(contentRect: NSRect(x: 40, y: 40, width: 320, height: 720), styleMask: [.borderless], backing: .buffered, defer: false)
window.isReleasedWhenClosed = false
let backdrop = NSVisualEffectView()
backdrop.material = .sidebar
backdrop.blendingMode = .behindWindow
backdrop.state = .active
window.contentView = backdrop
let hosting = NSHostingView(rootView: PanelView(model: model, topInset: 0, onHide: {}))
hosting.frame = backdrop.bounds
hosting.autoresizingMask = [.width, .height]
backdrop.addSubview(hosting)
window.orderBack(nil) // behind whatever you're working in

var failures: [String] = []
func check(_ ok: Bool, _ what: String) {
    print(ok ? "ok    \(what)" : "FAIL  \(what)")
    if !ok { failures.append(what) }
}

func onMain<T>(_ f: @escaping () -> T) -> T { DispatchQueue.main.sync(execute: f) }
func settle(_ t: TimeInterval = 0.45) { Thread.sleep(forTimeInterval: t) }

func shot(_ name: String) {
    settle()
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
    p.arguments = ["-x", "-o", "-l", String(onMain { window.windowNumber }), outDir.appendingPathComponent(name + ".png").path]
    try? p.run()
    p.waitUntilExit()
}

// MARK: - Accessibility driver

let axApp = AXUIElementCreateApplication(ProcessInfo.processInfo.processIdentifier)
func attr(_ e: AXUIElement, _ n: String) -> CFTypeRef? {
    var v: CFTypeRef?
    AXUIElementCopyAttributeValue(e, n as CFString, &v)
    return v
}
func labels(_ e: AXUIElement) -> [String] {
    [kAXTitleAttribute, kAXDescriptionAttribute, kAXValueAttribute].compactMap { attr(e, $0) as? String }
}
func find(_ e: AXUIElement, _ label: String) -> AXUIElement? {
    if labels(e).contains(label) {
        var names: CFArray?
        AXUIElementCopyActionNames(e, &names)
        if let n = names as? [String], n.contains(kAXPressAction) { return e }
    }
    for k in (attr(e, kAXChildrenAttribute) as? [AXUIElement]) ?? [] {
        if let f = find(k, label) { return f }
    }
    return nil
}
// Within one process these calls run on the caller's thread: SwiftUI needs main.
func press(_ label: String) -> Bool {
    settle(0.25)
    return onMain {
        guard let wins = attr(axApp, kAXWindowsAttribute) as? [AXUIElement] else { return false }
        for w in wins {
            if let b = find(w, label) { return AXUIElementPerformAction(b, kAXPressAction as CFString) == .success }
        }
        return false
    }
}

/// The controls at both ends of the tab row are inside the window: when the
/// row outgrows the panel, SwiftUI centres it and clips both.
func checkInsideWindow(_ tab: String) {
    guard AXIsProcessTrusted() else { return }
    settle()
    let inside = onMain { () -> Bool in
        guard let w = (attr(axApp, kAXWindowsAttribute) as? [AXUIElement])?.first else { return false }
        func xSpan(_ label: String) -> (CGFloat, CGFloat)? {
            guard let e = find(w, label), let v = attr(e, kAXPositionAttribute), let z = attr(e, kAXSizeAttribute) else { return nil }
            var p = CGPoint.zero, size = CGSize.zero
            AXValueGetValue(v as! AXValue, .cgPoint, &p)
            AXValueGetValue(z as! AXValue, .cgSize, &size)
            return (p.x, p.x + size.width)
        }
        let f = window.frame
        guard let first = xSpan(panelTabs[0].title), let last = xSpan("Hide Changes Panel") else { return false }
        return first.0 >= f.minX && last.1 <= f.maxX
    }
    check(inside, "\(tab) fits the narrowest panel")
}

// MARK: - Run

func set(_ f: @escaping () -> Void) { onMain(f) }

func appearance(_ name: NSAppearance.Name) {
    set { window.appearance = NSAppearance(named: name) }
}

func screenshots(suffix: String) {
    set { model.state = fullState; model.tab = .info }
    shot("info\(suffix)")
    set { model.tab = .changes; model.mode = "uncommitted"; model.changesAsTree = false }
    shot("changes-uncommitted\(suffix)")
    set { model.mode = "last-turn" }
    shot("changes-last-turn\(suffix)")
    set { model.mode = "branch" }
    shot("changes-branch\(suffix)")
    set { model.mode = "uncommitted"; model.changesAsTree = true }
    shot("changes-tree\(suffix)")
    set { model.changesAsTree = false; model.tab = .files; model.expandedFiles = ["panel", "cmd"] }
    shot("files\(suffix)")
    set { model.filter = "model" }
    shot("files-filtered\(suffix)")
    set { model.filter = "" }
}

func logicChecks() {
    let g = ChangeGroup.groups(for: changes("uncommitted", uncommitted, ref: "HEAD"))
    check(g.map(\.title) == ["Staged", "Unstaged"], "uncommitted splits into Staged and Unstaged")
    check(g.map { $0.files.count } == [2, 5], "every file lands in exactly one group")
    check(ChangeGroup.groups(for: changes("uncommitted", [file("a", "M", 1, 0)], ref: "HEAD")).map(\.title) == ["Unstaged"], "an empty group is left out")
    check(ChangeGroup.groups(for: changes("branch", uncommitted, ref: "origin/main")).map(\.title) == ["Changed"], "branch ignores the index")
    check(ChangeGroup.groups(for: changes("last-turn", [])).isEmpty, "no files, no groups")
    // The panel's spring against Rex's sidebar, measured frame by frame
    // (time after the start, fraction of the way): it must stay within 4%.
    let rex: [(Double, Double)] = [(0.018, 0.05), (0.053, 0.27), (0.087, 0.51), (0.104, 0.62), (0.132, 0.79),
                                   (0.182, 0.93), (0.232, 0.986), (0.265, 0.995), (0.298, 1.009), (0.381, 1.005)]
    let worst = rex.map { abs(EdgeSpring.progress($0.0) - $0.1) }.max() ?? 1
    check(worst < 0.04, String(format: "panel spring follows Rex's sidebar (worst %.1f%%)", worst * 100))
    let peak = stride(from: 0.0, through: EdgeSpring.duration, by: 0.005).map(EdgeSpring.progress).max() ?? 0
    check(peak > 1.0 && peak < 1.02, String(format: "it overshoots like Rex's, by %.1f%%", (peak - 1) * 100))
    check(abs(EdgeSpring.progress(EdgeSpring.duration) - 1) < 0.001, "it has settled when the animation stops")
}

func interactionChecks() {
    guard AXIsProcessTrusted() else {
        print("skip  interaction checks: this process has no Accessibility permission")
        return
    }
    try? FileManager.default.removeItem(at: callLog)
    set { model.state = fullState; model.tab = .info; model.mode = "uncommitted"; model.changesAsTree = false }

    check(press("Changes, 2 pending") && onMain { model.tab } == .changes, "Changes tab opens and shows the pending comment count")
    check(press("Files") && onMain { model.tab } == .files, "Files tab opens")
    check(press("Info") && onMain { model.tab } == .info, "Info tab opens")
    set { model.tab = .changes }
    check(onMain { panelTabs.map(\.title) } == ["Info", "Changes", "Files"], "three tabs: Info, Changes, Files")

    check(press("Model.swift, :44, Why is this optional?"), "a comment is pressable")
    settle()
    check(calls().contains("open --session session:preview --window window:preview --line 44 " + NSHomeDirectory() + "/rexmod/panel/Model.swift"),
          "pressing it opens the editor at its line")

    check(press("Send 2 to Claude"), "Send button is pressable")
    settle()
    check(calls().contains("comments send --session session:preview --window window:preview"), "Send runs `comments send` for this tab")

    set { model.tab = .changes }
    check(press("panel/Model.swift"), "a changed file is pressable")
    settle()
    check(calls().contains("open --session session:preview --window window:preview " + NSHomeDirectory() + "/rexmod/panel/Model.swift"),
          "pressing it opens that file in the editor")
    check(onMain { model.selected } == "panel/Model.swift", "the opened file is selected")

    let before = calls()
    check(press("docs/old-guide.md"), "a deleted file is listed")
    settle()
    check(calls() == before, "pressing a deleted file does nothing")

    check(press("Show as Tree") && onMain { model.changesAsTree }, "switches to the tree")
    check(press("panel folder") , "the tree shows folders")
    check(press("Show as List") && !onMain { model.changesAsTree }, "switches back to the list")

    set { model.tab = .info }
    // The copy checks borrow the clipboard; what was on it goes back after.
    let savedClipboard = NSPasteboard.general.string(forType: .string)
    defer {
        NSPasteboard.general.clearContents()
        if let s = savedClipboard { NSPasteboard.general.setString(s, forType: .string) }
    }
    NSPasteboard.general.clearContents()
    check(press("Copy Session ID"), "Copy Session ID is on the Info tab")
    check(NSPasteboard.general.string(forType: .string) == fullInfo.agent?.sessionId, "it copies the session ID")
    check(press("Copy Resume Command") && NSPasteboard.general.string(forType: .string) == fullInfo.agent?.resume,
          "Copy Resume Command copies `claude --resume <id>`")
    check(onMain { find(attr(axApp, kAXWindowsAttribute).flatMap { ($0 as? [AXUIElement])?.first } ?? axApp, "localhost:8765, Python") != nil
                   || find(axApp, "localhost:8765") != nil }, "the port is listed")
    check(Elapsed.text(3495) == "58m 15s" && Elapsed.text(9600, coarse: true) == "2h 40m" && Elapsed.text(30, coarse: true) == "<1m",
          "durations read the way the tab writes them")
    check(Elapsed.date("2026-11-02T20:30:00.462836+00:00") != nil && Elapsed.date("2026-11-02T15:56:01Z") != nil,
          "reset times with microseconds parse")
    check(press("Codex"), "the other agent is listed")
    settle()
    check(calls().contains("follow --session session:preview --block block:codex"), "pressing it follows that agent")
    check(press("2 pending comments") && onMain { model.tab } == .changes, "the review line leads to the comments on Changes")

    set { model.tab = .files }
    check(press("Find File"), "Find File is on the Files tab")
    settle()
    check(calls().contains("pick --session session:preview"), "Find File runs the picker")
}

// Return must keep going to the terminal next to the panel.
func returnKeyCheck() {
    set { model.state = fullState; model.tab = .changes }
    settle()
    let before = calls()
    let handled = onMain { () -> Bool in
        guard let e = NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
                                       windowNumber: window.windowNumber, context: nil, characters: "\r",
                                       charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36) else { return true }
        return hosting.performKeyEquivalent(with: e)
    }
    settle()
    check(!handled && calls() == before, "Return beside the pending comments doesn't send")
}

func stateChecks() {
    set { model.state = emptyState; model.mode = "uncommitted"; model.tab = .changes }
    shot("empty-changes")
    set { model.state = noRepoStateDTO; model.tab = .info }
    shot("no-repo-info")
    set { model.tab = .changes }
    shot("no-repo-changes")
}

/// `--animate`: plays each animation once, slowly enough to record, and prints
/// the window number first so a recorder can follow it. Prints the time of
/// each step relative to the first.
/// Counts display frames the main thread missed (gaps over 1.5 frames).
final class HitchMeter: NSObject {
    var last: CFTimeInterval = 0
    var step = "start"
    var hitches: [String] = []
    @objc func tick(_ l: CADisplayLink) {
        let now = l.timestamp
        if last > 0, now - last > l.duration * 1.5 {
            hitches.append(String(format: "%@: %.0f ms", step, (now - last) * 1000))
        }
        last = now
    }
}
let hitchMeter = HitchMeter()

func animationTour() {
    print("window \(onMain { window.windowNumber })"); fflush(stdout)
    set {
        let l = hosting.displayLink(target: hitchMeter, selector: #selector(HitchMeter.tick(_:)))
        l.add(to: .main, forMode: .common)
    }
    set { model.state = fullState; model.tab = .info; model.mode = "uncommitted"; model.changesAsTree = false; model.collapsed = [] }
    settle(1.5)
    let t0 = Date()
    func step(_ name: String, _ f: @escaping () -> Void) {
        set { hitchMeter.step = name; f() }
        print(String(format: "%.3f %@", Date().timeIntervalSince(t0), name)); fflush(stdout)
        settle(0.9)
    }
    step("tab changes") { model.tab = .changes }
    step("tab files") { model.tab = .files }
    step("tab info") { model.tab = .info }
    step("tab changes") { model.tab = .changes }
    step("mode branch") { model.mode = "branch" }
    step("mode uncommitted") { model.mode = "uncommitted" }
    step("tree") { model.changesAsTree = true }
    step("collapse panel") { model.toggleFolder("panel") }
    step("expand panel") { model.toggleFolder("panel") }
    step("file arrives") {
        var st = fullState
        st.changes?["uncommitted"]?.files.append(file("panel/Theme.swift", "M", 4, 1))
        st.changes?["uncommitted"]?.add += 4
        st.changes?["uncommitted"]?.del += 1
        withAnimation(Motion.fade) { model.state = st }
    }
    step("list") { model.changesAsTree = false }
    step("comments sent") { withAnimation(Motion.fade) { model.state = emptyState } }
    step("flash") { model.show("sent 2 comment(s) to Claude") }
    settle(4.5)
    onMain { print("hitches:", hitchMeter.hitches.isEmpty ? "none" : hitchMeter.hitches.joined(separator: ", ")) }
    exit(0)
}

DispatchQueue.global().async {
    if CommandLine.arguments.contains("--animate") { animationTour() }
    settle(0.8)
    logicChecks()
    appearance(.darkAqua)
    screenshots(suffix: "-dark")
    appearance(.aqua)
    screenshots(suffix: "-light")
    stateChecks()
    set { window.setFrame(NSRect(x: 40, y: 40, width: 220, height: 720), display: true); model.state = fullState; model.tab = .changes; model.mode = "uncommitted" }
    shot("narrow-changes")
    checkInsideWindow("Changes")
    set { model.tab = .files }
    shot("narrow-files")
    checkInsideWindow("Files")
    set { model.tab = .info }
    shot("narrow-info")
    checkInsideWindow("Info")
    set { window.setFrame(NSRect(x: 40, y: 40, width: 320, height: 720), display: true) }
    interactionChecks()
    returnKeyCheck()
    print(failures.isEmpty ? "\nall checks passed; screenshots in \(outDir.path)" : "\n\(failures.count) failed")
    exit(failures.isEmpty ? 0 : 1)
}
app.run()
