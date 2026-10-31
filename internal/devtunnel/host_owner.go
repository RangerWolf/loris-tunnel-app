package devtunnel

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"loris-tunnel/internal/conf"
)

type hostProcess struct {
	PID     int    `json:"pid"`
	Started string `json:"started"`
}

type hostOwner struct {
	lock   *os.File
	record string
}

func (c *Client) claimHost(id string) (*hostOwner, error) {
	dir := c.hostDir
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(cache, "loris-tunnel", "devtunnel-hosts")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(id))))
	lock, err := os.OpenFile(filepath.Join(dir, key+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockHostFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("this Tunnel ID is already managed by another Loris Tunnel instance: %w", err)
	}
	owner := &hostOwner{lock: lock, record: filepath.Join(dir, key+".json")}
	if err := owner.recover(); err != nil {
		lock.Close()
		return nil, err
	}
	return owner, nil
}

func (o *hostOwner) recover() error {
	data, err := os.ReadFile(o.record)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var process hostProcess
	if err := json.Unmarshal(data, &process); err != nil {
		slog.Warn("ignoring invalid CLI process record; unverified processes will not be stopped", "error", err)
		return nil
	}
	if process.PID <= 1 || process.Started == "" {
		return nil
	}
	identity, err := processIdentity(process.PID)
	if err != nil {
		return fmt.Errorf("cannot verify previous CLI process: %w", err)
	}
	if identity != process.Started {
		return nil
	}
	// The file lock proves the previous owner has exited. Creation time prevents PID reuse.
	killHostPID(process.PID)
	return nil
}

func (o *hostOwner) track(pid int) error {
	identity, err := processIdentity(pid)
	if err != nil {
		return err
	}
	if identity == "" {
		return nil
	} // The process may have already failed and exited.
	data, err := json.Marshal(hostProcess{PID: pid, Started: identity})
	if err != nil {
		return err
	}
	return conf.WritePrivateAtomic(o.record, data)
}

func (o *hostOwner) close() {
	_ = os.Remove(o.record)
	_ = o.lock.Close()
}
