// Package rfstest holds the helpers Flow tests share: an in-memory store, a
// feed reader, a fixture loader and a live page fetch. It is imported only
// from tests; rfs's own tests cannot use it because it imports rfs.
package rfstest

import (
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
)

// Store opens an in-memory SQLite store for one test and closes it when the
// test ends.
func Store(t testing.TB) *rfs.SQLiteStore {
	t.Helper()
	store, err := rfs.OpenSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
