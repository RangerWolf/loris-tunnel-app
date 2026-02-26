//go:build !windows

package forward

import "net"

func dialSSHAgentSocket(sock string) (net.Conn, error) {
	return net.Dial("unix", sock)
}

func defaultAgentSocketCandidates() []string {
	return nil
}
