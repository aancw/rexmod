import AppKit
import CoreServices
import Foundation
import ObjectiveC

enum RM {
    static let home = NSHomeDirectory()
    static let defaults = UserDefaults.standard

    static var rexmodPath: String = {
        let link = home + "/.local/bin/rexmod"
        if let dest = try? FileManager.default.destinationOfSymbolicLink(atPath: link) {
            return dest.hasPrefix("/") ? dest : (link as NSString).deletingLastPathComponent + "/" + dest
        }
        if FileManager.default.isExecutableFile(atPath: link) { return link }
        return home + "/Library/Application Support/rexmod/bin/rexmod"
    }()

    static let env: [String: String] = {
        var e = ProcessInfo.processInfo.environment
        let extra = [home + "/.local/bin", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"]
        e["PATH"] = (extra + (e["PATH"].map { [$0] } ?? [])).joined(separator: ":")
        e.removeValue(forKey: "DYLD_INSERT_LIBRARIES")
        return e
    }()

    static let cacheDir: URL = {
        let u = URL(fileURLWithPath: home + "/Library/Caches/rexmod", isDirectory: true)
        try? FileManager.default.createDirectory(at: u, withIntermediateDirectories: true)
        return u
    }()

    private static let logURL = URL(fileURLWithPath: home + "/Library/Logs/rexmod-panel.log")
    private static let logQueue = DispatchQueue(label: "rexmod.log")

    static func log(_ msg: @autoclosure () -> String) {
        let line = "\(ISO8601DateFormatter().string(from: Date())) \(msg())\n"
        logQueue.async {
            guard let data = line.data(using: .utf8) else { return }
            if let h = try? FileHandle(forWritingTo: logURL) {
                h.seekToEndOfFile()
                h.write(data)
                try? h.close()
            } else {
                try? data.write(to: logURL)
            }
        }
    }

    /// The session the Rex app shows, as the app itself records it.
    static func focusedSessionID() -> String? {
        let key = "Workspace.lastFocusedSession"
        var data = defaults.data(forKey: key)
        if data == nil, let s = defaults.string(forKey: key) { data = s.data(using: .utf8) }
        guard let d = data,
              let obj = try? JSONSerialization.jsonObject(with: d) as? [String: Any],
              let id = obj["sessionID"] as? String, !id.isEmpty
        else { return nil }
        return id
    }
}

/// Runs rexmod off the main thread and reports back on the main thread.
enum Shell {
    private static let queue = DispatchQueue(label: "rexmod.shell", qos: .userInitiated, attributes: .concurrent)

    static func rexmod(_ args: [String], timeout: TimeInterval = 25,
                       completion: @escaping (_ status: Int32, _ out: Data, _ err: String) -> Void) {
        run(RM.rexmodPath, args, timeout: timeout, completion: completion)
    }

    static func run(_ path: String, _ args: [String], timeout: TimeInterval = 25,
                    completion: @escaping (_ status: Int32, _ out: Data, _ err: String) -> Void) {
        queue.async {
            let p = Process()
            p.executableURL = URL(fileURLWithPath: path)
            p.arguments = args
            p.environment = RM.env
            p.currentDirectoryURL = URL(fileURLWithPath: RM.home)
            let out = Pipe(), err = Pipe()
            p.standardOutput = out
            p.standardError = err
            p.standardInput = FileHandle.nullDevice
            var errData = Data()
            let errGroup = DispatchGroup()
            errGroup.enter()
            DispatchQueue.global().async {
                errData = err.fileHandleForReading.readDataToEndOfFile()
                errGroup.leave()
            }
            do {
                try p.run()
            } catch {
                DispatchQueue.main.async { completion(-1, Data(), "\(error)") }
                return
            }
            let killer = DispatchWorkItem { if p.isRunning { p.terminate() } }
            DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: killer)
            let data = out.fileHandleForReading.readDataToEndOfFile()
            p.waitUntilExit()
            killer.cancel()
            _ = errGroup.wait(timeout: .now() + 2)
            let errStr = String(data: errData, encoding: .utf8) ?? ""
            let status = p.terminationStatus
            DispatchQueue.main.async { completion(status, data, errStr) }
        }
    }
}

