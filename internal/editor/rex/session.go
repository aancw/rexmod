package rex

import (
	"log"
	"sync"
)

// Our side of the Rex session: requests in flight, the session view, and
// which block Rex has focused. Methods lock a mutex, so Read may run in its
// own goroutine while another calls Next and the request methods.

// RequestKind is what a request asked for; names match Rex's methods.
type RequestKind int

const (
	ReqAttach RequestKind = iota
	ReqNewSplit
	ReqNewLayer
	ReqFocusBlock
	ReqView
	ReqDisownBlock
	ReqCloseBlock
	ReqCloseLayer
)

var reqNames = [...]string{"attach", "new_split", "new_layer", "focus_block", "view", "disown_block", "close_block", "close_layer"}

func (k RequestKind) String() string { return reqNames[k] }

// Request is a request kind and, for new_split and new_layer, its pane.
type Request struct {
	Kind RequestKind
	Pane uint32
}

type pending struct {
	id  uint64
	req Request
}

// ConnState: nothing is sent once closed; windows keep working without Rex.
type ConnState int

const (
	Attaching ConnState = iota
	Attached
	Closed
)

type fetchState int

const (
	fetchIdle fetchState = iota
	fetchRunning
	fetchStale
)

type focusState int

const (
	focusUnknown focusState = iota
	focusSettled
	focusRequesting
)

// Update is what the daemon has to act on: one of SplitMade, SplitFailed,
// LayerMade, LayerFailed, Layout, FocusMoved.
type Update interface{ isUpdate() }

// SplitMade: the split asked for Pane exists, as Block.
type SplitMade struct {
	Pane  uint32
	Block string
}

// SplitFailed: Rex refused the split asked for this pane.
type SplitFailed struct{ Pane uint32 }

// LayerMade: the floating layer asked for Pane exists, as Layer holding Block.
// Layer is empty when Rex didn't say.
type LayerMade struct {
	Pane  uint32
	Block string
	Layer string
}

// LayerFailed: Rex refused the floating layer asked for this pane.
type LayerFailed struct{ Pane uint32 }

// Layout is a newer view's windows, to read where our panes sit.
type Layout struct{ Windows []any }

// FocusMoved: the user focused this block in Rex.
type FocusMoved struct{ Block string }

func (SplitMade) isUpdate()   {}
func (SplitFailed) isUpdate() {}
func (LayerMade) isUpdate()   {}
func (LayerFailed) isUpdate() {}
func (Layout) isUpdate()      {}
func (FocusMoved) isUpdate()  {}

// Requester sends requests; *Client is one.
type Requester interface {
	Request(method string, payload any) (uint64, error)
}

type Session struct {
	mu        sync.Mutex
	client    Requester
	conn      *Client // for Read/Next; nil in tests that drive OnResponse
	SessionID string

	pending []pending
	state   ConnState

	viewRev   int64
	viewFetch fetchState

	focus         focusState
	focusBlock    string
	focusInFlight uint32
	focusApplied  int64

	// Window is the Rex window our panes are in, as last found in a view.
	window string

	// A focus change from the same view as a Layout update, sent after it.
	queuedFocus *string

	// Logf logs; nil means log.Printf.
	Logf func(format string, args ...any)
}

// NewSession works on sessionID over c.
func NewSession(c *Client, sessionID string) *Session {
	return &Session{client: c, conn: c, SessionID: sessionID, viewRev: -1}
}

// NewSessionWith uses any Requester; Read and Next then do nothing.
func NewSessionWith(r Requester, sessionID string) *Session {
	return &Session{client: r, SessionID: sessionID, viewRev: -1}
}

func (s *Session) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// State is the connection state.
func (s *Session) State() ConnState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Window is the Rex window our panes are in, or "".
func (s *Session) Window() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.window
}

// SetWindow records the Rex window holding our panes (43-byte id).
func (s *Session) SetWindow(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.window = id
}

// Attach joins our Rex session, so we hear its events and can split it.
func (s *Session) Attach() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(Request{Kind: ReqAttach}, "session.attach", map[string]any{"session_id": s.SessionID})
}

