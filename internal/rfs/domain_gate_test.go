package rfs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDomainGateRunSpacesSameDomainAndLeavesOtherDomainsIndependent(t *testing.T) {
	clock := &fakeSleepClock{now: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	gate := NewDomainGate(clock, DomainGateConfig{MinSpacing: 10 * time.Second})

	if _, err := gate.Run(context.Background(), "en.wikipedia.org", successfulPoll); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("first same-domain request should not sleep: %v", clock.sleeps)
	}

	if _, err := gate.Run(context.Background(), "example.com", successfulPoll); err != nil {
		t.Fatalf("other-domain run: %v", err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("other domain should not inherit spacing: %v", clock.sleeps)
	}

	if _, err := gate.Run(context.Background(), "en.wikipedia.org", successfulPoll); err != nil {
		t.Fatalf("second same-domain run: %v", err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 10*time.Second {
		t.Fatalf("same domain was not spaced: %v", clock.sleeps)
	}
}

func TestDomainGateRunBacksOffDomainAfterThrottle(t *testing.T) {
	clock := &fakeSleepClock{now: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	gate := NewDomainGate(clock, DomainGateConfig{MinSpacing: 10 * time.Second, InitialBackoff: time.Minute, MaxBackoff: time.Hour})

	if _, err := gate.Run(context.Background(), "en.wikipedia.org", func() (PollResult, error) {
		return PollResult{Status: PollThrottled, RetryAfter: 2 * time.Minute}, nil
	}); err != nil {
		t.Fatalf("throttled run: %v", err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("first run should not sleep before receiving throttle: %v", clock.sleeps)
	}

	if _, err := gate.Run(context.Background(), "en.wikipedia.org", successfulPoll); err != nil {
		t.Fatalf("wait after throttle: %v", err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 2*time.Minute {
		t.Fatalf("Retry-After was not honored: %v", clock.sleeps)
	}

	if _, err := gate.Run(context.Background(), "example.com", successfulPoll); err != nil {
		t.Fatalf("other-domain run: %v", err)
	}
	if len(clock.sleeps) != 1 {
		t.Fatalf("other domain should not be throttled: %v", clock.sleeps)
	}
}

func TestDomainGateDoesNotBackOffAfterActionFailures(t *testing.T) {
	clock := &fakeSleepClock{now: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	gate := NewDomainGate(clock, DomainGateConfig{MinSpacing: 10 * time.Second, InitialBackoff: time.Minute, MaxBackoff: time.Hour})
	failing := func() (PollResult, error) { return PollResult{}, errors.New("page layout changed") }

	for range 2 {
		if _, err := gate.Run(context.Background(), "example.com", failing); err == nil {
			t.Fatal("failing action did not return its error")
		}
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 10*time.Second {
		t.Fatalf("failures slept %v, want one MinSpacing wait: a poll error is not upstream throttling", clock.sleeps)
	}

	throttle := func() (PollResult, error) { return PollResult{Status: PollThrottled}, nil }
	if _, err := gate.Run(context.Background(), "example.com", throttle); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Run(context.Background(), "example.com", successfulPoll); err != nil {
		t.Fatal(err)
	}
	if got := clock.sleeps[len(clock.sleeps)-1]; got != time.Minute {
		t.Fatalf("first real throttle waited %s, want the InitialBackoff 1m: earlier failures inflated the backoff", got)
	}
}

func TestDomainGateAcquireBooksADomainSlotForFollowUpRequests(t *testing.T) {
	clock := &fakeSleepClock{now: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	gate := NewDomainGate(clock, DomainGateConfig{MinSpacing: 10 * time.Second})

	if err := gate.Acquire(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("first slot should be free: %v", clock.sleeps)
	}
	if err := gate.Acquire(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 10*time.Second {
		t.Fatalf("consecutive same-domain slots slept %v, want one MinSpacing wait", clock.sleeps)
	}
	if err := gate.Acquire(context.Background(), "other.test"); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 {
		t.Fatalf("another domain inherited spacing: %v", clock.sleeps)
	}
}

func TestDomainGateRunHonorsSlotsBookedByFollowUpRequests(t *testing.T) {
	clock := &fakeSleepClock{now: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	gate := NewDomainGate(clock, DomainGateConfig{MinSpacing: 10 * time.Second})

	if _, err := gate.Run(context.Background(), "example.com", func() (PollResult, error) {
		// A follow-up request inside the poll books the next slot.
		if err := gate.Acquire(context.Background(), "example.com"); err != nil {
			return PollResult{}, err
		}
		return PollResult{Status: PollUpdated}, nil
	}); err != nil {
		t.Fatal(err)
	}
	clock.sleeps = nil
	if _, err := gate.Run(context.Background(), "example.com", successfulPoll); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 10*time.Second {
		t.Fatalf("poll after a booked slot slept %v, want the booked MinSpacing", clock.sleeps)
	}
}

func successfulPoll() (PollResult, error) {
	return PollResult{Status: PollUpdated}, nil
}

type fakeSleepClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeSleepClock) Now() time.Time { return c.now }

func (c *fakeSleepClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}
