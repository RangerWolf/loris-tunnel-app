package devtunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func ValidTargetHost(host string) bool {
	if host == "" || host != strings.TrimSpace(host) {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsUnspecified()
	}
	if len(host) > 253 || strings.Trim(host, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func directLocalTarget(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func startTargetBridge(ctx context.Context, host string, port int) (func(), error) {
	return startMappedTargetBridge(ctx, host, port, port)
}

func startMappedTargetBridge(ctx context.Context, host string, port, listenPort int) (func(), error) {
	if !ValidTargetHost(host) || port < 1 || port > 65535 || listenPort < 1 || listenPort > 65535 {
		return nil, fmt.Errorf("invalid target address")
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve target host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("target host has no IP address")
		}
		if addresses[0].IP.IsUnspecified() {
			return nil, fmt.Errorf("target host resolves to an unspecified address")
		}
		host = addresses[0].IP.String()
	}
	if directLocalTarget(host) && port == listenPort {
		return func() {}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	var listeners []net.Listener
	var workers sync.WaitGroup
	closeBridge := func() {
		cancel()
		for _, ln := range listeners {
			_ = ln.Close()
		}
		workers.Wait()
	}
	for _, address := range []string{"127.0.0.1", "::1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(address, strconv.Itoa(listenPort)))
		if err != nil {
			if address == "::1" && (errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL)) {
				continue
			}
			closeBridge()
			return nil, fmt.Errorf("cannot forward to %s: local port %d is unavailable: %w", host, port, err)
		}
		listeners = append(listeners, ln)
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))
	for _, ln := range listeners {
		workers.Add(1)
		go func(ln net.Listener) {
			defer workers.Done()
			for {
				incoming, err := ln.Accept()
				if err != nil {
					return
				}
				workers.Add(1)
				go func() {
					defer workers.Done()
					defer incoming.Close()
					outgoing, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", target)
					if err != nil {
						return
					}
					defer outgoing.Close()
					stop := context.AfterFunc(ctx, func() { _ = incoming.Close(); _ = outgoing.Close() })
					defer stop()
					copied := make(chan struct{})
					go func() {
						_, _ = io.Copy(outgoing, incoming)
						_ = outgoing.(*net.TCPConn).CloseWrite()
						close(copied)
					}()
					_, _ = io.Copy(incoming, outgoing)
					_ = incoming.(*net.TCPConn).CloseWrite()
					<-copied
				}()
			}
		}(ln)
	}
	return closeBridge, nil
}
