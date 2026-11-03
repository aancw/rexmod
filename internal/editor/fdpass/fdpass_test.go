package fdpass

import (
	"net"
	"os"
	"syscall"
	"testing"
)

func socketPair(t *testing.T) [2]*net.UnixConn {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out [2]*net.UnixConn
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "sock")
		c, err := net.FileConn(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[i] = c.(*net.UnixConn)
	}
	return out
}

func TestPassedPipeWritesIntoOriginal(t *testing.T) {
	s := socketPair(t)
	defer s[0].Close()
	defer s[1].Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := Send(s[0], []byte("p"), int(w.Fd())); err != nil {
		t.Fatal(err)
	}
	w.Close() // the receiver's copy keeps it open

	buf := make([]byte, 8)
	n, fd, err := Recv(s[1], buf)
	if err != nil || string(buf[:n]) != "p" || fd < 0 {
		t.Fatalf("n=%d fd=%d err=%v", n, fd, err)
	}
	flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if e != 0 || flags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("not close-on-exec")
	}
	syscall.Write(fd, []byte("hi"))
	syscall.Close(fd)

	out := make([]byte, 8)
	m, _ := r.Read(out)
	if string(out[:m]) != "hi" {
		t.Fatalf("%q", out[:m])
	}
}

func TestPlainBytesThenHangUp(t *testing.T) {
	s := socketPair(t)
	defer s[1].Close()
	if _, err := s[0].Write([]byte("w")); err != nil {
		t.Fatal(err)
	}
	s[0].Close()
	buf := make([]byte, 8)
	n, fd, err := Recv(s[1], buf)
	if err != nil || n != 1 || fd != -1 {
		t.Fatalf("n=%d fd=%d err=%v", n, fd, err)
	}
	n, _, err = Recv(s[1], buf)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