/// Watches directories with FSEvents and reports changed paths, coalesced.
final class FSWatcher {
    private var stream: FSEventStreamRef?
    private let callback: ([String]) -> Void

    init?(paths: [String], latency: CFTimeInterval = 0.35, callback: @escaping ([String]) -> Void) {
        self.callback = callback
        var ctx = FSEventStreamContext(version: 0, info: Unmanaged.passUnretained(self).toOpaque(),
                                       retain: nil, release: nil, copyDescription: nil)
        let flags = UInt32(kFSEventStreamCreateFlagUseCFTypes | kFSEventStreamCreateFlagNoDefer | kFSEventStreamCreateFlagWatchRoot)
        let cb: FSEventStreamCallback = { _, info, count, eventPaths, _, _ in
            guard let info = info else { return }
            let me = Unmanaged<FSWatcher>.fromOpaque(info).takeUnretainedValue()
            let arr = Unmanaged<CFArray>.fromOpaque(eventPaths).takeUnretainedValue() as NSArray
            var paths: [String] = []
            for i in 0..<min(count, arr.count) {
                if let s = arr[i] as? String { paths.append(s) }
            }
            me.callback(paths)
        }
        guard let s = FSEventStreamCreate(kCFAllocatorDefault, cb, &ctx, paths as CFArray,
                                          FSEventStreamEventId(kFSEventStreamEventIdSinceNow), latency, flags)
        else { return nil }
        stream = s
        FSEventStreamSetDispatchQueue(s, DispatchQueue.main)
        FSEventStreamStart(s)
    }

    deinit {
        if let s = stream {
            FSEventStreamStop(s)
            FSEventStreamInvalidate(s)
            FSEventStreamRelease(s)
        }
    }
}

/// Sparkle can't update the patched Rex (its installer only accepts an app
/// signed by Superlogical), so "Check for Updates…" would always end in
/// "Update failed". Route Sparkle's user-initiated check to rexmod's
/// update.sh instead: it installs the official build, re-patches it, and the
/// result is shown in an alert.
enum UpdateHook {
    private static var installed = false
    private static var running = false
    static var script: String { RM.home + "/Library/Application Support/rexmod/scripts/update.sh" }

    /// Only SPUUpdater.checkForUpdates: the standard controller's
    /// checkForUpdates: ends there too, and this way the hook has the updater
    /// whose delegate (Rex) is waiting to hear how the check ended.
    static func install() {
        guard !installed else { return }
        installed = true
        guard let cls = NSClassFromString("SPUUpdater"),
              let method = class_getInstanceMethod(cls, NSSelectorFromString("checkForUpdates")) else {
            RM.log("update hook: SPUUpdater.checkForUpdates not found")
            return
        }
        // checkForUpdates takes no argument; the block must take only self.
        let block: @convention(block) (AnyObject) -> Void = { updater in
            DispatchQueue.main.async { runUpdate(updater: updater) }
        }
        method_setImplementation(method, imp_implementationWithBlock(block))
        RM.log("update hook installed on SPUUpdater.checkForUpdates")
    }

    static func runUpdate(updater: AnyObject) {
        guard !running else { return }
        guard FileManager.default.isExecutableFile(atPath: script) else {
            finishCycle(updater)
            alert("rexmod can't update Rex", "\(script) is missing. Run rexmod's install.sh again.")
            return
        }
        running = true
        Shell.run("/bin/sh", [script], timeout: 600) { status, out, err in
            running = false
            finishCycle(updater)
            let text = String(data: out, encoding: .utf8) ?? ""
            if status != 0 {
                let detail = (err.isEmpty ? text : err).trimmingCharacters(in: .whitespacesAndNewlines)
                alert("Rex update failed", String(detail.suffix(600)))
            } else if text.contains("installed official Rex") {
                alert("Rex was updated", "The new version is installed and patched. Quit Rex (⌘Q) and open it again to use it.")
            } else {
                let v = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? ""
                alert("Rex is up to date", "You're on the latest Rex\(v.isEmpty ? "" : " (\(v))"). If a newer one was installed in the background, quit and reopen Rex.")
            }
        }
    }

