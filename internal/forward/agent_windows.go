//go:build windows

package forward

import (
	"net"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const windowsSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

func dialSSHAgentSocket(sock string) (net.Conn, error) {
	pathPtr, err := windows.UTF16PtrFromString(sock)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: "pipe", Addr: pipeAddr(sock), Err: err}
	}
	return &pipeConn{f: os.NewFile(uintptr(h), sock), addr: pipeAddr(sock)}, nil
}

func defaultAgentSocketCandidates() []string {
	return []string{windowsSSHAgentPipe}
}

// pipeConn wraps os.File as net.Conn for Windows named pipes.
type pipeConn struct {
	f    *os.File
	addr pipeAddr
}

func (c *pipeConn) Read(b []byte) (int, error)  { return c.f.Read(b) }
func (c *pipeConn) Write(b []byte) (int, error) { return c.f.Write(b) }
func (c *pipeConn) Close() error                { return c.f.Close() }
func (c *pipeConn) LocalAddr() net.Addr         { return c.addr }
func (c *pipeConn) RemoteAddr() net.Addr        { return c.addr }

func (c *pipeConn) SetDeadline(_ time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(_ time.Time) error { return nil }

type pipeAddr string

func (p pipeAddr) Network() string { return "pipe" }
func (p pipeAddr) String() string  { return string(p) }
