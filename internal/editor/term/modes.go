package term

// EditorOn enables the alternate screen, bracketed paste, focus reports,
// mouse (1002 drags, 1006 SGR), the kitty keyboard flags, then theme reports
// and a query for the current theme.
const EditorOn = "\x1b[?1049h\x1b[?2004h\x1b[?1004h\x1b[?1002h\x1b[?1006h" + KittyPush +
	ThemeReportsOn + ThemeQuery

// EditorOff undoes EditorOn in reverse, and resets colors and the cursor's
// shape and visibility, which Neovim may have changed.
const EditorOff = ThemeReportsOff + KittyPop + "\x1b[?1006l\x1b[?1002l\x1b[?1004l\x1b[?2004l\x1b[0m\x1b[0 q\x1b[?25h\x1b[?1049l"
