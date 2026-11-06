package devtunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("LORIS_TEST_CLI"); mode != "" {
		state := os.Getenv("LORIS_TEST_CLI_STATE")
		if mode == "owner" {
			client := NewClient(os.Args[0])
			client.hostDir = os.Getenv("LORIS_TEST_CLI_HOST_DIR")
			_ = os.Setenv("LORIS_TEST_CLI", "ready")
			host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
			if err := host.Start(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			_ = os.WriteFile(state+".owner-ready", []byte("ready"), 0600)
			for {
				time.Sleep(time.Hour)
			}
		}
		if strings.HasPrefix(mode, "recovery-") && len(os.Args) > 1 && os.Args[1] == "show" {
			if mode == "recovery-probe-hang" {
				_ = os.WriteFile(state+".probe", []byte("started"), 0600)
				time.Sleep(time.Hour)
			}
			if mode == "recovery-probe-error" {
				os.Exit(1)
			}
			fmt.Println(`{"endpoints":[{"hostId":"observed-peer"}]}`)
			os.Exit(0)
		}
		data, _ := os.ReadFile(state)
		attempt, _ := strconv.Atoi(string(data))
		attempt++
		_ = os.WriteFile(state, []byte(strconv.Itoa(attempt)), 0600)
		if mode == "tree" {
			child := exec.Command(os.Args[0], "host", "abc.usw3")
			child.Env = append(os.Environ(), "LORIS_TEST_CLI=silent", "LORIS_TEST_CLI_STATE="+state+".child-state")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			_ = os.WriteFile(state+".child", []byte(strconv.Itoa(child.Process.Pid)), 0600)
		}
		if mode == "failure" {
			fmt.Fprintln(os.Stderr, "specific CLI failure")
			os.Exit(1)
		}
		if mode == "recovery-transient" && attempt > 1 && attempt < 4 {
			fmt.Fprintln(os.Stderr, "relay network timeout")
			os.Exit(1)
		}
		if mode == "reconnect" && attempt == 2 {
			fmt.Println("Ready to accept connections for tunnel: abc.usw3")
		} else if mode != "silent" {
			if mode == "two-urls" {
				fmt.Println("Connect via browser: https://abc.usw3.devtunnels.ms:3000, https://abc-3000.usw3.devtunnels.ms")
			} else {
				fmt.Println("Connect via browser: https://abc-3000.usw3.devtunnels.ms")
			}
			fmt.Println("Ready to accept connections for tunnel: abc.usw3")
		}
		if strings.HasPrefix(mode, "recovery-") && (attempt == 1 || mode == "recovery-flap") {
			time.Sleep(70 * time.Millisecond)
			switch mode {
			case "recovery-auth":
				fmt.Fprintln(os.Stderr, "Login token expired")
			case "recovery-transient":
				fmt.Fprintln(os.Stderr, "relay network timeout")
			default:
				fmt.Fprintln(os.Stderr, "SSH host disconnected: TooManyConnections conflict")
			}
			if mode != "recovery-live" {
				os.Exit(1)
			}
		}
		if mode == "reconnect" && attempt == 1 {
			time.Sleep(50 * time.Millisecond)
			os.Exit(1)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

func lifecycleClient(t *testing.T, mode string) (*Client, string) {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "attempts")
	t.Setenv("LORIS_TEST_CLI", mode)
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("LORIS_TEST_CLI_STATE", state)
	client := NewClient(path)
	client.hostDir = t.TempDir()
	return client, state
}

func awaitCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestHostStopBeforeStart(t *testing.T) {
	client, state := lifecycleClient(t, "silent")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := host.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err == nil {
		t.Fatal("stopped host started")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("CLI was launched")
	}
}

func TestHostStopWhileStarting(t *testing.T) {
	client, state := lifecycleClient(t, "silent")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	result := make(chan error, 1)
	go func() { result <- host.Start() }()
	awaitCondition(t, func() bool { _, err := os.Stat(state); return err == nil })
	if err := host.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("canceled start reported success")
	}
	owner, err := client.claimHost("abc.usw3")
	if err != nil {
		t.Fatalf("stop retained ownership: %v", err)
	}
	owner.close()
}

func TestHostConcurrentStartAndRepeatedRestart(t *testing.T) {
	client, state := lifecycleClient(t, "ready")
	for iteration := 0; iteration < 5; iteration++ {
		host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
		var wg sync.WaitGroup
		results := make(chan error, 6)
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- host.Start() }()
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				_ = host.Stop()
				t.Fatal(err)
			}
		}
		data, _ := os.ReadFile(state)
		if string(data) != strconv.Itoa(iteration+1) {
			_ = host.Stop()
			t.Fatalf("duplicate CLI launches: %s", data)
		}
		if err := host.Stop(); err != nil {
			t.Fatal(err)
		}
		if err := host.Stop(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostReconnectRequiresFreshReadinessAndHasTimeout(t *testing.T) {
	client, state := lifecycleClient(t, "reconnect")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	host.retryDelay = 5 * time.Millisecond
	host.readyTimeout = 150 * time.Millisecond
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	awaitCondition(t, func() bool { data, _ := os.ReadFile(state); return string(data) == "3" })
	awaitCondition(t, func() bool { return host.URL() != "" })
	for _, expected := range []forward.RuntimeEventType{forward.RuntimeEventDisconnected, forward.RuntimeEventDisconnected, forward.RuntimeEventReconnected} {
		select {
		case event := <-host.Events():
			if event.Type != expected {
				t.Fatalf("event=%s, expected=%s", event.Type, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s event", expected)
		}
	}
}

func TestHostStartupFailurePreservesOutputAndReleasesOwnership(t *testing.T) {
	client, _ := lifecycleClient(t, "failure")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := host.Start(); err == nil || !strings.Contains(err.Error(), "specific CLI failure") {
		t.Fatalf("failure=%v", err)
	}
	owner, err := client.claimHost("abc.usw3")
	if err != nil {
		t.Fatal(err)
	}
	owner.close()
}

func startUntrackedCLI(t *testing.T, client *Client) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(client.Binary, "host", "abc.usw3")
	configureCommand(cmd)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killProcessTree(cmd); _ = cmd.Wait() })
	return cmd
}