    /// Tells Rex (Sparkle's delegate) the check is over, the way Sparkle would
    /// after finding nothing, so its "Checking for updates" status clears.
    private static func finishCycle(_ updater: AnyObject) {
        guard let delegate = (updater as? NSObject)?.value(forKey: "delegate") as? NSObject else { return }
        let noUpdate = NSError(domain: "SUSparkleErrorDomain", code: 1001, userInfo: [NSLocalizedDescriptionKey: "You're up to date!"])
        let s1 = NSSelectorFromString("updaterDidNotFindUpdate:error:")
        let s0 = NSSelectorFromString("updaterDidNotFindUpdate:")
        if delegate.responds(to: s1) {
            typealias F = @convention(c) (AnyObject, Selector, AnyObject, NSError) -> Void
            unsafeBitCast(delegate.method(for: s1), to: F.self)(delegate, s1, updater, noUpdate)
        } else if delegate.responds(to: s0) {
            typealias F = @convention(c) (AnyObject, Selector, AnyObject) -> Void
            unsafeBitCast(delegate.method(for: s0), to: F.self)(delegate, s0, updater)
        }
        let s2 = NSSelectorFromString("updater:didFinishUpdateCycleForUpdateCheck:error:")
        if delegate.responds(to: s2) {
            typealias F = @convention(c) (AnyObject, Selector, AnyObject, Int, NSError?) -> Void
            unsafeBitCast(delegate.method(for: s2), to: F.self)(delegate, s2, updater, 0, nil)
        }
        RM.log("update hook: told the delegate the check finished")
    }

    private static func alert(_ title: String, _ info: String) {
        let a = NSAlert()
        a.messageText = title
        a.informativeText = info
        a.runModal()
    }
}

/// Moves the panel's edge the way Rex moves its sidebar's. Measured from Rex
/// 1742 at 60 fps, opening and closing alike: a spring with a 0.35 s response
/// and 0.83 damping, half way in about 85 ms, 99% in about 230 ms, then a 1%
/// overshoot that settles by 0.45 s. AppKit animations only take Bézier
/// curves, so this sets the value on every display frame.
final class EdgeSpring: NSObject {
    static let response = 0.35
    static let damping = 0.83
    /// Long past settling: the envelope is below 0.02% by then.
    static let duration = 0.6

    /// How far along the spring is, t seconds in: 0 at rest, 1 at its target.
    static func progress(_ t: Double) -> Double {
        guard t > 0 else { return 0 }
        let w0 = 2 * Double.pi / response
        let wd = w0 * (1 - damping * damping).squareRoot()
        return 1 - exp(-damping * w0 * t) * (cos(wd * t) + damping * w0 / wd * sin(wd * t))
    }

    private(set) var target: CGFloat = 0
    private var from: CGFloat = 0
    private var start: CFTimeInterval = 0
    private var link: CADisplayLink?
    private var step: ((CGFloat) -> Void)?
    private var done: (() -> Void)?
    var isRunning: Bool { link != nil }

    /// Starts from `from`, usually where a running spring has got to, so a
    /// second toggle mid-way turns around instead of jumping.
    func run(on view: NSView, from: CGFloat, to: CGFloat, step: @escaping (CGFloat) -> Void, done: @escaping () -> Void) {
        stop()
        self.from = from
        target = to
        start = CACurrentMediaTime()
        self.step = step
        self.done = done
        let l = view.displayLink(target: self, selector: #selector(tick(_:)))
        l.add(to: .main, forMode: .common)
        link = l
    }

    @objc private func tick(_ l: CADisplayLink) {
        let t = l.targetTimestamp - start
        if t >= Self.duration {
            step?(target)
            let d = done
            stop()
            d?()
            return
        }
        step?(from + (target - from) * CGFloat(Self.progress(t)))
    }

    func stop() {
        link?.invalidate()
        link = nil
        step = nil
        done = nil
    }
}
