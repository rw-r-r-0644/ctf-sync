package jeopardy

import "fmt"

// SettingDef describes a backend setting.
type SettingDef struct {
	ID       string
	Name     string
	Required bool
}

// BackendDef describes an available backend type.
type BackendDef struct {
	ID       string
	Name     string
	Settings []SettingDef
	Build    func(settings map[string]string) (Backend, error)
}

var registry []BackendDef

// Register adds a backend definition to the registry.
// Called from init() in backend implementation files.
func Register(b BackendDef) {
	registry = append(registry, b)
}

// Backends returns all registered backend definitions.
func Backends() []BackendDef {
	return registry
}

// Build creates a Backend from a backend ID and settings.
func Build(id string, settings map[string]string) (Backend, error) {
	for _, b := range registry {
		if b.ID == id {
			for _, s := range b.Settings {
				if s.Required && settings[s.ID] == "" {
					return nil, fmt.Errorf("%s is required", s.Name)
				}
			}
			return b.Build(settings)
		}
	}
	return nil, fmt.Errorf("unknown backend: %s", id)
}