func TestHostOwnershipRejectsDuplicateAndDoesNotKillUntrackedProcess(t *testing.T) {
	client, _ := lifecycleClient(t, "ready")
	external := startUntrackedCLI(t, client)
	owner, err := client.claimHost("abc.usw3")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if other, err := client.claimHost("abc.usw3"); err == nil {
		other.close()
		t.Fatal("duplicate owner accepted")
	}
	identity, err := processIdentity(external.Process.Pid)
	if err != nil || identity == "" {
		t.Fatalf("untracked process affected: %s %v", identity, err)
	}
}

func TestHostOwnershipRecoversOnlyMatchingProcessIdentity(t *testing.T) {
	for _, matches := range []bool{false, true} {
		t.Run(fmt.Sprint(matches), func(t *testing.T) {
			client, _ := lifecycleClient(t, "ready")
			child := startUntrackedCLI(t, client)
			owner, err := client.claimHost("abc.usw3")
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.track(child.Process.Pid); err != nil {
				t.Fatal(err)
			}
			if !matches {
				data, _ := json.Marshal(hostProcess{PID: child.Process.Pid, Started: "different creation time"})
				if err := conf.WritePrivateAtomic(owner.record, data); err != nil {
					t.Fatal(err)
				}
			}
			_ = owner.lock.Close() // Simulate an owner exiting without cleaning its process.
			next, err := client.claimHost("abc.usw3")
			if err != nil {
				t.Fatal(err)
			}
			defer next.close()
			if matches {
				done := make(chan error, 1)
				go func() { done <- child.Wait() }()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("recorded orphan survived recovery")
				}
			} else {
				identity, err := processIdentity(child.Process.Pid)
				if err != nil || identity == "" {
					t.Fatalf("PID identity mismatch killed an unrelated process: %v", err)
				}
			}
		})
	}
}

func TestCommandTimeoutStopsCLIProcess(t *testing.T) {
	client, _ := lifecycleClient(t, "silent")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client.run(ctx, client.Binary, "host", "abc.usw3")
	if err == nil || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout did not stop CLI: %v", err)
	}
}

