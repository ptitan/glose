package journal

import "github.com/ptitan/glose/internal/agents"

// TaskInfo is the immutable task selected by the author at this handover.
// Warnings distinguish requested grants from controls the CLI cannot enforce.
type TaskInfo struct {
	Icon            string        `json:"icon,omitempty"`
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Agent           agents.Alias  `json:"agent"`
	Scope           string        `json:"scope"`
	Files           []string      `json:"files"`
	Access          agents.Access `json:"access"`
	Warnings        []string      `json:"warnings,omitempty"`
	Workdir         string        `json:"workdir"`
	MaxMinutes      int           `json:"maxMinutes"`
	Prompt          string        `json:"prompt"`
	WhenAnnotations string        `json:"whenAnnotations"`
}
