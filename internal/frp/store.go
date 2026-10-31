package frp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"loris-tunnel/internal/conf"
)

type Store struct {
	mu   sync.Mutex
	Path string
}
type Settings struct {
	Binary  string   `json:"binary"`
	Servers []Server `json:"servers"`
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (s *Store) load() (Settings, error) {
	cfg := Settings{Servers: []Server{}}
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}
func (s *Store) save(cfg Settings) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return conf.WritePrivateAtomic(s.Path, data)
}
func (s *Store) Settings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.load()
	for i := range cfg.Servers {
		cfg.Servers[i].HasToken = cfg.Servers[i].Token != ""
		cfg.Servers[i].Token = ""
	}
	return cfg, err
}
func (s *Store) Server(id string) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.load()
	if err != nil {
		return Server{}, err
	}
	for _, server := range cfg.Servers {
		if server.ID == id {
			return server, nil
		}
	}
	return Server{}, fmt.Errorf("FRP server credentials are missing on this device; configure it in Providers")
}
func (s *Store) SaveServer(server Server) (Server, error) {
	if err := server.Normalize(); err != nil {
		return Server{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.load()
	if err != nil {
		return Server{}, err
	}
	for _, old := range cfg.Servers {
		if old.ID != server.ID && old.Name == server.Name {
			return Server{}, fmt.Errorf("FRP server name already exists")
		}
	}
	found := false
	for i, old := range cfg.Servers {
		if old.ID == server.ID {
			if server.Token == "" && !server.ClearToken {
				server.Token = old.Token
			}
			if server.ClearToken {
				server.Token = ""
			}
			server.ClearToken = false
			cfg.Servers[i] = server
			found = true
			break
		}
	}
	if !found {
		if server.ID != "" {
			return Server{}, fmt.Errorf("FRP server no longer exists")
		}
		server.ID = randomID()
		cfg.Servers = append(cfg.Servers, server)
	}
	err = s.save(cfg)
	server.HasToken = server.Token != ""
	server.Token = ""
	return server, err
}
func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.load()
	if err != nil {
		return err
	}
	for i, server := range cfg.Servers {
		if server.ID == id {
			cfg.Servers = append(cfg.Servers[:i], cfg.Servers[i+1:]...)
			return s.save(cfg)
		}
	}
	return fmt.Errorf("FRP server not found")
}

// MatchImport matches endpoints locally; existing server settings remain unchanged.
func (s *Store) MatchImport(server Server) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.load()
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, old := range cfg.Servers {
		if strings.EqualFold(old.Host, server.Host) && old.Port == server.Port {
			ids = append(ids, old.ID)
		}
	}
	return ids, nil
}

func (s *Store) SetBinary(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.load()
	if err != nil {
		return err
	}
	cfg.Binary = path
	return s.save(cfg)
}
