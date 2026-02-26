package biz

import (
	"errors"
	"fmt"
	"strings"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

var (
	ErrJumperNotFound = errors.New("jumper not found")
	ErrJumperInUse    = errors.New("jumper is used by existing tunnels")
)

const (
	defaultKeepAliveIntervalMs = 5000
	minKeepAliveIntervalMs     = 1000
	maxKeepAliveIntervalMs     = 120000
)

type JumperBiz struct {
	storage *conf.Storage
}

func NewJumperBiz(storage *conf.Storage) *JumperBiz {
	return &JumperBiz{storage: storage}
}

func (b *JumperBiz) List() ([]model.Jumper, error) {
	cfg, err := b.storage.Load()
	if err != nil {
		return nil, err
	}

	items := append([]model.Jumper{}, cfg.Jumpers...)
	return items, nil
}

func (b *JumperBiz) Create(payload model.JumperPayload) (model.Jumper, error) {
	payload = normalizeJumperPayload(payload)
	if err := validateJumperPayload(payload); err != nil {
		return model.Jumper{}, err
	}

	var created model.Jumper
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		created = model.Jumper{
			ID:                     nextJumperID(cfg.Jumpers),
			Name:                   payload.Name,
			Host:                   payload.Host,
			Port:                   payload.Port,
			User:                   payload.User,
			AuthType:               payload.AuthType,
			KeyPath:                payload.KeyPath,
			AgentSocketPath:        payload.AgentSocketPath,
			Password:               payload.Password,
			BypassHostVerification: payload.BypassHostVerification,
			KeepAliveIntervalMs:    payload.KeepAliveIntervalMs,
			TimeoutMs:              payload.TimeoutMs,
			HostKeyAlgorithms:      payload.HostKeyAlgorithms,
			Notes:                  payload.Notes,
		}
		cfg.Jumpers = append(cfg.Jumpers, created)
		return nil
	})
	if err != nil {
		return model.Jumper{}, err
	}

	return created, nil
}

func (b *JumperBiz) Update(id int, payload model.JumperPayload) (model.Jumper, error) {
	if id <= 0 {
		return model.Jumper{}, fmt.Errorf("invalid jumper id")
	}

	payload = normalizeJumperPayload(payload)
	if err := validateJumperPayload(payload); err != nil {
		return model.Jumper{}, err
	}

	var updated model.Jumper
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Jumpers {
			if cfg.Jumpers[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrJumperNotFound
		}

		updated = model.Jumper{
			ID:                     id,
			Name:                   payload.Name,
			Host:                   payload.Host,
			Port:                   payload.Port,
			User:                   payload.User,
			AuthType:               payload.AuthType,
			KeyPath:                payload.KeyPath,
			AgentSocketPath:        payload.AgentSocketPath,
			Password:               payload.Password,
			BypassHostVerification: payload.BypassHostVerification,
			KeepAliveIntervalMs:    payload.KeepAliveIntervalMs,
			TimeoutMs:              payload.TimeoutMs,
			HostKeyAlgorithms:      payload.HostKeyAlgorithms,
			Notes:                  payload.Notes,
		}
		cfg.Jumpers[idx] = updated
		return nil
	})
	if err != nil {
		return model.Jumper{}, err
	}

	return updated, nil
}

func (b *JumperBiz) Delete(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid jumper id")
	}

	_, err := b.storage.Update(func(cfg *conf.Config) error {
		for _, tunnel := range cfg.Tunnels {
			for _, jid := range tunnel.JumperIDs {
				if jid == id {
					return ErrJumperInUse
				}
			}
		}

		idx := -1
		for i := range cfg.Jumpers {
			if cfg.Jumpers[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrJumperNotFound
		}

		cfg.Jumpers = append(cfg.Jumpers[:idx], cfg.Jumpers[idx+1:]...)
		return nil
	})
	return err
}

func (b *JumperBiz) TestConnection(payload model.JumperPayload) error {
	payload = normalizeJumperPayload(payload)
	if err := validateJumperPayload(payload); err != nil {
		return err
	}

	j := model.Jumper{
		Name:                   payload.Name,
		Host:                   payload.Host,
		Port:                   payload.Port,
		User:                   payload.User,
		AuthType:               payload.AuthType,
		KeyPath:                payload.KeyPath,
		AgentSocketPath:        payload.AgentSocketPath,
		Password:               payload.Password,
		BypassHostVerification: payload.BypassHostVerification,
		KeepAliveIntervalMs:    payload.KeepAliveIntervalMs,
		TimeoutMs:              payload.TimeoutMs,
		HostKeyAlgorithms:      payload.HostKeyAlgorithms,
		Notes:                  payload.Notes,
	}

	return forward.TestJumperConnection(j)
}

func normalizeJumperPayload(payload model.JumperPayload) model.JumperPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Host = strings.TrimSpace(payload.Host)
	payload.User = strings.TrimSpace(payload.User)
	payload.AuthType = strings.TrimSpace(payload.AuthType)
	payload.KeyPath = strings.TrimSpace(payload.KeyPath)
	payload.AgentSocketPath = strings.TrimSpace(payload.AgentSocketPath)
	payload.HostKeyAlgorithms = strings.TrimSpace(payload.HostKeyAlgorithms)
	payload.Notes = strings.TrimSpace(payload.Notes)

	if payload.Port <= 0 {
		payload.Port = 22
	}
	if payload.TimeoutMs <= 0 {
		payload.TimeoutMs = 5000
	}
	if payload.KeepAliveIntervalMs < 0 {
		payload.KeepAliveIntervalMs = defaultKeepAliveIntervalMs
	}
	if payload.AuthType == "" {
		payload.AuthType = "ssh_key"
	}
	if payload.AuthType != "ssh_key" {
		payload.KeyPath = ""
	}
	if payload.AuthType == "ssh_agent" {
		payload.Password = ""
	}

	return payload
}

func validateJumperPayload(payload model.JumperPayload) error {
	if payload.Name == "" {
		return fmt.Errorf("name is required")
	}
	if payload.Host == "" {
		return fmt.Errorf("host is required")
	}
	if payload.User == "" {
		return fmt.Errorf("user is required")
	}
	if payload.Port < 1 || payload.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if payload.TimeoutMs < 100 || payload.TimeoutMs > 120000 {
		return fmt.Errorf("timeoutMs must be between 100 and 120000")
	}
	if payload.KeepAliveIntervalMs > maxKeepAliveIntervalMs {
		return fmt.Errorf("keepAliveIntervalMs must be 0 (disable) or between %d and %d", minKeepAliveIntervalMs, maxKeepAliveIntervalMs)
	}
	if payload.KeepAliveIntervalMs > 0 && payload.KeepAliveIntervalMs < minKeepAliveIntervalMs {
		return fmt.Errorf("keepAliveIntervalMs must be 0 (disable) or between %d and %d", minKeepAliveIntervalMs, maxKeepAliveIntervalMs)
	}
	switch payload.AuthType {
	case "password":
		if strings.TrimSpace(payload.Password) == "" {
			return fmt.Errorf("password auth requires password")
		}
	case "ssh_key":
		if payload.KeyPath == "" {
			return fmt.Errorf("ssh_key auth requires keyPath")
		}
	case "ssh_agent":
	default:
		return fmt.Errorf("unsupported authType: %s", payload.AuthType)
	}
	return nil
}

func nextJumperID(items []model.Jumper) int {
	next := 1
	for _, item := range items {
		if item.ID >= next {
			next = item.ID + 1
		}
	}
	return next
}
