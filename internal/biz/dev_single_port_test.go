package biz

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

// A stateful remote: verification must observe mutations, not canned success.
func singlePortBiz(t *testing.T, ports []devtunnel.Port, mode string) (*TunnelBiz, model.Tunnel, string, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "ports.json")
	log := filepath.Join(dir, "args.log")
	raw, _ := json.Marshal(ports)
	if err := os.WriteFile(state, raw, 0600); err != nil {
		t.Fatal(err)
	}
	script := strings.NewReplacer("STATE", strconv.Quote(state), "LOG", strconv.Quote(log), "MODE", strconv.Quote(mode)).Replace(`#!/usr/bin/env python3
import json,sys,time,os
state=STATE
mode=MODE
a=sys.argv[1:]
with open(LOG,'a') as f: f.write(' '.join(a)+'\n')
if a[0]=='--version': print('Tunnel CLI version: 1.0.2115')
elif a[0]=='user': print('{"login":"test","provider":"github"}')
elif a[0]=='access': print('[]')
elif a[0]=='show':
 if mode=='query-fail': print('network error'); sys.exit(1)
 ports=json.load(open(state))
 print(json.dumps({'tunnelId':'abc','clusterId':'usw3','ports':[{'portNumber':p['port'],'protocol':p.get('protocol','auto')} for p in ports],'allowAnonymous':True}))
elif a[0]=='port':
 ports=json.load(open(state)); port=int(a[a.index('-p')+1])
 if a[1]=='delete':
  if mode=='delete-fail': print('permission denied'); sys.exit(1)
  if mode=='block':
   open(state+'.blocked','w').close()
   while True: time.sleep(.01)
  if mode!='recreate': ports=[p for p in ports if p['port']!=port]
 elif a[1]=='create':
  if mode=='create-fail': print('permission denied'); sys.exit(1)
  ports.append({'port':port,'protocol':a[a.index('--protocol')+1]})
 with open(state,'w') as f: json.dump(ports,f)
`)
	b := newDevTunnelBiz(t, script)
	record := mustCreateBoundDevTunnel(t, b, "devspace", "abc.usw3", 7676, "auto")
	b.newDevServiceHost = func(_ string, targets []devtunnel.ServiceTarget) devServiceHost {
		return &fakeDevServiceHost{targets: targets, done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 16)}
	}
	return b, record, state, log
}

func remotePorts(t *testing.T, path string) []devtunnel.Port {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ports []devtunnel.Port
	if err = json.Unmarshal(raw, &ports); err != nil {
		t.Fatal(err)
	}
	return ports
}

func TestSinglePortReconcilesStaleBindingAndStarts(t *testing.T) {
	for _, ports := range [][]devtunnel.Port{
		{{Number: 7676, Protocol: "auto"}, {Number: 8080, Protocol: "auto"}},
		{{Number: 8080, Protocol: "auto"}},
		{{Number: 7676, Protocol: "auto"}},
	} {
		t.Run(fmt.Sprint(ports), func(t *testing.T) {
			b, record, state, log := singlePortBiz(t, ports, "")
			defer b.Shutdown()
			setDevBinding(t, b, record.ID, "devspace", "abc.usw3", 7676, 8080)
			waitDevTunnelRunning(t, b, record.ID)
			got := remotePorts(t, state)
			if len(got) != 1 || got[0].Number != 7676 {
				t.Fatalf("ports: %+v", got)
			}
			saved, _ := b.tunnelByID(record.ID)
			if saved.DevTunnel.TunnelPort != 7676 {
				t.Fatal(saved.DevTunnel)
			}
			args, _ := os.ReadFile(log)
			if strings.Contains(string(args), "-p 8080 --protocol") {
				t.Fatal("recreated stale port", string(args))
			}
			if len(ports) == 1 && ports[0].Number == 7676 && strings.Contains(string(args), "port ") {
				t.Fatal("unchanged ports mutated", string(args))
			}
			if !strings.Contains(saved.PublicURL, "7676") {
				listed, _ := b.List()
				if !strings.Contains(listed[0].PublicURL, "7676") {
					t.Fatal(listed)
				}
			}
		})
	}
}

func TestSinglePortFailuresNeverStartHost(t *testing.T) {
	for _, mode := range []string{"query-fail", "delete-fail", "recreate", "create-fail"} {
		t.Run(mode, func(t *testing.T) {
			b, record, _, log := singlePortBiz(t, []devtunnel.Port{{Number: 8080, Protocol: "auto"}}, mode)
			defer b.Shutdown()
			hosts := 0
			b.newDevServiceHost = func(string, []devtunnel.ServiceTarget) devServiceHost {
				hosts++
				t.Error("host started after failed reconciliation")
				return nil
			}
			_, err := b.Toggle(record.ID, func() int { return 0 })
			if err != nil {
				t.Fatal(err)
			}
			waitDevTunnelStatus(t, b, record.ID, "error")
			b.startWG.Wait()
			if hosts != 0 {
				t.Fatal(hosts)
			}
			if mode == "query-fail" {
				args, _ := os.ReadFile(log)
				if strings.Contains(string(args), "port delete") {
					t.Fatal("deleted after failed lookup")
				}
			}
		})
	}
}

