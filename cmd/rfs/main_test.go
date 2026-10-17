package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
)

func TestShouldEnableSelfUpdate(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		flagEnabled bool
		want        bool
	}{
		{"release build enabled by default", "2026.709.201956", true, true},
		{"release-like local build can opt out", "2026.709.201956", false, false},
		{"dev build is disabled even when flag is true", "dev", true, false},
		{"dev build is disabled when flag is false", "dev", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableSelfUpdate(tt.version, tt.flagEnabled)
			if got != tt.want {
				t.Fatalf("shouldEnableSelfUpdate(%q, %v) = %v, want %v", tt.version, tt.flagEnabled, got, tt.want)
			}
		})
	}
}

func TestPollCycleDetails(t *testing.T) {
	sources := []rfs.Source{{ID: "meltzer-5-star-matches"}}
	if got := pollCycleDetails(sources); got != "source poll meltzer-5-star-matches" {
		t.Fatalf("pollCycleDetails(one source) = %q", got)
	}
	if got := pollCycleDetails(nil); got != "0 source polls" {
		t.Fatalf("pollCycleDetails(no sources) = %q", got)
	}
}

// gateTestClock is a GateClock that records sleeps instead of waiting.
type gateTestClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *gateTestClock) Now() time.Time { return c.now }

func (c *gateTestClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func TestGateGuardSpacesFollowUpRequestsPerDomain(t *testing.T) {
	clock := &gateTestClock{now: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)}
	gate := rfs.NewDomainGate(clock, rfs.DomainGateConfig{MinSpacing: 10 * time.Second})
	guard := gateGuard(gate)

	if err := guard(t.Context(), "https://example.com/album:1/metadata"); err != nil {
		t.Fatal(err)
	}
	if err := guard(t.Context(), "https://example.com/album:2/metadata"); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 10*time.Second {
		t.Fatalf("follow-up requests slept %v, want one per-domain MinSpacing wait", clock.sleeps)
	}
	if err := guard(t.Context(), "https://other.test/album:1/metadata"); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 {
		t.Fatalf("another domain inherited spacing: %v", clock.sleeps)
	}
}

func TestNewHTTPServerSetsConnectionTimeouts(t *testing.T) {
	server := newHTTPServer(":0", http.NotFoundHandler(), serverTimeouts{})
	if server.Addr != ":0" || server.Handler == nil {
		t.Fatalf("server = %#v, want the requested address and handler", server)
	}
	timeouts := map[string]time.Duration{
		"ReadHeaderTimeout": server.ReadHeaderTimeout,
		"ReadTimeout":       server.ReadTimeout,
		"WriteTimeout":      server.WriteTimeout,
		"IdleTimeout":       server.IdleTimeout,
	}
	for name, got := range timeouts {
		if got <= 0 {
			t.Errorf("%s = %s, want a bounded timeout so a slow client cannot pin a connection", name, got)
		}
	}
}

func TestNewHTTPServerClosesConnectionsThatNeverFinishHeaders(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newHTTPServer(listener.Addr().String(), http.NotFoundHandler(), serverTimeouts{
		ReadHeaderTimeout: 50 * time.Millisecond,
	})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: rfs\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// The server must give up and close the connection on its own; a read that
	// only ends because of the test deadline means the header timeout is unused.
	if _, err := io.Copy(io.Discard, conn); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatal("the server kept a partial-header connection open; ReadHeaderTimeout is not enforced")
		}
	}
}

func TestBuildPollSchedulesHonorsSourceIntervals(t *testing.T) {
	defaultInterval := time.Hour
	sources := []rfs.Source{
		{ID: "default-a"},
		{ID: "fast-source", Interval: 30 * time.Minute},
		{ID: "default-b"},
	}

	schedules := buildPollSchedules(sources, defaultInterval)
	got := make(map[string]time.Duration)
	for _, schedule := range schedules {
		for _, source := range schedule.sources {
			if _, duplicate := got[source.ID]; duplicate {
				t.Fatalf("source %q appears in more than one poll schedule", source.ID)
			}
			got[source.ID] = schedule.interval
		}
	}

	want := map[string]time.Duration{
		"default-a":   time.Hour,
		"fast-source": 30 * time.Minute,
		"default-b":   time.Hour,
	}
	if len(got) != len(want) {
		t.Fatalf("scheduled %d sources, want %d", len(got), len(want))
	}
	for sourceID, wantInterval := range want {
		if got[sourceID] != wantInterval {
			t.Errorf("source %q interval = %s, want %s", sourceID, got[sourceID], wantInterval)
		}
	}
}
