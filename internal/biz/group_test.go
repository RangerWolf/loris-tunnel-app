package biz

import (
	"errors"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

func TestGroupCRUDAndTunnelGroupID(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	groupBiz := NewGroupBiz(storage)
	tunnelBiz := NewTunnelBiz(storage)

	group, err := groupBiz.Create(model.TunnelGroupPayload{Name: "Production"})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	jumperBiz := NewJumperBiz(storage)
	jumper, err := jumperBiz.Create(model.JumperPayload{
		Name:     "jump",
		Host:     "jump.example.com",
		Port:     22,
		User:     "root",
		AuthType: "ssh_agent",
	})
	if err != nil {
		t.Fatalf("create jumper: %v", err)
	}

	_, err = tunnelBiz.Create(model.TunnelPayload{
		Name:       "api",
		GroupID:    999,
		Mode:       "local",
		JumperIDs:  []int{jumper.ID},
		LocalHost:  "127.0.0.1",
		LocalPort:  10022,
		RemoteHost: "10.0.0.1",
		RemotePort: 22,
		Status:     "stopped",
	})
	if err == nil {
		t.Fatalf("expected invalid group id to fail")
	}

	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:       "api",
		GroupID:    group.ID,
		Mode:       "local",
		JumperIDs:  []int{jumper.ID},
		LocalHost:  "127.0.0.1",
		LocalPort:  10022,
		RemoteHost: "10.0.0.1",
		RemotePort: 22,
		Status:     "stopped",
	})
	if err != nil {
		t.Fatalf("create tunnel: %v", err)
	}
	if created.GroupID != group.ID {
		t.Fatalf("expected group id %d, got %d", group.ID, created.GroupID)
	}

	if err := groupBiz.Delete(group.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}

	tunnels, err := tunnelBiz.List()
	if err != nil {
		t.Fatalf("list tunnels: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].GroupID != 0 {
		t.Fatalf("expected tunnel group cleared, got %+v", tunnels[0])
	}
}

func TestGroupDuplicateName(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	groupBiz := NewGroupBiz(storage)

	if _, err := groupBiz.Create(model.TunnelGroupPayload{Name: "Default2"}); err != nil {
		t.Fatalf("create group: %v", err)
	}

	_, err = groupBiz.Create(model.TunnelGroupPayload{Name: "Default2"})
	if !errors.Is(err, ErrGroupNameExists) {
		t.Fatalf("expected ErrGroupNameExists, got %v", err)
	}

	_, err = groupBiz.Create(model.TunnelGroupPayload{Name: "  default2  "})
	if !errors.Is(err, ErrGroupNameExists) {
		t.Fatalf("expected duplicate case-insensitive name to fail, got %v", err)
	}

	created, err := groupBiz.Create(model.TunnelGroupPayload{Name: "Other"})
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}

	_, err = groupBiz.Update(created.ID, model.TunnelGroupPayload{Name: "Default2"})
	if !errors.Is(err, ErrGroupNameExists) {
		t.Fatalf("expected rename duplicate to fail, got %v", err)
	}

	updated, err := groupBiz.Update(created.ID, model.TunnelGroupPayload{Name: "Renamed"})
	if err != nil {
		t.Fatalf("rename group: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("expected Renamed, got %q", updated.Name)
	}
}

func TestMoveToGroup(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	groupBiz := NewGroupBiz(storage)
	tunnelBiz := NewTunnelBiz(storage)

	targetGroup, err := groupBiz.Create(model.TunnelGroupPayload{Name: "Default"})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	jumperBiz := NewJumperBiz(storage)
	jumper, err := jumperBiz.Create(model.JumperPayload{
		Name:     "jump",
		Host:     "jump.example.com",
		Port:     22,
		User:     "root",
		AuthType: "ssh_agent",
	})
	if err != nil {
		t.Fatalf("create jumper: %v", err)
	}

	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:       "api",
		Mode:       "local",
		JumperIDs:  []int{jumper.ID},
		LocalHost:  "127.0.0.1",
		LocalPort:  10022,
		RemoteHost: "10.0.0.1",
		RemotePort: 22,
		Status:     "stopped",
	})
	if err != nil {
		t.Fatalf("create tunnel: %v", err)
	}

	moved, err := tunnelBiz.MoveToGroup(created.ID, targetGroup.ID)
	if err != nil {
		t.Fatalf("move to group: %v", err)
	}
	if moved.GroupID != targetGroup.ID {
		t.Fatalf("expected group id %d, got %d", targetGroup.ID, moved.GroupID)
	}
}

func TestGroupReorder(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	groupBiz := NewGroupBiz(storage)
	first, err := groupBiz.Create(model.TunnelGroupPayload{Name: "A"})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	second, err := groupBiz.Create(model.TunnelGroupPayload{Name: "B"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	third, err := groupBiz.Create(model.TunnelGroupPayload{Name: "C"})
	if err != nil {
		t.Fatalf("create C: %v", err)
	}

	if err := groupBiz.Reorder([]int{third.ID, first.ID, second.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	groups, err := groupBiz.List()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].ID != third.ID || groups[1].ID != first.ID || groups[2].ID != second.ID {
		t.Fatalf("unexpected order: %+v", groups)
	}

	if err := groupBiz.Reorder([]int{first.ID, second.ID}); err == nil {
		t.Fatalf("expected incomplete reorder to fail")
	}
	if err := groupBiz.Reorder([]int{third.ID, first.ID, 999}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}