func TestHostOwnershipRecoversAfterOwnerProcessCrash(t *testing.T) {
	client, state := lifecycleClient(t, "owner")
	t.Setenv("LORIS_TEST_CLI_HOST_DIR", client.hostDir)
	ownerProcess := exec.Command(client.Binary, "owner-helper")
	configureCommand(ownerProcess)
	if err := ownerProcess.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ownerProcess.Process.Kill(); _ = ownerProcess.Wait() }()
	awaitCondition(t, func() bool { _, err := os.Stat(state + ".owner-ready"); return err == nil })
	files, err := filepath.Glob(filepath.Join(client.hostDir, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("process records: %v, %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var child hostProcess
	if err := json.Unmarshal(data, &child); err != nil {
		t.Fatal(err)
	}
	defer killHostPID(child.PID)
	_ = ownerProcess.Process.Kill()
	_ = ownerProcess.Wait()
	if identity, err := processIdentity(child.PID); err != nil || (identity == "" && runtime.GOOS != "windows") {
		t.Fatalf("helper did not leave a child: %v", err)
	}
	owner, err := client.claimHost("abc.usw3")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	awaitCondition(t, func() bool { identity, err := processIdentity(child.PID); return err == nil && identity == "" })
}

func TestHostSupportsExecutablePathWithSpaces(t *testing.T) {
	client, _ := lifecycleClient(t, "ready")
	dir := filepath.Join(t.TempDir(), "CLI tools with spaces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, filepath.Base(client.Binary))
	source, err := os.Open(client.Binary)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(binary, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy executable: %v %v", copyErr, closeErr)
	}
	client.Binary = binary
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestHostStopDuringReconnectBackoff(t *testing.T) {
	client, state := lifecycleClient(t, "reconnect")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	host.retryDelay = time.Hour
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-host.Events():
		if event.Type != forward.RuntimeEventDisconnected {
			t.Fatalf("unexpected event: %s", event.Type)
		}
	case <-time.After(3 * time.Second):
		_ = host.Stop()
		t.Fatal("no disconnection")
	}
	if err := host.Stop(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(state)
	if string(data) != "1" {
		t.Fatalf("stopped host launched another CLI: %s", data)
	}
}

func TestHostStopCleansProcessTree(t *testing.T) {
	client, state := lifecycleClient(t, "tree")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state + ".child")
	if err != nil {
		_ = host.Stop()
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		_ = host.Stop()
		t.Fatal(err)
	}
	defer killHostPID(pid)
	if err := host.Stop(); err != nil {
		t.Fatal(err)
	}
	awaitCondition(t, func() bool { identity, err := processIdentity(pid); return err == nil && identity == "" })
}

func TestHostOwnershipIgnoresInvalidRecord(t *testing.T) {
	client, _ := lifecycleClient(t, "ready")
	owner, err := client.claimHost("abc.usw3")
	if err != nil {
		t.Fatal(err)
	}
	if err := conf.WritePrivateAtomic(owner.record, []byte("{broken")); err != nil {
		owner.close()
		t.Fatal(err)
	}
	_ = owner.lock.Close()
	next, err := client.claimHost("abc.usw3")
	if err != nil {
		t.Fatal(err)
	}
	next.close()
}

func TestHostPreservesBothPublicAddresses(t *testing.T) {
	client, _ := lifecycleClient(t, "two-urls")
	host := client.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	expected := "https://abc-3000.usw3.devtunnels.ms, https://abc.usw3.devtunnels.ms:3000"
	if host.URL() != expected {
		t.Fatalf("public addresses lost: %s", host.URL())
	}
}

func TestServiceHostPassesOnlyActivePortsToCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "devtunnel")
	argsFile := filepath.Join(dir, "args")
	script := `#!/bin/sh
echo "$@" > "` + argsFile + `"
id="$2"
shift 2
while [ "$#" -gt 0 ]; do
 if [ "$1" != "-p" ]; then exit 2; fi
 shift
 echo "Connect via browser: https://abc-$1.usw3.devtunnels.ms"
 shift
done
echo "Ready to accept connections for tunnel: $id"
while :; do sleep 1; done
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	host := NewClient(binary).NewServiceHost("abc.usw3", []ServiceTarget{{Host: "127.0.0.1", Port: 8080, LocalPort: 8080}, {Host: "localhost", Port: 9090, LocalPort: 9090}})
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	if len(URLsForPort(strings.Split(host.URL(), ","), 8080)) != 1 || len(URLsForPort(strings.Split(host.URL(), ","), 9090)) != 1 {
		t.Fatal(host.URL())
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(args)) != "host abc.usw3 -p 8080 -p 9090" {
		t.Fatalf("unexpected CLI port selection: %s", args)
	}
}

func TestHostFallsBackAfterBatchPortUpdateRejection(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "devtunnel")
	script := `#!/bin/sh
if [ "$1" = "show" ]; then
 echo '{"tunnelId":"abc","clusterId":"usw3","ports":[{"portNumber":8080,"protocol":"http"}]}'
 exit 0
fi
if [ "$1" = "host" ]; then
 if [ "$#" -gt 2 ]; then
  echo 'Tunnel service error: Batch update of ports is not supported.'
  exit 1
 fi
 echo 'Connect via browser: https://abc-8080.usw3.devtunnels.ms'
 echo 'Ready to accept connections for tunnel: abc.usw3'
 while :; do sleep 1; done
fi
exit 2
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	host := NewClient(binary).NewServiceHost("abc.usw3", []ServiceTarget{{Host: "127.0.0.1", Port: 8080, LocalPort: 8080}})
	host.readyTimeout = 3 * time.Second
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	if configuredPortsAreActive(Resource{PortsKnown: true, Ports: []Port{{Number: 8080}, {Number: 9090}}}, host.targets) {
		t.Fatal("inactive port allowed")
	}
}
