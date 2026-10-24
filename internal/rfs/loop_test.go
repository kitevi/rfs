package rfs

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestLoopDrainsInFlightPollBeforeReturning verifies that on shutdown Run
// waits for an in-flight poll to return, so a commit already under way
// finishes before the process exits. This is the drain contract the
// self-update exec path and a SIGTERM both depend on.
func TestLoopDrainsInFlightPollBeforeReturning(t *testing.T) {
	started := make(chan struct{})
	proceed := make(chan struct{})
	pollDone := make(chan struct{})
	poll := func(context.Context) {
		close(started)
		<-proceed // block until the test releases the in-flight poll
		close(pollDone)
	}
	loop := Loop{Poll: poll, Interval: time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	runReturned := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(runReturned)
	}()

	// Wait for the initial poll to be in-flight.
	<-started

	// Shut down. Run must NOT return while the poll is still running.
	cancel()
	select {
	case <-runReturned:
		t.Fatal("Run returned before the in-flight poll finished (drain broken)")
	case <-time.After(50 * time.Millisecond):
		// Good: still draining.
	}

	// Release the poll. Now Run should return.
	close(proceed)
	select {
	case <-runReturned:
		// Good.
	case <-time.After(time.Second):
		t.Fatal("Run did not return after the in-flight poll finished")
	}
	<-pollDone
}

// TestLoopShutdownCancelsInFlightPoll verifies that shutdown reaches a poll
// that is still waiting on its context, so drain does not sit out the poll's
// own deadline. Without this, a poll with many paced requests left would keep
// the process from exiting or re-executing until every request had run.
func TestLoopShutdownCancelsInFlightPoll(t *testing.T) {
	pollStarted := make(chan struct{})
	poll := func(ctx context.Context) {
		close(pollStarted)
		<-ctx.Done() // a poll that only stops when its context is cancelled
	}
	loop := Loop{Poll: poll, Interval: time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	runReturned := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(runReturned)
	}()

	<-pollStarted
	cancel()

	select {
	case <-runReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after shutdown; the in-flight poll never saw the cancellation")
	}
}

// TestLoopCutsAPollAtItsTimeout verifies that a poll which never finishes on
// its own is cut by PollTimeout, so the next cycle can start.
func TestLoopCutsAPollAtItsTimeout(t *testing.T) {
	cut := make(chan struct{})
	poll := func(ctx context.Context) {
		<-ctx.Done()
		select {
		case <-cut:
		default:
			close(cut)
		}
	}
	loop := Loop{Poll: poll, Interval: time.Hour, PollTimeout: 50 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)

	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Fatal("a stuck poll was not cut by its own timeout")
	}
}

// TestLoopPollTimeoutDefaultsToInterval verifies that a cycle is allowed to
// run until the next one is due. A fixed two-minute bound used to cut polls
// whose paced follow-up requests needed longer, so they never committed.
func TestLoopPollTimeoutDefaultsToInterval(t *testing.T) {
	remaining := make(chan time.Duration, 1)
	poll := func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if !ok {
			remaining <- 0
			return
		}
		remaining <- time.Until(deadline)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Loop{Poll: poll, Interval: time.Hour}.Run(ctx)

	got := <-remaining
	if got < 59*time.Minute || got > time.Hour {
		t.Fatalf("poll deadline in %s, want about the one-hour interval", got)
	}
}

func TestLoopNeverOverlapsPolls(t *testing.T) {
	firstPollStarted := make(chan struct{})
	releaseFirstPoll := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirstPoll) }) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer release()

	var mu sync.Mutex
	calls := 0
	loop := Loop{
		Interval:    5 * time.Millisecond,
		PollTimeout: time.Second,
		Poll: func(context.Context) {
			mu.Lock()
			calls++
			call := calls
			mu.Unlock()
			if call == 1 {
				close(firstPollStarted)
				<-releaseFirstPoll
			}
		},
	}
	loopDone := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(loopDone)
	}()

	<-firstPollStarted
	time.Sleep(30 * time.Millisecond) // several intervals elapse while poll 1 is blocked
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("poll calls while first poll is in flight = %d, want 1", gotCalls)
	}

	release()
	cancel()
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("Loop did not stop after cancellation")
	}
}
