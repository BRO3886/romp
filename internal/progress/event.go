package progress

import (
	"github.com/BRO3886/romp/internal/protocol"
	"time"
)

type Phase = string

const (
	PhaseClaiming    Phase = "claiming"
	PhasePreparing   Phase = "preparing"
	PhaseAgent       Phase = "agent"
	PhaseVerifying   Phase = "verifying"
	PhasePublishing  Phase = "publishing"
	PhaseReviewing   Phase = "reviewing"
	PhaseFixing      Phase = "fixing"
	PhaseReverifying Phase = "re-verifying"
	PhaseRereviewing Phase = "re-reviewing"
	PhaseDone        Phase = "done"
	PhaseFailed      Phase = "failed"
)

type Event = protocol.Event

type Sink func(Event)

func (s Sink) Emit(event Event) {
	if s == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	s(event)
}