// Read does one blocking read from Rex; false once it hung up. Then call
// Next until nil. Safe to call from its own goroutine.
func (s *Session) Read() (bool, error) {
	if s.conn == nil {
		return false, nil
	}
	ok, err := s.conn.Read()
	if !ok {
		s.mu.Lock()
		if err == nil {
			s.logf("rexmod: Rex hung up")
		}
		s.state = Closed
		s.mu.Unlock()
	}
	return ok, err
}

// Next is the next update for the daemon, or nil when there are none.
func (s *Session) Next() (Update, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queuedFocus != nil {
		b := *s.queuedFocus
		s.queuedFocus = nil
		return FocusMoved{Block: b}, nil
	}
	if s.conn != nil {
		for {
			m, err := s.conn.Next()
			if err != nil {
				return nil, err
			}
			if m == nil {
				break
			}
			switch m.Kind {
			case KindResponse:
				u, err := s.onResponse(m.Response)
				if err != nil || u != nil {
					return u, err
				}
			case KindEvent:
				if err := s.onEvent(m.Event); err != nil {
					return nil, err
				}
			case KindClosed:
				s.onClosed(m.Closed)
			}
		}
	}
	if s.state == Closed {
		return s.failPendingSplit(), nil
	}
	return nil, nil
}

// Focus asks Rex to focus block, unless it already has or will. "" is a no-op.
func (s *Session) Focus(block string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == Closed || block == "" {
		return nil
	}
	if s.hasFocused(block) {
		return nil
	}
	if err := s.request(Request{Kind: ReqFocusBlock}, "session.focus_block", map[string]any{
		"session_id": s.SessionID, "block_id": block,
	}); err != nil {
		return err
	}
	if s.focus == focusRequesting {
		s.focusBlock = block
		s.focusInFlight++
	} else {
		s.focus, s.focusBlock, s.focusInFlight, s.focusApplied = focusRequesting, block, 1, 0
	}
	return nil
}

// HasFocused: Rex has focused block, or will once our request lands.
func (s *Session) HasFocused(block string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasFocused(block)
}

func (s *Session) hasFocused(block string) bool {
	if s.focus == focusUnknown || block == "" {
		return false
	}
	return SameBlock(s.focusBlock, block)
}

// Disown: the creator owns a block and sizes it; this lets the app size it instead.
func (s *Session) Disown(block string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(Request{Kind: ReqDisownBlock}, "session.disown_block", map[string]any{"session_id": s.SessionID, "block_ids": []string{block}})
}

// CloseBlock closes a block we made.
func (s *Session) CloseBlock(block string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(Request{Kind: ReqCloseBlock}, "block.close", map[string]any{"session_id": s.SessionID, "block_id": block})
}

// CloseLayer closes a floating layer we made.
func (s *Session) CloseLayer(layer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(Request{Kind: ReqCloseLayer}, "session.close_layer", map[string]any{"session_id": s.SessionID, "layer_id": layer})
}

// SplitSpec is session.new_split's payload beyond session_id.
type SplitSpec struct {
	AnchorID  string `json:"anchor_id"`
	Direction string `json:"direction"` // "horizontal" or "vertical"
	Side      string `json:"side"`      // "before" or "after"
	Layout    any    `json:"layout"`
}

// NewSplit asks for a split beside spec.AnchorID for pane; the answer comes
// as SplitMade or SplitFailed.
func (s *Session) NewSplit(pane uint32, spec SplitSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(Request{Kind: ReqNewSplit, Pane: pane}, "session.new_split", map[string]any{
		"session_id": s.SessionID, "anchor_id": spec.AnchorID, "direction": spec.Direction,
		"side": spec.Side, "layout": spec.Layout,
	})
}

// NewLayer asks for a floating layer at bounds in our window, holding layout.
func (s *Session) NewLayer(pane uint32, bounds [4]float64, layout any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := map[string]any{"session_id": s.SessionID, "bounds": bounds, "layout": layout}
	if s.window != "" {
		p["window_id"] = s.window
	}
	return s.request(Request{Kind: ReqNewLayer, Pane: pane}, "session.new_layer", p)
}

