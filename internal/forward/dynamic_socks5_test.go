package forward

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestReadSOCKS5ConnectTarget_Domain(t *testing.T) {
	serverConn, clientConn := makePipeConns(t)
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		if _, err := clientConn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			errCh <- err
			return
		}

		resp := make([]byte, 2)
		if _, err := io.ReadFull(clientConn, resp); err != nil {
			errCh <- err
			return
		}
		if resp[0] != socksVersion || resp[1] != socksAuthNoAuth {
			errCh <- fmt.Errorf("unexpected auth response: %v", resp)
			return
		}

		host := "example.com"
		req := []byte{0x05, socksCmdConnect, 0x00, socksAddrDomain, byte(len(host))}
		req = append(req, []byte(host)...)
		req = append(req, 0x00, 0x50) // 80
		if _, err := clientConn.Write(req); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	target, err := readSOCKS5ConnectTarget(serverConn)
	if err != nil {
		t.Fatalf("readSOCKS5ConnectTarget() error = %v", err)
	}
	if target != "example.com:80" {
		t.Fatalf("target = %s, want example.com:80", target)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client side error = %v", err)
	}
}

func TestReadSOCKS5ConnectTarget_UnsupportedCommand(t *testing.T) {
	serverConn, clientConn := makePipeConns(t)
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		if _, err := clientConn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			errCh <- err
			return
		}

		resp := make([]byte, 2)
		if _, err := io.ReadFull(clientConn, resp); err != nil {
			errCh <- err
			return
		}
		if resp[0] != socksVersion || resp[1] != socksAuthNoAuth {
			errCh <- fmt.Errorf("unexpected auth response: %v", resp)
			return
		}

		// Only write request header because parser rejects unsupported command before reading address/port.
		req := []byte{0x05, 0x02, 0x00, socksAddrIPv4}
		if _, err := clientConn.Write(req); err != nil {
			errCh <- err
			return
		}

		reply := make([]byte, 10)
		if _, err := io.ReadFull(clientConn, reply); err != nil {
			errCh <- err
			return
		}
		if reply[0] != socksVersion || reply[1] != socksReplyCommandUnsupported {
			errCh <- fmt.Errorf("unexpected command reject response: %v", reply)
			return
		}
		errCh <- nil
	}()

	_, err := readSOCKS5ConnectTarget(serverConn)
	if err == nil || !strings.Contains(err.Error(), "unsupported socks5 command") {
		t.Fatalf("expected unsupported command error, got %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client side error = %v", err)
	}
}

func TestReadSOCKS5ConnectTarget_NoNoAuthMethod(t *testing.T) {
	serverConn, clientConn := makePipeConns(t)
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		if _, err := clientConn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
			errCh <- err
			return
		}

		resp := make([]byte, 2)
		if _, err := io.ReadFull(clientConn, resp); err != nil {
			errCh <- err
			return
		}
		if resp[0] != socksVersion || resp[1] != socksAuthNoAccepted {
			errCh <- fmt.Errorf("unexpected no-auth rejection response: %v", resp)
			return
		}
		errCh <- nil
	}()

	_, err := readSOCKS5ConnectTarget(serverConn)
	if err == nil || !strings.Contains(err.Error(), "no-auth method") {
		t.Fatalf("expected no-auth method error, got %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("client side error = %v", err)
	}
}

func makePipeConns(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	return net.Pipe()
}
