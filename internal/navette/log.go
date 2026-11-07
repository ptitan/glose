package navette

import "github.com/ptitan/glose/internal/journal"

// OpLog is one operation of the history, as « glose log » and the tab
// « Navette » of the interface show it.
type OpLog struct {
	Seq     int64            `json:"seq"`
	Time    string           `json:"time"`
	Actor   string           `json:"actor"`
	Kind    string           `json:"kind"`
	Files   []string         `json:"files"`
	Created []string         `json:"created"`
	Closed  []journal.Closed `json:"closed"`
	Revised []journal.Closed `json:"revised,omitempty"`
}

// TurnLog is one part of the exchange: the operations made while one actor
// had the hand, then the handover that ended it. The last part, still
// running, has no actor.
type TurnLog struct {
	Number    int            `json:"number"`
	Round     int            `json:"round"`           // the round trip (D-31): (number + 1) / 2
	Actor     string         `json:"actor,omitempty"` // who handed over; empty for the current part
	Time      string         `json:"time,omitempty"`
	Note      string         `json:"note,omitempty"`
	Reclaimed bool           `json:"reclaimed,omitempty"`
	Counts    map[string]int `json:"counts,omitempty"` // annotations handed over, by kind
	Ops       []OpLog        `json:"operations"`
}

// Log groups the effective operations of the history by part, in order.
func (h *History) Log() []TurnLog {
	var turns []TurnLog
	ti := 0
	cur := TurnLog{Number: 1, Round: 1, Ops: []OpLog{}}
	flush := func(ev *journal.Event) {
		if ev != nil {
			cur.Actor, cur.Time, cur.Note, cur.Reclaimed, cur.Counts = ev.Actor, ev.Time, ev.TurnEv.Note, ev.TurnEv.Reclaimed, ev.TurnEv.Counts
		}
		turns = append(turns, cur)
		cur = TurnLog{Number: cur.Number + 1, Round: Round(cur.Number + 1), Ops: []OpLog{}}
	}
	for _, op := range h.Ops {
		for ti < len(h.Turns) && h.Turns[ti].Seq < op.Seq {
			flush(&h.Turns[ti])
			ti++
		}
		o := OpLog{Seq: op.Seq, Time: op.Time, Actor: op.Actor, Kind: op.Kind, Created: op.Created, Closed: op.Closed, Revised: op.Revised}
		for _, f := range op.Files {
			o.Files = append(o.Files, f.Path)
		}
		cur.Ops = append(cur.Ops, o)
	}
	for ; ti < len(h.Turns); ti++ {
		flush(&h.Turns[ti])
	}
	flush(nil)
	return turns
}
