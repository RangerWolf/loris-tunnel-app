package forward

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
)

const (
	socksVersion        = 0x05
	socksAuthNoAuth     = 0x00
	socksAuthNoAccepted = 0xFF
	socksCmdConnect     = 0x01

	socksAddrIPv4   = 0x01
	socksAddrDomain = 0x03
	socksAddrIPv6   = 0x04

	socksReplySucceeded          = 0x00
	socksReplyGeneralFailure     = 0x01
	socksReplyCommandUnsupported = 0x07
	socksReplyAddressUnsupported = 0x08
)

func readSOCKS5ConnectTarget(conn net.Conn) (string, error) {
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return "", fmt.Errorf("read socks5 greeting failed: %w", err)
	}
	if greeting[0] != socksVersion {
		return "", fmt.Errorf("unsupported socks version: %d", greeting[0])
	}

	methodCount := int(greeting[1])
	if methodCount <= 0 {
		return "", fmt.Errorf("empty socks5 auth methods")
	}
	methods := make([]byte, methodCount)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", fmt.Errorf("read socks5 auth methods failed: %w", err)
	}

	if !containsSOCKSAuthMethod(methods, socksAuthNoAuth) {
		_, _ = conn.Write([]byte{socksVersion, socksAuthNoAccepted})
		return "", fmt.Errorf("socks5 no-auth method is not accepted by client")
	}
	if _, err := conn.Write([]byte{socksVersion, socksAuthNoAuth}); err != nil {
		return "", fmt.Errorf("write socks5 auth response failed: %w", err)
	}

	var reqHeader [4]byte
	if _, err := io.ReadFull(conn, reqHeader[:]); err != nil {
		return "", fmt.Errorf("read socks5 request header failed: %w", err)
	}
	if reqHeader[0] != socksVersion {
		return "", fmt.Errorf("unsupported socks request version: %d", reqHeader[0])
	}

	cmd := reqHeader[1]
	atyp := reqHeader[3]
	if cmd != socksCmdConnect {
		_ = writeSOCKS5Reply(conn, socksReplyCommandUnsupported)
		return "", fmt.Errorf("unsupported socks5 command: %d", cmd)
	}

	host, err := readSOCKSAddressHost(conn, atyp)
	if err != nil {
		_ = writeSOCKS5Reply(conn, socksReplyAddressUnsupported)
		return "", err
	}

	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return "", fmt.Errorf("read socks5 target port failed: %w", err)
	}
	port := int(binary.BigEndian.Uint16(portBytes[:]))
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func writeSOCKS5Reply(conn net.Conn, reply byte) error {
	_, err := conn.Write([]byte{
		socksVersion,
		reply,
		0x00,
		socksAddrIPv4,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
	})
	return err
}

func containsSOCKSAuthMethod(methods []byte, want byte) bool {
	for _, method := range methods {
		if method == want {
			return true
		}
	}
	return false
}

func readSOCKSAddressHost(conn net.Conn, atyp byte) (string, error) {
	switch atyp {
	case socksAddrIPv4:
		var raw [4]byte
		if _, err := io.ReadFull(conn, raw[:]); err != nil {
			return "", fmt.Errorf("read socks5 ipv4 failed: %w", err)
		}
		return net.IP(raw[:]).String(), nil
	case socksAddrDomain:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return "", fmt.Errorf("read socks5 domain length failed: %w", err)
		}
		if size[0] == 0 {
			return "", fmt.Errorf("invalid socks5 domain length")
		}
		raw := make([]byte, int(size[0]))
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", fmt.Errorf("read socks5 domain failed: %w", err)
		}
		return string(raw), nil
	case socksAddrIPv6:
		var raw [16]byte
		if _, err := io.ReadFull(conn, raw[:]); err != nil {
			return "", fmt.Errorf("read socks5 ipv6 failed: %w", err)
		}
		return net.IP(raw[:]).String(), nil
	default:
		return "", fmt.Errorf("unsupported socks5 address type: %d", atyp)
	}
}
