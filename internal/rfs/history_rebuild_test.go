package rfs_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/film"
	"github.com/kitevi/rfs/internal/sources/ptg"
)

func TestStoredInputRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, page string
		flow       rfs.Flow
	}{
		{"ptg", `[{"threads":[{"no":123,"sub":"/ptg/ - General","com":"discuss /ptg/ today<br>body &amp; links","time":1000,"replies":123}]}]`, ptg.Flow{}},
		{"film", `[{"threads":[{"no":123,"sub":"/film/ - General","com":"discuss /film/ today<br>body &amp; links","time":1000,"replies":123}]}]`, film.Flow{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extracted, err := tc.flow.Extract(rfs.Page(tc.page))
			if err != nil {
				t.Fatal(err)
			}
			x := extracted[0]
			want := rfs.Item{GUID: x.GUID, Title: x.Title, Link: x.Link, Description: x.Description, PubDate: *x.PubDate, Replies: x.Replies, Metadata: x.Metadata}
			old := want
			old.Title = "stale"
			old.Description = "stale"
			old.Link = "stale"
			old.Replies = 0
			rebuild := tc.flow.(rfs.HistoryRebuilder).RebuildStored
			got, err := rebuild(old)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round trip: got %#v want %#v", got, want)
			}
			again, err := rebuild(got)
			if err != nil || !reflect.DeepEqual(again, want) {
				t.Fatalf("repeat rebuild: %#v %v", again, err)
			}
			legacy := rfs.Item{Title: "/" + tc.name + "/ - /" + tc.name + "/ repeated marker"}
			once, err := rebuild(legacy)
			if err != nil {
				t.Fatal(err)
			}
			twice, err := rebuild(once)
			if err != nil || !reflect.DeepEqual(once, twice) {
				t.Fatalf("legacy repair repeated: %#v %#v %v", once, twice, err)
			}
			for _, raw := range []string{"{", "{}"} {
				bad := old
				bad.Metadata = raw
				if _, err := rebuild(bad); err == nil {
					t.Fatalf("accepted bad input %q", raw)
				}
			}
		})
	}
}

func TestHistoryRebuildsArchivedRowsOnlyUnderPoll(t *testing.T) {
	ctx := context.Background()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page := rfs.Page(`[{"threads":[{"no":123,"sub":"/ptg/ - General","com":"edition","time":1788200342,"replies":1}]}]`)
	extracted, err := (ptg.Flow{}).Extract(page)
	if err != nil {
		t.Fatal(err)
	}
	x := extracted[0]
	archived := rfs.Item{GUID: x.GUID, Title: "stale", Link: "stale", Description: "stale", PubDate: *x.PubDate, Replies: 0, Metadata: x.Metadata}
	if err := store.MergeHistory(ctx, "ptg", []rfs.Item{archived}, nil, 11); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFetchCache(ctx, "ptg", rfs.FetchCache{ExtractVersion: ptg.ExtractVersion - 1}); err != nil {
		t.Fatal(err)
	}

	pollPTG(t, store, ptg.Flow{})

	stored, err := store.LoadSnapshot(ctx, "ptg")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("lost history: %#v", stored)
	}
	for _, item := range stored {
		if item.Metadata == "" {
			t.Fatal("poll lost extraction inputs")
		}
		if item.GUID == archived.GUID {
			if item.Title == "stale" || item.Link == "stale" || item.Description == "stale" {
				t.Fatalf("archived row not rebuilt: %#v", item)
			}
		}
	}
	var got rfs.Item
	for _, item := range stored {
		if item.GUID == archived.GUID {
			got = item
		}
	}
	if got.Title != x.Title || got.Link != x.Link || got.Description != x.Description {
		t.Fatalf("archived projection not rebuilt: %#v", got)
	}
	if got.Replies != x.Replies {
		t.Fatalf("archived replies not rebuilt from saved input: %d, want %d", got.Replies, x.Replies)
	}
	live, err := store.LoadLiveGUIDs(ctx, "ptg")
	if err != nil || len(live) != 1 || live[0] == archived.GUID {
		t.Fatalf("archived row became live: %v %v", live, err)
	}
	cache, err := store.LoadFetchCache(ctx, "ptg")
	if err != nil || cache.ExtractVersion != ptg.ExtractVersion {
		t.Fatalf("version not advanced: %#v", cache)
	}
}

func TestCommitHistorySkipsUndecodableRow(t *testing.T) {
	ctx := context.Background()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bad := rfs.Item{GUID: "ptg:bad", Title: "stale-but-kept"}
	good := rfs.Item{GUID: "ptg:good", Title: "stale"}
	if err := store.MergeHistory(ctx, "ptg", []rfs.Item{bad, good}, nil, 11); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFetchCache(ctx, "ptg", rfs.FetchCache{ExtractVersion: 1}); err != nil {
		t.Fatal(err)
	}
	rebuild := func(item rfs.Item) (rfs.Item, error) {
		if item.GUID == "ptg:bad" {
			return rfs.Item{}, errors.New("bad input")
		}
		item.Title = "rebuilt"
		return item, nil
	}
	fresh := rfs.Item{GUID: "ptg:new", Title: "new"}
	if err := store.CommitHistory(ctx, "ptg", []rfs.Item{fresh}, []string{"ptg:new"}, 11, rfs.FetchCache{ExtractVersion: 2}, rebuild); err != nil {
		t.Fatalf("one bad row wedged the commit: %v", err)
	}
	stored, err := store.LoadSnapshot(ctx, "ptg")
	if err != nil {
		t.Fatal(err)
	}
	byGUID := map[string]rfs.Item{}
	for _, item := range stored {
		byGUID[item.GUID] = item
	}
	if byGUID["ptg:bad"].Title != "stale-but-kept" {
		t.Fatalf("bad row not preserved: %#v", byGUID["ptg:bad"])
	}
	if byGUID["ptg:good"].Title != "rebuilt" {
		t.Fatalf("good row not rebuilt: %#v", byGUID["ptg:good"])
	}
	if byGUID["ptg:new"].Title != "new" {
		t.Fatalf("fresh row not merged: %#v", byGUID)
	}
	cache, err := store.LoadFetchCache(ctx, "ptg")
	if err != nil || cache.ExtractVersion != 2 {
		t.Fatalf("version not advanced past skipped row: %#v", cache)
	}
}

func TestCommitHistoryRollsBackChangedIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old := rfs.Item{GUID: "ptg:1", Title: "old"}
	if err := store.MergeHistory(ctx, "ptg", []rfs.Item{old}, nil, 11); err != nil {
		t.Fatal(err)
	}
	for _, rebuild := range []func(rfs.Item) (rfs.Item, error){
		func(item rfs.Item) (rfs.Item, error) { item.GUID = "changed"; return item, nil },
		func(item rfs.Item) (rfs.Item, error) { item.PubDate = item.PubDate.Add(1); return item, nil },
	} {
		if err := store.CommitHistory(ctx, "ptg", nil, nil, 11, rfs.FetchCache{}, rebuild); err == nil {
			t.Fatal("accepted changed identity")
		}
	}
	stored, err := store.LoadSnapshot(ctx, "ptg")
	if err != nil || !reflect.DeepEqual(stored, []rfs.Item{old}) {
		t.Fatalf("identity change altered history: %#v %v", stored, err)
	}
}
