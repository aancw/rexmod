import AppKit
import Combine

/// The colors Rex draws its chrome with, for one theme.
struct RexThemeColors {
    let content: NSColor
    let contentSecondary: NSColor
    let surfaceRaised: NSColor
    let controlHover: NSColor

    /// themes.json has only the built-in themes; a custom theme gets neutral
    /// colors for its appearance. Not system colors: those resolve against the
    /// app's appearance, and Rex sets the appearance on its windows.
    static func fallback(dark: Bool) -> RexThemeColors {
        let ink: NSColor = dark ? .white : .black
        return RexThemeColors(content: ink.withAlphaComponent(0.85), contentSecondary: ink.withAlphaComponent(0.55),
                              surfaceRaised: NSColor(white: dark ? 0.15 : 0.98, alpha: 1),
                              controlHover: ink.withAlphaComponent(0.05))
    }
}

/// Reads the active theme the way Rex does: its settings name a theme for each
/// appearance, the design tokens in Rex's bundle hold that theme's colors.
enum RexTheme {
    private struct Token: Decodable {
        let red, green, blue, alpha: CGFloat
        var color: NSColor { NSColor(displayP3Red: red, green: green, blue: blue, alpha: alpha) }
    }

    private struct Semantic: Decodable {
        let content, contentSecondary, surfaceRaised, controlHover: Token
    }

    private struct Theme: Decodable {
        let id: String
        let semantic: Semantic
    }

    private static let themes: [String: RexThemeColors] = {
        let path = "SuperUI_SuperDesignTokens.bundle/Contents/Resources/themes.json"
        guard let url = Bundle.main.resourceURL?.appendingPathComponent(path),
              let data = try? Data(contentsOf: url),
              let list = try? JSONDecoder().decode([Theme].self, from: data)
        else {
            RM.log("theme: cannot read \(path)")
            return [:]
        }
        var out: [String: RexThemeColors] = [:]
        for t in list {
            let s = t.semantic
            out[t.id] = RexThemeColors(content: s.content.color, contentSecondary: s.contentSecondary.color,
                                       surfaceRaised: s.surfaceRaised.color, controlHover: s.controlHover.color)
        }
        return out
    }()

    static func colors(for view: NSView) -> RexThemeColors {
        colors(dark: view.effectiveAppearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua)
    }

    static func colors(dark: Bool) -> RexThemeColors {
        let id = RM.defaults.string(forKey: dark ? "Workspace.darkThemeID" : "Workspace.lightThemeID")
            ?? (dark ? "merino-dark" : "merino-light")
        return themes[id] ?? .fallback(dark: dark)
    }
}

/// Rex's Interface settings that change how its surfaces are drawn: Classic
/// or Glass, each tinted with the theme or not. Rex stores the pair as one
/// value: useTheme (Classic, tinted), system (Classic), blendTheme (Glass,
/// tinted), modern (Glass). Rex writes it in this process, so a change in its
/// Settings shows up at once.
final class RexLook: ObservableObject {
    static let shared = RexLook()
    @Published private(set) var glass = false
    @Published private(set) var tinted = true
    /// Changes with the selected themes, so views using theme colors redraw.
    @Published private(set) var themes = ""

    private init() {
        read()
        NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: nil, queue: .main) { [weak self] _ in
            self?.read()
        }
    }

    private func read() {
        let style = RM.defaults.string(forKey: "Workspace.interfaceStyle") ?? "useTheme"
        let g: Bool
        if #available(macOS 26.0, *) { g = ["blendTheme", "modern", "glass"].contains(style) } else { g = false }
        let t = !["system", "modern"].contains(style)
        let th = (RM.defaults.string(forKey: "Workspace.lightThemeID") ?? "") + "|" + (RM.defaults.string(forKey: "Workspace.darkThemeID") ?? "")
        if g != glass { glass = g }
        if t != tinted { tinted = t }
        if th != themes { themes = th }
    }
}
