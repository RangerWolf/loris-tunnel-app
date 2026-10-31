package frp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type orphanProcess struct {
	pid               int
	identity, command string
}

// Retry only reclaims orphaned clients from this store. Live app instances retain ownership.
func (c *Client) Retry(ctx context.Context, s Server, name, kind, host string, port, remote int, domain string) (result *Host, retryErr error) {
	defer func() {
		if retryErr != nil {
			slog.Warn("FRP retry failed", "proxy", name, "server", s.Host, "port", s.Port)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.Normalize(); err != nil {
		return nil, err
	}
	if err := ValidateProxy(kind, host, port, remote, domain); err != nil {
		return nil, err
	}
	base := filepath.Join(filepath.Dir(c.Store.Path), "frp")
	dirs, err := filepath.Glob(filepath.Join(base, "run-*", "frpc.toml"))
	if err != nil {
		return nil, err
	}
	candidates := dirs[:0]
	for _, path := range dirs {
		if matchesRetryProxy(path, s, name) {
			candidates = append(candidates, path)
		}
	}
	dirs = candidates
	if len(dirs) == 0 {
		slog.Debug("FRP retry has no matching config", "proxy", name)
		return c.Start(ctx, s, name, kind, host, port, remote, domain)
	}
	binary, err := c.resolve()
	if err != nil {
		return nil, fmt.Errorf("frpc is not installed")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	processes, err := orphanProcesses(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect previous FRP client: %w", err)
	}
	matched := false
	for _, config := range dirs {
		if !matchesRetryProxy(config, s, name) {
			continue
		}
		absolute, err := filepath.Abs(config)
		if err != nil {
			return nil, err
		}
		for _, process := range processes {
			if !usesRetryConfig(process.command, binary, absolute) {
				continue
			}
			matched = true
			slog.Info("FRP retry stopping orphan", "proxy", name, "pid", process.pid)
			if err := stopOrphan(ctx, process); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			slog.Info("FRP retry orphan exited", "proxy", name, "pid", process.pid)
			if err := os.RemoveAll(filepath.Dir(config)); err != nil {
				return nil, err
			}
		}
	}
	if !matched {
		slog.Debug("FRP retry found no matching orphan", "proxy", name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Start(ctx, s, name, kind, host, port, remote, domain)
}

func matchesRetryProxy(path string, s Server, name string) bool {
	var cfg struct {
		ServerAddr string
		ServerPort int
		User       string
		Proxies    []struct{ Name string }
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return false
	}
	return strings.EqualFold(cfg.ServerAddr, s.Host) && cfg.ServerPort == s.Port && cfg.User == s.User && len(cfg.Proxies) == 1 && cfg.Proxies[0].Name == name
}

func stopOrphan(ctx context.Context, original orphanProcess) error {
	// Recheck creation time and orphan status before each signal, preventing stale PID reuse.
	signal := func(force bool) (bool, error) {
		current, exists, orphan, err := inspectRetryPID(ctx, original.pid)
		if err != nil {
			return false, err
		}
		if exists && current.identity == original.identity {
			if !orphan || current.command != original.command {
				return false, fmt.Errorf("previous FRP client ownership changed")
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			return true, signalOrphan(current.pid, force)
		}
		return false, nil
	}
	exists, err := signal(false)
	if err != nil || !exists {
		return err
	}
	grace := time.NewTimer(3 * time.Second)
	defer grace.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	forced := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-grace.C:
			if forced {
				return fmt.Errorf("previous FRP client has not exited; retry after it stops")
			}
			slog.Info("FRP orphan graceful stop timed out", "pid", original.pid)
			exists, err = signal(true)
			if err != nil || !exists {
				return err
			}
			forced = true
			grace.Reset(2 * time.Second)
		case <-tick.C:
			current, exists, orphan, err := inspectRetryPID(ctx, original.pid)
			if err != nil {
				return err
			}
			found := exists && current.identity == original.identity
			if found && (!orphan || current.command != original.command) {
				return fmt.Errorf("previous FRP client ownership changed")
			}
			if !found {
				return nil
			}
		}
	}
}
