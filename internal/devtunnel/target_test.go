package devtunnel

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestValidTargetHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "192.168.1.20", "::1", "2001:db8::1", "localhost", "app.internal", "web-server", "app.internal."} {
		if !ValidTargetHost(host) {
			t.Errorf("rejected %q", host)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "::", "::ffff:0.0.0.0", "1.2.3", "256.1.2.3", "01.2.3.4", "123", "1.2.3.", "a..b", "-app.local", "app-.local", "https://app.local", "host:8080", "[::1]", "2001:::1", " host", "a/b", "a_b"} {
		if ValidTargetHost(host) {
			t.Errorf("accepted %q", host)
		}
	}
}

func bridgeTarget(t *testing.T) net.Listener {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addresses {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || ip.To4() == nil || ip.IsLoopback() {
			continue
		}
		ln, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return ln
	}
	t.Skip("no non-loopback IPv4 interface")
	return nil
}

func TestTargetBridgeForwardsAndPreservesHalfClose(t *testing.T) {
	target := bridgeTarget(t)
	address := target.Addr().(*net.TCPAddr)
	go func() {
		conn, err := target.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		data, _ := io.ReadAll(conn)
		_, _ = conn.Write(append([]byte("reply:"), data...))
	}()
	stop, err := startTargetBridge(context.Background(), address.IP.String(), address.Port)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(address.Port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte("request"))
	_ = conn.(*net.TCPConn).CloseWrite()
	data, err := io.ReadAll(conn)
	if err != nil || string(data) != "reply:request" {
		t.Fatalf("reply=%q, error=%v", data, err)
	}
}

func TestTargetBridgeStopClosesConnectionsAndReleasesPort(t *testing.T) {
	target := bridgeTarget(t)
	address := target.Addr().(*net.TCPAddr)
	go func() {
		conn, err := target.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	stop, err := startTargetBridge(context.Background(), address.IP.String(), address.Port)
	if err != nil {
		t.Fatal(err)
	}
	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(address.Port))
	conn, err := net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte("x"))
	if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil {
		stop()
		t.Fatal(err)
	}
	stop()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection survived bridge stop")
	}
	ln, err := net.Listen("tcp4", local)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if closeBridge, err := startTargetBridge(context.Background(), address.IP.String(), address.Port); err == nil {
		closeBridge()
		t.Fatal("occupied local port accepted")
	}
}

func TestMappedTargetBridgeDistinguishesSamePortOnDifferentHosts(t *testing.T) {
	target := bridgeTarget(t)
	addr := target.Addr().(*net.TCPAddr)
	local, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	available, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	publicPort := available.Addr().(*net.TCPAddr).Port
	available.Close()
	serve := func(listener net.Listener, label string) {
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = conn.Write([]byte(label))
		}()
	}
	serve(local, "local-service")
	serve(target, "other-host-service")
	stop, err := startMappedTargetBridge(t.Context(), addr.IP.String(), addr.Port, publicPort)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	read := func(port int) string {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		data, err := io.ReadAll(conn)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if read(addr.Port) != "local-service" || read(publicPort) != "other-host-service" {
		t.Fatal("service targets were mixed")
	}
	stop()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(publicPort)), time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("stopped public mapping still accepts connections")
	}
	conn, err = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)), time.Second)
	if err != nil {
		t.Fatal("stopping the mapping affected the other local service", err)
	}
	conn.Close()
}
