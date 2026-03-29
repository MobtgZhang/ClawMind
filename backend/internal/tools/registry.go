package tools

import (
	"encoding/json"
	"os"
)

// Definition matches OpenAI tools JSON shape (subset).
type Definition struct {
	Type     string `json:"type"`
	Function FunctionSpec `json:"function"`
}

type FunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Registry loads tool definitions from JSON file.
type Registry struct {
	Tools []Definition `json:"tools"`
}

// Load reads path; missing file returns empty registry.
func Load(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{}, nil
		}
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
