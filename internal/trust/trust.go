package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Entry struct {
	Addr        string `json:"addr"`
	Fingerprint string `json:"fingerprint"`
	Token       string `json:"token"`
}

type Store struct {
	path    string
	Entries map[string]Entry `json:"entries"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "xpctl", "known_agents.json"), nil
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, Entries: make(map[string]Entry)}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.Entries); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.Entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0600)
}

func (s *Store) Get(addr string) (Entry, bool) {
	e, ok := s.Entries[addr]
	return e, ok
}

func (s *Store) Set(e Entry) {
	s.Entries[e.Addr] = e
}