func TestSinglePortProtocolMismatchDoesNotDeletePorts(t *testing.T) {
	b, record, state, log := singlePortBiz(t, []devtunnel.Port{{Number: 7676, Protocol: "https"}, {Number: 8080, Protocol: "auto"}}, "")
	defer b.Shutdown()
	_, err := b.storage.Update(func(c *conf.Config) error {
		c.Tunnels[0].Mode = "http"
		c.Tunnels[0].DevTunnel.Protocol = "http"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = b.Toggle(record.ID, func() int { return 0 })
	waitDevTunnelStatus(t, b, record.ID, "error")
	args, _ := os.ReadFile(log)
	if strings.Contains(string(args), "port delete") || len(remotePorts(t, state)) != 2 {
		t.Fatal("protocol mismatch mutated ports", string(args))
	}
}

func TestSinglePortSaveClearsOnlyWhenPortChanges(t *testing.T) {
	b, record, _, log := singlePortBiz(t, nil, "")
	setDevBinding(t, b, record.ID, "devspace", "abc.usw3", 8080, 8080)
	_, err := b.storage.Update(func(c *conf.Config) error {
		c.Tunnels[0].DevTunnel.LastPublicURL = "https://abc-8080.usw3.devtunnels.ms"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p := model.TunnelPayload{Name: "renamed", Provider: model.ProviderDevTunnel, Mode: "auto", LocalHost: "localhost", LocalPort: 8080, Status: "stopped", DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{TunnelID: "abc.usw3", Access: "private"}}
	updated, err := b.Update(record.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DevTunnel.TunnelPort != 8080 || updated.DevTunnel.LastPublicURL == "" {
		t.Fatal("metadata edit lost public address")
	}
	b.mu.Lock()
	b.knownURL[record.ID] = "old"
	b.mu.Unlock()
	p.LocalPort = 7676
	updated, err = b.Update(record.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DevTunnel.TunnelPort != 0 || updated.DevTunnel.LastPublicURL != "" {
		t.Fatal(updated.DevTunnel)
	}
	b.mu.Lock()
	_, cached := b.knownURL[record.ID]
	b.mu.Unlock()
	if cached {
		t.Fatal("stale runtime URL")
	}
	if args, _ := os.ReadFile(log); len(args) > 0 {
		t.Fatal("save contacted remote", string(args))
	}
}

func TestSinglePortLegacyDuplicatesNeverTouchRemote(t *testing.T) {
	b, first, _, log := singlePortBiz(t, nil, "")
	second := mustCreateDevTunnel(t, b)
	setDevBinding(t, b, second.ID, "legacy", "ABC", 9090, 9090)
	_, err := b.storage.Update(func(c *conf.Config) error {
		for i := range c.Tunnels {
			c.Tunnels[i].AutoStart = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.StartAutoStart(0); err != nil {
		t.Fatal(err)
	}
	b.startWG.Wait()
	for _, id := range []int{first.ID, second.ID} {
		waitDevTunnelStatus(t, b, id, "error")
	}
	_, _ = b.Toggle(first.ID, func() int { return 0 })
	if args, _ := os.ReadFile(log); len(args) > 0 {
		t.Fatal("conflict contacted remote", string(args))
	}
	cfg, _ := b.storage.Load()
	if len(cfg.Tunnels) != 2 {
		t.Fatal("legacy record deleted")
	}
}

func TestSinglePortCancellationAndConcurrentEdit(t *testing.T) {
	b, record, state, _ := singlePortBiz(t, []devtunnel.Port{{Number: 8080, Protocol: "auto"}}, "block")
	defer b.Shutdown()
	_, err := b.Toggle(record.ID, func() int { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		if _, err := os.Stat(state + ".blocked"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p := model.TunnelPayload{Name: "changed", Provider: model.ProviderDevTunnel, Mode: "auto", LocalHost: "127.0.0.1", LocalPort: 9000, Status: "stopped", DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{TunnelID: "abc.usw3", Access: "private"}}
	if _, err := b.Update(record.ID, p); err == nil {
		t.Fatal("edit accepted during startup")
	}
	if _, err := b.beginAsyncStart(record, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Toggle(record.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { b.startWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not interrupt cleanup")
	}
	saved, _ := b.tunnelByID(record.ID)
	if saved.LocalPort != 7676 || saved.Status != "stopped" {
		t.Fatal(saved)
	}
}

func TestSinglePortBridgeAllocationAvoidsOccupiedLocalPort(t *testing.T) {
	b := NewTunnelBiz(nil)
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	local := occupied.Addr().(*net.TCPAddr).Port
	public, err := b.allocateDevTunnelPort("192.0.2.1", local)
	if err != nil {
		t.Fatal(err)
	}
	if public == local || public == 0 {
		t.Fatal("occupied bridge port reused")
	}
	direct, err := b.allocateDevTunnelPort("127.0.0.1", local)
	if err != nil || direct != local {
		t.Fatal("direct service port changed")
	}
}
