package progress

import "time"

type Phase string

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

type Event struct {
	Issue           int
	Phase           Phase
	Detail          string
	Outcome         string
	URL             string
	Harness         string
	BuilderHarness  string
	ReviewerHarness string
	At              time.Time
	Terminal        bool
}

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
