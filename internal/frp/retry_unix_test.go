//go:build unix

package frp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRetryOnlyReclaimsMatchingOrphan(t *testing.T) {
	t.Setenv("LORIS_FRP_TEST_CHILD", "1")
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Store: &Store{Path: filepath.Join(root, "local.json")}}
	if err := c.Store.SetBinary(binary); err != nil {
		t.Fatal(err)
	}
	server := testServer()
	makeConfig := func(label string, s Server) string {
		dir := filepath.Join(root, "frp", "run-"+label)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		data, err := configuration(s, "web", "tcp", "127.0.0.1", 3000, 13000, "", 0, "secret")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "frpc.toml")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	spawnOrphan := func(path string) orphanProcess {
		cmd := exec.Command("sh", "-c", `"$1" -c "$2" >/dev/null 2>&1 & echo $!`, "sh", binary, path)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			procs, err := orphanProcesses(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range procs {
				if p.pid == pid {
					t.Cleanup(func() {
						ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
						defer cancel()
						_ = stopOrphan(ctx, p)
					})
					return p
				}
			}
			if ctx.Err() != nil {
				t.Fatal("child did not become orphan")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	target := makeConfig("target", server)
	orphan := spawnOrphan(target)
	other := server
	other.Port++
	otherPath := makeConfig("other", other)
	keep := spawnOrphan(otherPath)
	stale := keep
	stale.identity += "-stale"
	if err := stopOrphan(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	canceled, cancelRetry := context.WithCancel(context.Background())
	cancelRetry()
	if _, err := c.Retry(canceled, server, "web", "tcp", "127.0.0.1", 3000, 13000, ""); err == nil {
		t.Fatal("canceled retry succeeded")
	}
	livePath := makeConfig("live", server)
	live := exec.Command(binary, "-c", livePath)
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	liveDone := make(chan struct{})
	go func() { _ = live.Wait(); close(liveDone) }()
	t.Cleanup(func() {
		select {
		case <-liveDone:
			return
		default:
			_ = live.Process.Kill()
			<-liveDone
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := c.Retry(ctx, server, "web", "tcp", "127.0.0.1", 3000, 13000, "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	procs, err := orphanProcesses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundKeep := false
	for _, p := range procs {
		if p.pid == orphan.pid {
			t.Fatal("matching orphan survived retry")
		}
		if p == keep {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatal("different server orphan was stopped")
	}
	select {
	case <-liveDone:
		t.Fatal("live application client was stopped")
	default:
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("old config was not removed")
	}
	for _, path := range []string{otherPath, livePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetryConfigIdentity(t *testing.T) {
	binary := "/Applications/Loris Tunnel/frpc"
	config := "/tmp/Application Support/frp/run-a/frpc.toml"
	for _, command := range []string{binary + " -c " + config + ".bak", "other -c " + config, binary + " -c " + config + " extra"} {
		if usesRetryConfig(command, binary, config) {
			t.Fatalf("matched unrelated command %q", command)
		}
	}
	if !usesRetryConfig(binary+" -c "+config, binary, config) {
		t.Fatal("spaced paths did not match")
	}
}

func TestInspectRetryPIDDistinguishesFailure(t *testing.T) {
	ctx := context.Background()
	t.Run("live-owner", func(t *testing.T) {
		_, exists, orphan, err := inspectRetryPID(ctx, os.Getpid())
		if err != nil || !exists || orphan {
			t.Fatalf("exists=%v orphan=%v err=%v", exists, orphan, err)
		}
	})
	t.Run("missing-tool", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, _, _, err := inspectRetryPID(ctx, os.Getpid()); err == nil {
			t.Fatal("inspection failure treated as exit")
		}
	})
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, _, _, err := inspectRetryPID(ctx, os.Getpid()); err == nil {
			t.Fatal("cancellation ignored")
		}
	})
}

func TestRetryWithoutMatchingConfigNeedsNoScanner(t *testing.T) {
	t.Setenv("LORIS_FRP_TEST_CHILD", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "frp", "run-unrelated")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frpc.toml"), []byte("serverAddr = \"other.example.com\"\nserverPort = 7000\n[[proxies]]\nname = \"other\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Client{Store: &Store{Path: filepath.Join(root, "local.json")}}
	if err := c.Store.SetBinary(binary); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	h, err := c.Retry(ctx, testServer(), "web", "tcp", "127.0.0.1", 3000, 13000, "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
}
