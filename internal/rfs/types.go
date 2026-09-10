package rfs

import "time"

// SourceMeta describes the RSS channel served for a Source.
type SourceMeta struct {
	// ItemDescriptionsHTML is only for Flows that construct safe HTML and escape
	// all upstream text. Never enable it for raw upstream descriptions.
	ItemDescriptionsHTML bool
	Title                string
	Description          string
	Link                 string
}

// Page is the response body fetched for a Source. A Flow decides how to decode
// it; HTML and JSON are both source-specific representations.
type Page []byte

// Flow extracts source-specific items from a fetched Page.
type Flow interface {
	Extract(Page) ([]ExtractedItem, error)

	// Version is bumped whenever Extract's output can change for a fixed Page.
	// rfs persists the version that produced each stored snapshot and, on a
	// mismatch, forces a full re-fetch+re-extraction rather than trusting an
	// HTTP 304 (which only proves the page bytes are unchanged, not that the
	// parser is). See docs/adr/0003-extract-version-invalidates-snapshot.md.
	Version() int
}

// HistoryPolicy opts a Source into catalog-history accumulation instead of
// snapshot replace. Stored rows are pruned to StoredLimit (never deleting
// live GUIDs); feeds serve at most VisibleLimit rows, hiding live threads
// until they reach MinLiveReplies mature posts.
type HistoryPolicy struct {
	VisibleLimit   int
	StoredLimit    int
	MinLiveReplies int
}

// DefaultCatalogHistory is the agreed window for /ptg/ and /film/: keep the
// last 10 superseded threads visible, store 11 to buffer 1 live thread, and
// surface a live thread once it has 100 posts.
func DefaultCatalogHistory() *HistoryPolicy {
	return &HistoryPolicy{VisibleLimit: 10, StoredLimit: 11, MinLiveReplies: 100}
}

// Source wires a hardcoded upstream resource to the Flow and feed metadata.
type Source struct {
	ID   string
	URL  string
	Meta SourceMeta
	Flow Flow

	// Interval overrides the process's default poll interval when positive.
	Interval time.Duration

	// EmitInitial publishes a change feed's first complete observation as feed
	// items instead of storing it as a silent baseline, so subscribers of a new
	// feed see the notices that are already in force. An extraction-version
	// rebaseline stays silent either way. The zero value keeps the ADR 0008
	// behavior (SeaDex and any Flow whose first observation must not announce).
	EmitInitial bool

	// EmitVersionChanges compares the stored baseline with a re-derived
	// observation when the Flow's extraction version changes, instead of
	// replacing the baseline in silence. It is for a Flow whose own comparison
	// treats an older stored payload as state rather than as a change: a
	// broader scope then hands existing subscribers the coverage they were
	// missing. A Flow that cannot compare older payloads announces its whole
	// feed again on every bump, so the zero value keeps the silent rebaseline.
	EmitVersionChanges bool

	// History, when non-nil, enables accumulation instead of replace.
	// Nil preserves the original current-state projection (e.g. meltzer).
	History *HistoryPolicy
}

// Item is a single entry in a Source's RSS feed.
type Item struct {
	GUID        string
	Title       string
	Link        string
	Description string
	// Metadata is opaque, versioned flow data, separate from rendered text.
	Metadata string
	// DateLabel overrides the HTML date; it is derived at presentation time.
	DateLabel string
	PubDate   time.Time
	// Replies is the catalog reply count observed when the thread was last
	// seen. Zero when the Flow does not report one.
	Replies int
}

// ExtractedItem is an Item emitted by a Flow before rfs has applied fallback
// values such as first-seen pubDate.
type ExtractedItem struct {
	GUID        string
	Title       string
	Link        string
	Description string
	// Metadata is opaque, versioned flow data, separate from rendered text.
	Metadata string
	PubDate  *time.Time
	// Replies is the catalog reply count for maturity filtering. Flows that
	// do not observe one leave it zero (treated as immature while live).
	Replies int
}