// Request sends a raw request and tracks it as req.
func (s *Session) Request(req Request, method string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request(req, method, payload)
}

func (s *Session) request(req Request, method string, payload any) error {
	if s.state == Closed {
		return nil
	}
	id, err := s.client.Request(method, payload)
	if err != nil {
		return err
	}
	s.pending = append(s.pending, pending{id: id, req: req})
	return nil
}

// OnResponse hands an answer to its request's handler.
func (s *Session) OnResponse(r *Response) (Update, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onResponse(r)
}

// OnEvent handles an event: only a newer session view matters.
func (s *Session) OnEvent(ev *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onEvent(ev)
}

func (s *Session) onResponse(r *Response) (Update, error) {
	req, ok := s.takePending(r.ID)
	if !ok {
		return nil, nil
	}
	if r.Result.Err != nil {
		s.logRequestError(req, r.Result.Err)
	}
	switch req.Kind {
	case ReqNewSplit:
		return s.onNewSplitResult(req.Pane, r.Result), nil
	case ReqNewLayer:
		return s.onNewLayerResult(req.Pane, r.Result), nil
	case ReqView:
		return s.onViewResult(r.Result)
	case ReqFocusBlock:
		return nil, s.onFocusBlockResult(r.Result)
	case ReqAttach:
		return nil, s.onAttachResult(r.Result)
	}
	return nil, nil
}

func (s *Session) onEvent(ev *Event) error {
	if ev.Name != "session_view_changed" {
		return nil
	}
	rev, ok := GetInt(ev.Payload, "revision")
	if !ok {
		return nil
	}
	if rev > s.viewRev {
		return s.fetchView()
	}
	return nil
}

func (s *Session) onClosed(c *Close) {
	if c.HasCode {
		s.logf("rexmod: Rex closed %d %s", c.Code, c.Reason)
	} else {
		s.logf("rexmod: Rex closed %s", c.Reason)
	}
	s.state = Closed
}

// Closed: no answers will come, so fail the splits still waiting.
func (s *Session) failPendingSplit() Update {
	for len(s.pending) > 0 {
		p := s.pending[len(s.pending)-1]
		s.pending = s.pending[:len(s.pending)-1]
		switch p.req.Kind {
		case ReqNewSplit:
			return SplitFailed{Pane: p.req.Pane}
		case ReqNewLayer:
			return LayerFailed{Pane: p.req.Pane}
		}
	}
	return nil
}

// takePending removes and returns the request id answers (swap-remove).
func (s *Session) takePending(id uint64) (Request, bool) {
	for i, p := range s.pending {
		if p.id == id {
			last := len(s.pending) - 1
			s.pending[i] = s.pending[last]
			s.pending = s.pending[:last]
			return p.req, true
		}
	}
	return Request{}, false
}

// Attached: a view gives the starting focus.
func (s *Session) onAttachResult(r Result) error {
	if r.Err != nil {
		s.state = Closed
		return nil
	}
	s.state = Attached
	return s.fetchView()
}

// Always an update: the pane is waiting either way.
func (s *Session) onNewSplitResult(pane uint32, r Result) Update {
	if r.Err != nil {
		return SplitFailed{Pane: pane}
	}
	block, ok := newBlockID(r.Ok)
	if !ok {
		s.logUnexpected(Request{Kind: ReqNewSplit, Pane: pane}, "block_ids")
		return SplitFailed{Pane: pane}
	}
	return SplitMade{Pane: pane, Block: block}
}

func (s *Session) onNewLayerResult(pane uint32, r Result) Update {
	if r.Err != nil {
		return LayerFailed{Pane: pane}
	}
	block, ok := newBlockID(r.Ok)
	if !ok {
		s.logUnexpected(Request{Kind: ReqNewLayer, Pane: pane}, "block_ids")
		return LayerFailed{Pane: pane}
	}
	layer, _ := GetStr(r.Ok, "layer_id")
	return LayerMade{Pane: pane, Block: block, Layer: layer}
}

