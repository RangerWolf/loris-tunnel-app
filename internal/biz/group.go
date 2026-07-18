package biz

import (
	"errors"
	"fmt"
	"strings"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

var (
	ErrGroupNotFound   = errors.New("group not found")
	ErrGroupNameExists = errors.New("group name already exists")
)

type GroupBiz struct {
	storage *conf.Storage
}

func NewGroupBiz(storage *conf.Storage) *GroupBiz {
	return &GroupBiz{storage: storage}
}

func (b *GroupBiz) List() ([]model.TunnelGroup, error) {
	cfg, err := b.storage.Load()
	if err != nil {
		return nil, err
	}

	items := append([]model.TunnelGroup{}, cfg.Groups...)
	return items, nil
}

func (b *GroupBiz) Create(payload model.TunnelGroupPayload) (model.TunnelGroup, error) {
	payload = normalizeGroupPayload(payload)
	if err := validateGroupPayload(payload); err != nil {
		return model.TunnelGroup{}, err
	}

	var created model.TunnelGroup
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if groupNameTaken(cfg.Groups, payload.Name, 0) {
			return ErrGroupNameExists
		}

		created = model.TunnelGroup{
			ID:   nextGroupID(cfg.Groups),
			Name: payload.Name,
		}
		cfg.Groups = append(cfg.Groups, created)
		return nil
	})
	if err != nil {
		return model.TunnelGroup{}, err
	}

	return created, nil
}

func (b *GroupBiz) Update(id int, payload model.TunnelGroupPayload) (model.TunnelGroup, error) {
	if id <= 0 {
		return model.TunnelGroup{}, fmt.Errorf("invalid group id")
	}

	payload = normalizeGroupPayload(payload)
	if err := validateGroupPayload(payload); err != nil {
		return model.TunnelGroup{}, err
	}

	var updated model.TunnelGroup
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Groups {
			if cfg.Groups[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrGroupNotFound
		}
		if groupNameTaken(cfg.Groups, payload.Name, id) {
			return ErrGroupNameExists
		}

		updated = model.TunnelGroup{
			ID:   id,
			Name: payload.Name,
		}
		cfg.Groups[idx] = updated
		return nil
	})
	if err != nil {
		return model.TunnelGroup{}, err
	}

	return updated, nil
}

func (b *GroupBiz) Delete(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid group id")
	}

	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Groups {
			if cfg.Groups[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrGroupNotFound
		}

		cfg.Groups = append(cfg.Groups[:idx], cfg.Groups[idx+1:]...)
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].GroupID == id {
				cfg.Tunnels[i].GroupID = 0
			}
		}
		return nil
	})
	return err
}

func (b *GroupBiz) Reorder(ids []int) error {
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if len(ids) != len(cfg.Groups) {
			return fmt.Errorf("group order must include all groups")
		}
		if len(ids) == 0 {
			return nil
		}

		byID := make(map[int]model.TunnelGroup, len(cfg.Groups))
		for _, group := range cfg.Groups {
			byID[group.ID] = group
		}

		seen := make(map[int]struct{}, len(ids))
		next := make([]model.TunnelGroup, 0, len(ids))
		for _, id := range ids {
			if id <= 0 {
				return fmt.Errorf("invalid group id")
			}
			group, ok := byID[id]
			if !ok {
				return ErrGroupNotFound
			}
			if _, exists := seen[id]; exists {
				return fmt.Errorf("duplicate group id in order")
			}
			seen[id] = struct{}{}
			next = append(next, group)
		}

		cfg.Groups = next
		return nil
	})
	return err
}

func normalizeGroupPayload(payload model.TunnelGroupPayload) model.TunnelGroupPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	return payload
}

func validateGroupPayload(payload model.TunnelGroupPayload) error {
	if payload.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func groupNameTaken(groups []model.TunnelGroup, name string, excludeID int) bool {
	normalized := strings.TrimSpace(name)
	for _, group := range groups {
		if excludeID > 0 && group.ID == excludeID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(group.Name), normalized) {
			return true
		}
	}
	return false
}

func findGroupByID(items []model.TunnelGroup, id int) (model.TunnelGroup, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.TunnelGroup{}, false
}

func validateGroupID(groups []model.TunnelGroup, groupID int) error {
	if groupID <= 0 {
		return nil
	}
	if _, ok := findGroupByID(groups, groupID); !ok {
		return ErrGroupNotFound
	}
	return nil
}

func nextGroupID(items []model.TunnelGroup) int {
	next := 1
	for _, item := range items {
		if item.ID >= next {
			next = item.ID + 1
		}
	}
	return next
}
