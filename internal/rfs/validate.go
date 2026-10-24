package rfs

import (
	"fmt"
	"net/url"
)

// ValidateSource rejects a Source whose Flow capabilities and settings do not
// pick exactly one way of building its feed. The poller selects behavior by
// the optional interfaces a Flow implements, so a combination it cannot honor
// would otherwise only surface on the first poll, or not at all.
func ValidateSource(source Source) error {
	if source.ID == "" {
		return fmt.Errorf("source without an ID")
	}
	if source.Flow == nil {
		return fmt.Errorf("source %s: missing flow", source.ID)
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("source %s: URL %q is not an absolute HTTP(S) URL", source.ID, source.URL)
	}
	_, announces := source.Flow.(AnnouncementFlow)
	compares := comparesChanges(source.Flow)
	if announces && compares {
		return fmt.Errorf("source %s: flow cannot both announce and compare changes", source.ID)
	}
	if source.History != nil && (announces || compares) {
		return fmt.Errorf("source %s: catalog history only applies to projected feeds", source.ID)
	}
	if _, ok := source.Flow.(HistoryRebuilder); ok && source.History == nil {
		return fmt.Errorf("source %s: flow rebuilds stored history but the source keeps none", source.ID)
	}
	return nil
}

// ValidateSources validates every Source and rejects duplicate IDs, which
// would share one feed's stored state.
func ValidateSources(sources []Source) error {
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if err := ValidateSource(source); err != nil {
			return err
		}
		if seen[source.ID] {
			return fmt.Errorf("duplicate source ID %s", source.ID)
		}
		seen[source.ID] = true
	}
	return nil
}
