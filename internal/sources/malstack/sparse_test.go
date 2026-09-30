package malstack_test

import (
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/malstack"
)

const ruriDragonCard = `<div class="seasonal-anime js-seasonal-anime">
    <div class="head">
      <div class="title-text">
        <h2 class="h2_anime_title"><a href="https://myanimelist.net/anime/63188/Ruri_Dragon" class="link-title">Ruri Dragon</a></h2></div>
      <div class="add-edit-button">
        <a href="https://myanimelist.net/ownlist/anime/add?selected_series_id=63188&amp;hideLayout=1&amp;click_type=list-add-stacks" class="Lightbox_AddEdit button_add ga-click btn-anime-watch-status js-anime-watch-status button notinmylist addtolist"><span class="ga-click ga-impression" data-ga-click-type="list-add-stacks" data-ga-click-param="aid:63188" data-work-type="anime" data-status="0" style="line-height: unset;" data-ga-impression-type="list-add-button">Add to My List</span></a>
      </div>
    </div>
    <div class="image">
      <a href="https://myanimelist.net/anime/63188/Ruri_Dragon"><img src="https://cdn.myanimelist.net/images/anime/1109/154687.jpg" width="124" alt="Ruri Dragon" srcset="https://cdn.myanimelist.net/images/anime/1109/154687.jpg 1x, https://cdn.myanimelist.net/images/anime/1109/154687.jpg 2x"></a>
    </div>
    <div class="info">
      TV, -,
      ? eps
      <span class="fl-r mr12">Me:<i class="fas fa-star"></i>-</span>
      <span class="fl-r mr12">Author:<i class="fas fa-star"></i>-</span>
    </div>
    <div class="intro">` + "\n      \n" + `    </div>
  </div>`

func sparseStackHTML() string {
	return strings.Replace(stackHTML(1), `<div class="list-anime-list">`, `<div class="list-anime-list">`+ruriDragonCard, 1)
}

func TestFlowExtractsSparseAnime(t *testing.T) {
	items := observations(t, sparseStackHTML())
	if len(items) != 1 {
		t.Fatalf("sparse card must remain in the collection, got %d items", len(items))
	}
	item := items[0]
	if item.GUID != "63188" || item.Title != "Ruri Dragon" || item.Link != "https://myanimelist.net/anime/63188" {
		t.Fatalf("unexpected sparse anime identity: %+v", item)
	}
}

func TestChangesHandlesSparseAnime(t *testing.T) {
	page := sparseStackHTML()
	before := observations(t, page)
	withNotes := observations(t, strings.Replace(page, "<div class=\"intro\">\n      \n    </div>", `<div class="intro">Announced.</div>`, 1))
	whitespace := observations(t, strings.Replace(page, "<div class=\"intro\">\n      \n    </div>", "<div class=\"intro\"> &nbsp; \t\n </div>", 1))
	cases := []struct {
		name        string
		previous    []rfs.ExtractedItem
		current     []rfs.ExtractedItem
		title       string
		description string
	}{
		{"added without notes", nil, before, "Added: Ruri Dragon", "<p>Added to the stack.</p>"},
		{"unchanged empty notes", before, whitespace, "", ""},
		{"notes added", before, withNotes, "Notes updated: Ruri Dragon", `<p>Notes changed.</p><pre style="white-space:pre-wrap">+ Announced.</pre>`},
		{"notes cleared", withNotes, before, "Notes updated: Ruri Dragon", `<p>Notes changed.</p><pre style="white-space:pre-wrap">- Announced.</pre>`},
		{"removed without notes", before, nil, "Removed: Ruri Dragon", "<p>Removed from the stack.</p>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes, err := (malstack.Flow{}).Changes(tc.previous, tc.current)
			if err != nil {
				t.Fatalf("Changes: %v", err)
			}
			if tc.title == "" {
				if len(changes) != 0 {
					t.Fatalf("empty-note formatting produced changes: %+v", changes)
				}
				return
			}
			if len(changes) != 1 || changes[0].GUID != "63188" || changes[0].Title != tc.title || changes[0].Description != tc.description {
				t.Fatalf("changes = %+v, want %q with %q", changes, tc.title, tc.description)
			}
		})
	}
}
