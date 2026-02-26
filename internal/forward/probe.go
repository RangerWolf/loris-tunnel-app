package forward

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"loris-tunnel/internal/model"

	"golang.org/x/crypto/ssh"
)

// TestJumperLatency measures pure SSH channel round-trip latency via keepalive.
func TestJumperLatency(client *ssh.Client) (time.Duration, error) {
	if client == nil {
		return 0, fmt.Errorf("ssh client is nil")
	}

	start := time.Now()
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

// TestJumperConnection verifies SSH handshake/auth against the jumper.
func TestJumperConnection(jumper model.Jumper) error {
	client, err := dialSSH(jumper)
	if err != nil {
		return err
	}
	return client.Close()
}

// TestTunnelConnection verifies tunnel prerequisites and target reachability.
// Currently it supports "local", "remote" and "dynamic" modes only.
func TestTunnelConnection(tunnel model.Tunnel, jumpers []model.Jumper) (time.Duration, error) {
	mode := strings.TrimSpace(tunnel.Mode)
	if mode == "" {
		mode = "local"
	}
	if mode != "local" && mode != "remote" && mode != "dynamic" {
		return 0, fmt.Errorf("mode %s test is not supported yet", mode)
	}

	if mode == "local" || mode == "dynamic" {
		localHost := strings.TrimSpace(tunnel.LocalHost)
		if localHost == "" {
			localHost = "127.0.0.1"
		}
		localAddr := net.JoinHostPort(localHost, strconv.Itoa(tunnel.LocalPort))
		ln, err := net.Listen("tcp", localAddr)
		if err != nil {
			return 0, fmt.Errorf("local listen %s failed: %w", localAddr, err)
		}
		_ = ln.Close()
	}

	client, closeChain, err := dialSSHChain(jumpers)
	if err != nil {
		return 0, err
	}
	defer closeChain()

	latency, err := TestJumperLatency(client)
	if err != nil {
		return 0, fmt.Errorf("measure ssh latency failed: %w", err)
	}

	if mode == "dynamic" {
		if err := probeDynamicForwardCapability(client); err != nil {
			return 0, err
		}
		return latency, nil
	}
	if mode == "remote" {
		if err := probeRemoteListen(client, tunnel.RemoteHost, tunnel.RemotePort); err != nil {
			return 0, err
		}
		return latency, nil
	}

	if err := probeRemoteDial(client, tunnel.RemoteHost, tunnel.RemotePort); err != nil {
		return 0, err
	}
	return latency, nil
}

func probeRemoteDial(client *ssh.Client, remoteHost string, remotePort int) error {
	addr := net.JoinHostPort(strings.TrimSpace(remoteHost), strconv.Itoa(remotePort))
	remoteConn, err := client.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("remote dial %s failed: %w", addr, err)
	}
	return remoteConn.Close()
}

func probeRemoteListen(client *ssh.Client, remoteHost string, remotePort int) error {
	host := strings.TrimSpace(remoteHost)
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(remotePort))
	ln, err := client.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("remote listen %s failed: %w", addr, err)
	}
	return ln.Close()
}

func probeDynamicForwardCapability(client *ssh.Client) error {
	// Use a closed local target to detect "forwarding prohibited" without requiring a real endpoint.
	probeAddr := "127.0.0.1:1"
	remoteConn, err := client.Dial("tcp", probeAddr)
	if err == nil {
		return remoteConn.Close()
	}
	if isPortForwardDenied(err) {
		return fmt.Errorf("dynamic forward is not allowed by ssh server: %w", err)
	}
	return nil
}

func isPortForwardDenied(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "administratively prohibited") ||
		strings.Contains(msg, "forwarding disabled") ||
		strings.Contains(msg, "port forwarding disabled")
}
