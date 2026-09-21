package onepiece

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
)

func loadFixture(t *testing.T, name string) rfs.Page {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return page
}

func TestExtractReturnsMainSeriesChapters(t *testing.T) {
	items, err := Flow{}.Extract(loadFixture(t, "one-piece-archive.html"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := []struct{ guid, title, link string }{
		{
			guid:  "one-piece:chapter:1193",
			title: "One Piece Chapter 1193 — I'm still working on it",
			link:  "https://tcbonepiecechapters.com/chapters/8005/one-piece-chapter-1193",
		},
		{
			guid:  "one-piece:chapter:1192",
			title: "One Piece Chapter 1192 — We won't stand for it",
			link:  "https://tcbonepiecechapters.com/chapters/8002/one-piece-chapter-1192",
		},
		{
			guid:  "one-piece:chapter:1191",
			title: "One Piece Chapter 1191",
			link:  "https://tcbonepiecechapters.com/chapters/7998/one-piece-chapter-1191",
		},
	}
	if len(items) != len(want) {
		t.Fatalf("extracted %d items, want %d: %#v", len(items), len(want), items)
	}
	for i, expected := range want {
		got := items[i]
		if got.GUID != expected.guid || got.Title != expected.title || got.Link != expected.link {
			t.Fatalf("item %d = %#v, want %#v", i, got, expected)
		}
	}
}

func TestExtractRejectsPagesWithoutChapters(t *testing.T) {
	_, err := Flow{}.Extract(rfs.Page(`<html><body><p>Nothing to see here</p></body></html>`))
	if err == nil || !strings.Contains(err.Error(), "no chapter links") {
		t.Fatalf("error = %v, want a rejection", err)
	}
}

func TestExtractRejectsChapterHeadingMismatch(t *testing.T) {
	page := rfs.Page(`<html><body><a href="/chapters/1/one-piece-chapter-5"><div>One Piece Chapter 6</div></a></body></html>`)
	_, err := Flow{}.Extract(page)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want a heading mismatch rejection", err)
	}
}

func chapterItems(numbers ...int) []rfs.ExtractedItem {
	items := make([]rfs.ExtractedItem, 0, len(numbers))
	for _, number := range numbers {
		items = append(items, rfs.ExtractedItem{
			GUID:  guidPrefix + strconv.Itoa(number),
			Title: "One Piece Chapter " + strconv.Itoa(number),
			Link:  "https://tcbonepiecechapters.com/chapters/1/one-piece-chapter-" + strconv.Itoa(number),
		})
	}
	return items
}

func encodeCheckpointForTest(t *testing.T, state checkpoint) json.RawMessage {
	t.Helper()
	encoded, err := encodeCheckpoint(state)
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}
	return encoded
}

func decodeCheckpoint(t *testing.T, raw json.RawMessage) checkpoint {
	t.Helper()
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode checkpoint %q: %v", raw, err)
	}
	return state
}

func announcementGUIDs(items []rfs.ExtractedItem) []string {
	guids := make([]string, 0, len(items))
	for _, item := range items {
		guids = append(guids, item.GUID)
	}
	return guids
}

func TestEvaluateInitializesWithOnlyTheLatestChapter(t *testing.T) {
	decision, err := Flow{}.Evaluate(nil, chapterItems(1193, 1192, 1191))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := announcementGUIDs(decision.Announcements); !slices.Equal(got, []string{"one-piece:chapter:1193"}) {
		t.Fatalf("initial announcements = %v, want only the latest chapter", got)
	}
	state := decodeCheckpoint(t, decision.Checkpoint)
	if state.Version != CheckpointVersion || state.Initial != 1193 || !slices.Equal(state.Announced, []int{1193}) {
		t.Fatalf("initial checkpoint = %#v", state)
	}
}

