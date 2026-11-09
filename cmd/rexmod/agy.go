package main

import (
	"crypto/sha256"
	"encoding/hex"
)

// Antigravity CLI (agy) runs `rexmod hook prompt|stop --agent agy` from the
// rexmod plugin (~/.gemini/config/plugins/rexmod). Its hooks run in the agy
// process, so REX_BLOCK names its pane, but in the plugin's folder, and the
// payload is camelCase. agy has no UserPromptSubmit: PreInvocation fires
// before every model call and stands in for it.

// agyHook is the part of an agy hook payload rexmod reads.
type agyHook struct {
	ConversationID string   `json:"conversationId"`
	WorkspacePaths []string `json:"workspacePaths"`
	TranscriptPath string   `json:"transcriptPath"`
	LastUserInput  string   `json:"lastUserInput"`
	InvocationNum  int      `json:"invocationNum"` // PreInvocation: 0 on the first model call of a run
	FullyIdle      bool     `json:"fullyIdle"`     // Stop: no subagent or background task still running
}

func (h agyHook) input() hookInput {
	in := hookInput{SessionID: h.ConversationID, TranscriptPath: h.TranscriptPath}
	if len(h.WorkspacePaths) > 0 {
		in.Cwd = h.WorkspacePaths[0]
	}
	return in
}

// marksTurn tells whether this hook run starts (prompt) or ends (stop) a
// turn of the user's; cur is the conversation's current turn, if any.
//
// PreInvocation also fires for every later model call of a run
// (invocationNum > 0) and for subagents, which run in the same process as
// their own conversation with no user input. A run also starts again, at
// invocationNum 0 with the same input, when a subagent it waits for
// finishes: that continues the turn. Stop fires for that wait too, while
// the subagent still runs (not fully idle); the turn ends at the last one.
func (h agyHook) marksTurn(event string, cur *Turn) bool {
	switch event {
	case "prompt":
		if h.InvocationNum > 0 || h.LastUserInput == "" {
			return false
		}
		resumed := cur != nil && cur.End == "" && cur.Prompt == promptKey(h.LastUserInput)
		return !resumed
	case "stop":
		return h.FullyIdle
	}
	return false
}

// promptKey identifies the user message that started a turn without
// keeping its text.
func promptKey(input string) string {
	if input == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:8])
}

// agyReply is the JSON agy reads back from the hook, on every run. Only
// the plain systemMessage: agy 1.5 panics on the documented
// metadata.user_markdown and drops the whole step.
func agyReply(notes string) map[string]any {
	if notes == "" {
		return map[string]any{}
	}
	return map[string]any{
		"injectSteps": []any{
			map[string]any{"systemMessage": map[string]any{"systemMessage": notes}},
		},
	}
}
