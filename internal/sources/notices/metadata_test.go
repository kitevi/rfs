package notices

import (
	"encoding/json"
	"testing"

	"github.com/ppowo/rfs/internal/rfs"
)

func TestPublicationLabels(t *testing.T) {
	for _, test := range []struct {
		name      string
		published Date
		label     string
	}{
		{"day", DayDate(polledAt()), "Pubblicato: 10/09/2026"},
		{"minute", MinuteDate(polledAt()), "Pubblicato: 10/09/2026 12:00"},
		{"unknown", Date{}, "Pubblicazione: non disponibile"},
		{"unreadable", Date{Text: "unreadable"}, "Pubblicazione: non disponibile"},
		{"future", DayDate(polledAt().AddDate(0, 1, 0)), "Pubblicato: 10/10/2026"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(announcementMetadata{Version: 1, Notice: Notice{Published: test.published}, Observed: polledAt()})
			if err != nil {
				t.Fatal(err)
			}
			item := rfs.Item{Metadata: string(data), PubDate: polledAt()}
			presented := (Flow{}).PresentItem(item)
			if presented.DateLabel != test.label {
				t.Fatalf("label = %s", presented.DateLabel)
			}
			if !presented.PubDate.Equal(item.PubDate) {
				t.Fatal("presentation changed RSS timestamp")
			}
		})
	}
}

func TestUnreadableMetadataDoesNotInventDatesOrExpiry(t *testing.T) {
	for _, metadata := range []string{"", "malformed", `{"v":99}`} {
		item := rfs.Item{Metadata: metadata}
		flow := Flow{}
		if !flow.LiveAt(item, polledAt()) {
			t.Fatal("unknown metadata invented expiry")
		}
		if flow.PresentItem(item).DateLabel != "Pubblicazione: non disponibile" {
			t.Fatal("unknown metadata invented a publication date")
		}
	}
}
