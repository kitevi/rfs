package rfs

import (
	"context"
	"sync"
	"time"
)

type DomainGateConfig struct {
	MinSpacing     time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type GateClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type DomainGate struct {
	mu     sync.Mutex
	clock  GateClock
	config DomainGateConfig
	states map[string]*domainGateState
}

type domainGateState struct {
	// lane serializes whole polls for one domain. It is separate from lock so a
	// running poll can book follow-up slots (Acquire) without deadlocking.
	lane sync.Mutex
	// lock guards the spacing fields below.
	lock        sync.Mutex
	nextAllowed time.Time
	backoff     time.Duration
}

func NewDomainGate(clock GateClock, config DomainGateConfig) *DomainGate {
	if clock == nil {
		clock = realGateClock{}
	}
	if config.InitialBackoff == 0 {
		config.InitialBackoff = time.Minute
	}
	if config.MaxBackoff == 0 {
		config.MaxBackoff = time.Hour
	}
	return &DomainGate{
		clock:  clock,
		config: config,
		states: map[string]*domainGateState{},
	}
}

// Run waits for the domain gate, runs action while holding that domain's lane,
// and records success/throttle state from the PollResult. Calls for different
// domains can run independently; calls for the same domain are serialized.
func (g *DomainGate) Run(ctx context.Context, domain string, action func() (PollResult, error)) (PollResult, error) {
	state := g.stateFor(domain)
	state.lane.Lock()
	defer state.lane.Unlock()

	if err := g.waitForSlot(ctx, state); err != nil {
		return PollResult{}, err
	}

	result, err := action()
	if err != nil {
		// A failure is rfs's problem (a layout change, a decode error, a broken
		// page), not upstream asking for less traffic. Applying the same
		// spacing a success gets keeps the domain polite without inflating the
		// throttle backoff, which would delay the retry that fixes it.
		g.recordFailure(state)
		return result, err
	}
	if result.Status == PollThrottled {
		g.recordThrottle(state, result.RetryAfter)
		return result, nil
	}
	g.recordSuccess(state)
	return result, nil
}

// Acquire blocks until the domain's next allowed instant and books that slot,
// so consecutive follow-up requests to one domain stay spaced even though they
// are not whole polls. It takes no lane, so it is safe inside a Run action.
func (g *DomainGate) Acquire(ctx context.Context, domain string) error {
	state := g.stateFor(domain)
	state.lock.Lock()
	defer state.lock.Unlock()

	if err := g.waitLocked(ctx, state); err != nil {
		return err
	}
	booked := g.clock.Now().Add(g.config.MinSpacing)
	if booked.After(state.nextAllowed) {
		state.nextAllowed = booked
	}
	return nil
}

// waitForSlot waits without holding the lane's action lock, so Acquire and the
// record helpers can take the spacing lock in turn.
func (g *DomainGate) waitForSlot(ctx context.Context, state *domainGateState) error {
	state.lock.Lock()
	defer state.lock.Unlock()
	return g.waitLocked(ctx, state)
}

func (g *DomainGate) waitLocked(ctx context.Context, state *domainGateState) error {
	for {
		delay := state.nextAllowed.Sub(g.clock.Now())
		if delay <= 0 {
			return nil
		}
		if err := g.clock.Sleep(ctx, delay); err != nil {
			return err
		}
	}
}

// recordFailure books the normal spacing after a failed call and leaves the
// throttle backoff untouched, so only real throttling escalates it.
func (g *DomainGate) recordFailure(state *domainGateState) {
	state.lock.Lock()
	defer state.lock.Unlock()

	nextAllowed := g.clock.Now().Add(g.config.MinSpacing)
	if nextAllowed.After(state.nextAllowed) {
		state.nextAllowed = nextAllowed
	}
}

func (g *DomainGate) recordSuccess(state *domainGateState) {
	state.lock.Lock()
	defer state.lock.Unlock()

	state.backoff = 0
	nextAllowed := g.clock.Now().Add(g.config.MinSpacing)
	if nextAllowed.After(state.nextAllowed) {
		state.nextAllowed = nextAllowed
	}
}

func (g *DomainGate) recordThrottle(state *domainGateState, retryAfter time.Duration) {
	state.lock.Lock()
	defer state.lock.Unlock()

	delay := retryAfter
	if delay <= 0 {
		delay = g.nextBackoff(state.backoff)
	}
	state.backoff = delay
	nextAllowed := g.clock.Now().Add(delay)
	if nextAllowed.After(state.nextAllowed) {
		state.nextAllowed = nextAllowed
	}
}

func (g *DomainGate) stateFor(domain string) *domainGateState {
	g.mu.Lock()
	defer g.mu.Unlock()

	state, ok := g.states[domain]
	if !ok {
		state = &domainGateState{}
		g.states[domain] = state
	}
	return state
}

func (g *DomainGate) nextBackoff(previous time.Duration) time.Duration {
	if previous <= 0 {
		return g.config.InitialBackoff
	}
	next := previous * 2
	if next > g.config.MaxBackoff {
		return g.config.MaxBackoff
	}
	return next
}

type realGateClock struct{}

func (realGateClock) Now() time.Time { return time.Now().UTC() }

func (realGateClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