// The block a split made.
func newBlockID(ok any) (string, bool) {
	ids, isArr := Get(ok, "block_ids").([]any)
	if !isArr || len(ids) == 0 {
		return "", false
	}
	return Str(ids[0])
}

// Records the revision our focus change landed at.
func (s *Session) onFocusBlockResult(r Result) error {
	if s.focus != focusRequesting {
		return nil
	}
	s.focusInFlight--
	if r.Err == nil {
		rev, _ := GetInt(r.Ok, "revision")
		if rev > s.focusApplied {
			s.focusApplied = rev
		}
		return nil
	}
	// refused: ask for a view
	if s.focusInFlight == 0 {
		return s.fetchView()
	}
	return nil
}

// Applies the view, then fetches again if it was already out of date.
func (s *Session) onViewResult(r Result) (Update, error) {
	stale := s.viewFetch == fetchStale
	s.viewFetch = fetchIdle
	var u Update
	if r.Err == nil {
		u = s.applyView(r.Ok)
	}
	if stale {
		if err := s.fetchView(); err != nil {
			return u, err
		}
	}
	return u, nil
}

// Fetches the session view, or another after the one on its way.
func (s *Session) fetchView() error {
	switch s.viewFetch {
	case fetchIdle:
		if s.state == Closed {
			return nil
		}
		if err := s.request(Request{Kind: ReqView}, "session.view", map[string]any{"session_id": s.SessionID}); err != nil {
			return err
		}
		s.viewFetch = fetchRunning
	case fetchRunning:
		// the answer will be out of date: fetch again when it arrives
		s.viewFetch = fetchStale
	}
	return nil
}

// Skips views no newer than the last one read. Layout comes first, then
// any focus change.
func (s *Session) applyView(snapshot any) Update {
	rev, ok := GetInt(snapshot, "revision")
	if !ok || rev <= s.viewRev {
		return nil
	}
	s.viewRev = rev
	var focusUpdate *FocusMoved
	if block, ok := findFocusedBlock(snapshot); ok {
		focusUpdate = s.applyFocus(block, rev)
	} else {
		s.logUnexpected(Request{Kind: ReqView}, "focused block")
	}
	windows, isArr := Get(snapshot, "windows").([]any)
	if !isArr {
		if focusUpdate != nil {
			return *focusUpdate
		}
		return nil
	}
	if focusUpdate != nil {
		b := focusUpdate.Block
		s.queuedFocus = &b
	}
	return Layout{Windows: windows}
}

// The focused block of the active window.
func findFocusedBlock(snapshot any) (string, bool) {
	active, ok := GetStr(snapshot, "active_window_id")
	if !ok {
		return "", false
	}
	windows, _ := Get(snapshot, "windows").([]any)
	for _, w := range windows {
		id, ok := GetStr(w, "window_id")
		if !ok {
			continue
		}
		if id == active {
			b, ok := GetStr(w, "focused_block_id")
			if !ok {
				return "", false
			}
			if _, err := Block(b); err != nil {
				return "", false
			}
			return b, true
		}
	}
	return "", false
}

// Reports focus the user moved; echoes of our own change are ignored.
func (s *Session) applyFocus(block string, rev int64) *FocusMoved {
	switch s.focus {
	case focusRequesting:
		if s.focusInFlight > 0 || rev < s.focusApplied {
			return nil
		}
		if s.focusBlock == block {
			s.focus, s.focusBlock = focusSettled, block
			return nil
		}
	case focusSettled:
		if s.focusBlock == block {
			return nil
		}
	}
	report := s.focus != focusUnknown
	s.focus, s.focusBlock = focusSettled, block
	if report {
		return &FocusMoved{Block: block}
	}
	return nil
}

func (s *Session) logRequestError(req Request, e *Error) {
	if req.Kind == ReqNewSplit {
		s.logf("rexmod: Rex new_split for pane %d failed: %s: %s", req.Pane, e.Code, e.Message)
		return
	}
	s.logf("rexmod: Rex %s failed: %s: %s", req.Kind, e.Code, e.Message)
}

func (s *Session) logUnexpected(req Request, what string) {
	s.logf("rexmod: Rex %s answer has no %s", req.Kind, what)
}
