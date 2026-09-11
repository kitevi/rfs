package main

import (
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
