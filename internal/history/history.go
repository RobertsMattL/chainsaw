package history

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const Max = 50

type History struct {
	path    string
	Entries []string
}

func Load() *History {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	path := filepath.Join(dir, "chainsaw", "search_history.json")
	h := &History{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	_ = json.Unmarshal(data, &h.Entries)
	return h
}

func (h *History) Add(s string) {
	if s == "" {
		return
	}
	out := h.Entries[:0]
	for _, e := range h.Entries {
		if e != s {
			out = append(out, e)
		}
	}
	h.Entries = append([]string{s}, out...)
	if len(h.Entries) > Max {
		h.Entries = h.Entries[:Max]
	}
	data, err := json.Marshal(h.Entries)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(h.path), 0755)
	os.WriteFile(h.path, data, 0644)
}
