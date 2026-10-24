package rfs

import (
	"context"
	"time"
)

// Loop polls on a fixed interval until ctx is cancelled. Cancellation reaches
// the in-flight poll, which stops at its next request or gate wait; Run then
// waits for it to return (drain), so the process never exits or re-execs under
// a running poll. Stopping a poll early is safe because every poll commits its
// items, state and fetch validators in one transaction that does not observe
// cancellation (ADR 0013): a poll that stopped before committing left nothing
// behind, and the next start redoes it.
type Loop struct {
	Poll     func(context.Context)
	Interval time.Duration

	// PollTimeout bounds a single poll cycle. It defaults to Interval when
	// zero, so a cycle may run until the next one is due. A cycle has to fit
	// every follow-up request its sources make under the domain gate's
	// spacing, so a fixed short bound would cut a healthy poll that has many
	// pages to read and repeat the same work every cycle.
	PollTimeout time.Duration
}

func (l Loop) Run(ctx context.Context) {
	pollTimeout := l.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = l.Interval
	}

	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		// The poll context follows the shutdown signal, so shutdown stops the
		// poll between requests instead of waiting for every remaining one.
		// Running it synchronously prevents overlap.
		pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
		l.Poll(pollCtx)
		cancel()

		wait := time.Until(started.Add(l.Interval))
		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
