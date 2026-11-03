// Package fdpass passes file descriptors over a Unix socket (SCM_RIGHTS).
// TLPI section 61.13.3. Ported from neorex (MIT).
package fdpass

import (
	"errors"
	"io"
	"net"
	"syscall"
)

// maxFds is the most fds one read takes; extras are closed.
const maxFds = 4

// Send writes b with a copy of fd attached. b must not be empty.
func Send(c *net.UnixConn, b []byte, fd int) error {
	if len(b) == 0 {
		return errors.New("fdpass: empty message")
	}
	n, _, err := c.WriteMsgUnix(b, syscall.UnixRights(fd), nil)
	if err != nil {
		return err
	}
	if n < len(b) {
		// the fd rode with the first bytes; send the rest plainly
		_, err = c.Write(b[n:])
	}
	return err
}

// Recv does one read into buf, keeping the first passed fd (close-on-exec,
// owned by the caller) or -1 if none came. n is 0 when the peer hung up.
func Recv(c *net.UnixConn, buf []byte) (n int, fd int, err error) {
	oob := make([]byte, syscall.CmsgSpace(maxFds*4))
	n, oobn, _, _, err := c.ReadMsgUnix(buf, oob)
	fd = -1
	if oobn > 0 {
		msgs, perr := syscall.ParseSocketControlMessage(oob[:oobn])
		if perr == nil {
			for i := range msgs {
				fds, rerr := syscall.ParseUnixRights(&msgs[i])
				if rerr != nil {
					continue
				}
				for _, got := range fds {
					if fd != -1 {
						syscall.Close(got)
						continue
					}
					// macOS has no MSG_CMSG_CLOEXEC
					syscall.CloseOnExec(got)
					fd = got
				}
			}
		}
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) {
			return 0, fd, nil
		}
		return n, fd, err
	}
	return n, fd, nil
}
