package server

import "github.com/ptitan/glose/internal/navette"

type missionLabel struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

func missionLabels(h *navette.History) map[string]missionLabel {
	out := map[string]missionLabel{}
	for _, ev := range h.Turns {
		if t := ev.TurnEv.Task; t != nil {
			icon := t.Icon
			if icon == "" {
				icon = "agent"
			}
			out[t.ID] = missionLabel{t.Name, icon}
		}
	}
	return out
}
