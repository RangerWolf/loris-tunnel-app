//go:build unix

package frp

import (
	"bufio"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestStopLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		forced       bool
	}{
		{"graceful", "trap 'exit 0' TERM; echo ready; while :; do sleep 1; done", false},
		{"ignores-term", "trap '' TERM; echo ready; exec sleep 60", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", tc.script)
			configureCommand(cmd)
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			h := &Host{cmd: cmd, done: make(chan struct{})}
			go func() { _ = cmd.Wait(); close(h.done) }()
			t.Cleanup(func() {
				select {
				case <-h.done:
					return
				default:
					forceStop(cmd)
				}
				<-h.done
			})
			ready := make(chan bool, 1)
			go func() { scanner := bufio.NewScanner(output); ready <- scanner.Scan() && scanner.Text() == "ready" }()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child did not become ready")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("child readiness timed out")
			}
			started := time.Now()
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); _ = h.Stop() }()
			}
			stopped := make(chan struct{})
			go func() { wg.Wait(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(6 * time.Second):
				t.Fatal("Stop did not finish")
			}
			elapsed := time.Since(started)
			if tc.forced {
				if elapsed < 3*time.Second {
					t.Fatal("did not allow graceful stop timeout")
				}
				if cmd.ProcessState.Success() {
					t.Fatal("expected forced termination")
				}
			} else {
				if elapsed >= 3*time.Second {
					t.Fatal("graceful stop reached kill timeout")
				}
				if !cmd.ProcessState.Success() {
					t.Fatal("child did not exit gracefully")
				}
			}
			if err := h.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
