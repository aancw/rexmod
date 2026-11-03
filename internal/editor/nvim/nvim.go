// Package nvim is a connection to Neovim speaking msgpack-RPC: an embedded
// child's stdin and stdout, or a socket to a Neovim started with --listen.
//
// The caller drives reads: Read, then Next until it returns nil. To read
// in another goroutine instead, read from Reader and hand the bytes to Feed.
package nvim

import (
	"errors"
	"io"
	"net"
	"os/exec"
	"time"

	"github.com/alpcanaydin/rexmod/internal/editor/msgpack"
)

// Client is one connection. Not safe for concurrent use.
type Client struct {
	r      io.Reader
	w      io.Writer
	conn   net.Conn  // nil when embedded
	cmd    *exec.Cmd // nil when connected to a socket
	stdin  io.Closer
	out    []byte // buffered writes; send with Flush
	framer msgpack.Framer
	nextID uint32
	// the UI size last sent, as columns and rows
	uiSize [2]uint32
}

// Spawn starts argv, which must include --embed. Neovim's stderr is
// inherited.
func Spawn(argv ...string) (*Client, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Client{r: stdout, w: stdin, cmd: cmd, stdin: stdin, nextID: 1}, nil
}

// Connect connects to the socket of a Neovim started with --listen.
func Connect(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return NewConn(conn), nil
}

// NewConn wraps an open connection.
func NewConn(conn net.Conn) *Client {
	return &Client{r: conn, w: conn, conn: conn, nextID: 1}
}

// ErrTimeout is ConnectRetry giving up.
var ErrTimeout = errors.New("nvim: timed out connecting")

// ConnectRetry is Connect, retrying for up to 2 s: Neovim makes its socket a
// moment after starting.
func ConnectRetry(path string) (*Client, error) {
	for range 200 {
		if c, err := Connect(path); err == nil {
			return c, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, ErrTimeout
}

// Conn is the socket, or nil when embedded.
func (c *Client) Conn() net.Conn { return c.conn }

// Reader is Neovim's stdout, or the socket.
func (c *Client) Reader() io.Reader { return c.r }

// Close sends what's buffered. Embedded: closes Neovim's stdin, which makes
// it exit, then waits for it. Socket: closes the socket; Neovim keeps running.
func (c *Client) Close() error {
	c.Flush()
	if c.cmd != nil {
		c.stdin.Close()
		return c.cmd.Wait()
	}
	return c.conn.Close()
}

// Request buffers a request and returns its id. Send with Flush.
func (c *Client) Request(method string, args ...any) (uint32, error) {
	id := c.nextID
	c.nextID++
	if args == nil {
		args = []any{}
	}
	b, err := appendRequest(c.out, id, method, args)
	if err != nil {
		return 0, err
	}
	c.out = b
	return id, nil
}

// Notify buffers a notification. Send with Flush.
func (c *Client) Notify(method string, args ...any) error {
	if args == nil {
		args = []any{}
	}
	b := msgpack.AppendArrayHeader(c.out, 3)
	b = msgpack.AppendInt(b, 2)
	b = msgpack.AppendStr(b, method)
	b, err := msgpack.Append(b, args)
	if err != nil {
		return err
	}
	c.out = b
	return nil
}

// Flush sends everything buffered so far.
func (c *Client) Flush() error {
	if len(c.out) == 0 {
		return nil
	}
	_, err := c.w.Write(c.out)
	c.out = c.out[:0]
	return err
}

// Respond answers request id with result and flushes.
func (c *Client) Respond(id uint32, result any) error {
	return c.respond(id, nil, result)
}

// RespondError answers request id with an error, so Neovim stops waiting,
// and flushes.
func (c *Client) RespondError(id uint32, message string) error {
	return c.respond(id, message, nil)
}

func (c *Client) respond(id uint32, e, result any) error {
	b, err := msgpack.Append(c.out, []any{1, id, e, result})
	if err != nil {
		return err
	}
	c.out = b
	return c.Flush()
}

// Read reads what Neovim has sent, blocking until something arrives. False
// once Neovim has exited. Invalidates messages from Next.
func (c *Client) Read() (bool, error) {
	var buf [64 * 1024]byte
	n, err := c.r.Read(buf[:])
	if n > 0 {
		c.framer.Feed(buf[:n])
		return true, nil
	}
	if err == io.EOF || errors.Is(err, net.ErrClosed) {
		return false, nil
	}
	return err == nil, err
}

// Feed adds bytes read elsewhere, as Read would.
func (c *Client) Feed(b []byte) { c.framer.Feed(b) }

// Next returns the next whole message, or nil until Read gets more.
func (c *Client) Next() (Message, error) {
	b, err := c.framer.Next()
	if b == nil || err != nil {
		return nil, err
	}
	return Parse(b)
}

// UIOptions for UIAttach. ext_linegrid and rgb are always on.
type UIOptions struct {
	// Unset default colors arrive as -1, so the terminal's own are used.
	ExtTermcolors bool
	// A grid per window.
	ExtMultigrid bool
	// Receive nvim_ui_send data (kitty graphics from image plugins) as
	// ui_send redraw events, to write to the panes' terminals.
	StdoutTTY bool
}

// UIAttach attaches as a UI of cols x rows and flushes.
func (c *Client) UIAttach(cols, rows uint32, o UIOptions) error {
	opts := msgpack.Map{
		{Key: "ext_linegrid", Val: true},
		{Key: "rgb", Val: true},
		{Key: "ext_termcolors", Val: o.ExtTermcolors},
		{Key: "ext_multigrid", Val: o.ExtMultigrid},
	}
	if o.StdoutTTY {
		opts = append(opts, msgpack.KV{Key: "stdout_tty", Val: true})
	}
	if _, err := c.Request("nvim_ui_attach", cols, rows, opts); err != nil {
		return err
	}
	c.uiSize = [2]uint32{cols, rows}
	return c.Flush()
}

// UITryResize asks to resize the UI and flushes. Neovim answers with
// grid_resize. Does nothing for the size it already has: every resize
// redraws the whole screen.
func (c *Client) UITryResize(cols, rows uint32) error {
	if c.uiSize == [2]uint32{cols, rows} {
		return nil
	}
	if _, err := c.Request("nvim_ui_try_resize", cols, rows); err != nil {
		return err
	}
	c.uiSize = [2]uint32{cols, rows}
	return c.Flush()
}

// UITryResizeGrid asks for grid (a window's) to be cols x rows. Doesn't flush.
func (c *Client) UITryResizeGrid(grid, cols, rows uint32) error {
	_, err := c.Request("nvim_ui_try_resize_grid", grid, max(cols, 1), max(rows, 1))
	return err
}

// ExecLua buffers nvim_exec_lua(code, args) and returns its id. Send with
// Flush.
func (c *Client) ExecLua(code string, args ...any) (uint32, error) {
	if args == nil {
		args = []any{}
	}
	return c.Request("nvim_exec_lua", code, args)
}
