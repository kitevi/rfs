package notices

import (
	"encoding/json"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
)

type announcementMetadata struct {
	Version  int       `json:"v"`
	Notice   Notice    `json:"notice"`
	Observed time.Time `json:"observed"`
}

func readMetadata(item rfs.Item) (announcementMetadata, bool) {
	var m announcementMetadata
	err := json.Unmarshal([]byte(item.Metadata), &m)
	return m, err == nil && m.Version == 1
}

func (Flow) LiveAt(item rfs.Item, at time.Time) bool {
	m, ok := readMetadata(item)
	return !ok || Eligible(m.Notice, at)
}

func (Flow) PresentItem(item rfs.Item) rfs.Item {
	m, ok := readMetadata(item)
	item.DateLabel = "Pubblicazione: non disponibile"
	if ok && m.Notice.Published.Known() {
		item.DateLabel = "Pubblicato: " + m.Notice.Published.Display()
	}
	return item
}