func TestEvaluateAnnouncesMissedChaptersInOrder(t *testing.T) {
	start := encodeCheckpointForTest(t, checkpoint{Version: CheckpointVersion, Initial: 1193, Announced: []int{1193}})
	decision, err := Flow{}.Evaluate(start, chapterItems(1196, 1195, 1194, 1193))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	want := []string{"one-piece:chapter:1194", "one-piece:chapter:1195", "one-piece:chapter:1196"}
	if got := announcementGUIDs(decision.Announcements); !slices.Equal(got, want) {
		t.Fatalf("announcements = %v, want %v", got, want)
	}
	state := decodeCheckpoint(t, decision.Checkpoint)
	if !slices.Equal(state.Announced, []int{1193, 1194, 1195, 1196}) {
		t.Fatalf("checkpoint announced = %v", state.Announced)
	}
}

func TestEvaluateAnnouncesLateChapterBelowTheLatest(t *testing.T) {
	start := encodeCheckpointForTest(t, checkpoint{Version: CheckpointVersion, Initial: 100, Announced: []int{100, 102}})
	decision, err := Flow{}.Evaluate(start, chapterItems(102, 101))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := announcementGUIDs(decision.Announcements); !slices.Equal(got, []string{"one-piece:chapter:101"}) {
		t.Fatalf("announcements = %v, want the late chapter", got)
	}
}

func TestEvaluateIgnoresHistoricalChapters(t *testing.T) {
	start := encodeCheckpointForTest(t, checkpoint{Version: CheckpointVersion, Initial: 1193, Announced: []int{1193}})
	decision, err := Flow{}.Evaluate(start, chapterItems(1193, 1190))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(decision.Announcements) != 0 {
		t.Fatalf("announcements = %#v, want none", decision.Announcements)
	}
}

func TestEvaluateDoesNotRepeatAnnouncements(t *testing.T) {
	first, err := Flow{}.Evaluate(nil, chapterItems(1193, 1192))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	second, err := Flow{}.Evaluate(first.Checkpoint, chapterItems(1193, 1192))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(second.Announcements) != 0 {
		t.Fatalf("repeated announcements = %#v, want none", second.Announcements)
	}
	state := decodeCheckpoint(t, second.Checkpoint)
	if !slices.Equal(state.Announced, []int{1193}) {
		t.Fatalf("checkpoint announced = %v", state.Announced)
	}
}

func TestEvaluateKeepsAnnouncedChapterWhenTheArchiveDropsIt(t *testing.T) {
	start := encodeCheckpointForTest(t, checkpoint{Version: CheckpointVersion, Initial: 100, Announced: []int{100, 101, 102}})
	decision, err := Flow{}.Evaluate(start, chapterItems(103, 100))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := announcementGUIDs(decision.Announcements); !slices.Equal(got, []string{"one-piece:chapter:103"}) {
		t.Fatalf("announcements = %v, want only chapter 103", got)
	}
	state := decodeCheckpoint(t, decision.Checkpoint)
	if !slices.Equal(state.Announced, []int{100, 101, 102, 103}) {
		t.Fatalf("checkpoint announced = %v, want dropped chapters retained", state.Announced)
	}
}

func TestEvaluateRejectsUnknownCheckpointVersion(t *testing.T) {
	_, err := Flow{}.Evaluate(json.RawMessage(`{"version":99,"initial":1193,"announced":[1193]}`), chapterItems(1194))
	if err == nil || !strings.Contains(err.Error(), "unsupported checkpoint version") {
		t.Fatalf("error = %v, want an unsupported version rejection", err)
	}
}

func TestEvaluateRejectsObservationsWithoutChapters(t *testing.T) {
	if _, err := (Flow{}).Evaluate(nil, nil); err == nil {
		t.Fatal("evaluation of an empty observation succeeded")
	}
	if _, err := (Flow{}).Evaluate(json.RawMessage(`{"version":1,"initial":1,"announced":[1]}`), []rfs.ExtractedItem{{GUID: "other:1"}}); err == nil {
		t.Fatal("evaluation accepted an item that is not a One Piece chapter")
	}
}
