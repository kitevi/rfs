package rfs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLiteStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) init(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS change_state (
			source_id TEXT PRIMARY KEY,
			baseline BLOB NOT NULL,
			version INTEGER NOT NULL,
			revision INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS snapshots (
			source_id TEXT NOT NULL,
			guid TEXT NOT NULL,
			title TEXT NOT NULL,
			link TEXT NOT NULL,
			description TEXT NOT NULL,
			pub_date TEXT NOT NULL,
			replies INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (source_id, guid)
		)`,
		`CREATE TABLE IF NOT EXISTS live_state (
			source_id TEXT NOT NULL,
			guid TEXT NOT NULL,
			PRIMARY KEY (source_id, guid)
		)`,
		`CREATE TABLE IF NOT EXISTS first_seen (
			source_id TEXT NOT NULL,
			guid TEXT NOT NULL,
			seen_at TEXT NOT NULL,
			PRIMARY KEY (source_id, guid)
		)`,
		`CREATE TABLE IF NOT EXISTS item_dates (
			source_id TEXT NOT NULL,
			guid TEXT NOT NULL,
			pub_date TEXT NOT NULL,
			PRIMARY KEY (source_id, guid)
		)`,
		`CREATE TABLE IF NOT EXISTS fetch_cache (
			source_id TEXT PRIMARY KEY,
			etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			extract_version INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS announcement_state (
			source_id TEXT PRIMARY KEY,
			checkpoint BLOB NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return s.migrate(ctx)
}

// migrate adds columns introduced after the initial schema to databases
// created by older rfs builds. Each step is idempotent.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	if err := s.addColumnIfMissing(ctx, "snapshots", "metadata", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "fetch_cache", "extract_version", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return s.addColumnIfMissing(ctx, "snapshots", "replies", "INTEGER NOT NULL DEFAULT 0")
}

// addColumnIfMissing adds column to table with the given SQLite definition when
// it is not already present. table, column and definition are internal
// constants, not user input, so interpolating them into the ALTER is safe.
func (s *SQLiteStore) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	var present int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&present); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if present > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// inTx runs fn in one transaction: it commits when fn returns nil and rolls back
// otherwise.
func (s *SQLiteStore) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op once committed
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// execer and queryer are the parts of *sql.DB and *sql.Tx one statement needs.
type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// The three ways to write an Item differ only in what an already stored GUID
// means: an error, the stored Item wins, or the new Item wins.
const (
	insertItem    = "INSERT" + intoSnapshots
	insertNewItem = "INSERT OR IGNORE" + intoSnapshots
	upsertItem    = insertItem + ` ON CONFLICT(source_id, guid) DO UPDATE SET title = excluded.title, link = excluded.link,
		description = excluded.description, pub_date = excluded.pub_date, replies = excluded.replies, metadata = excluded.metadata`
	intoSnapshots = ` INTO snapshots (source_id, guid, title, link, description, pub_date, replies, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
)

func writeItems(ctx context.Context, tx *sql.Tx, query, sourceID string, items []Item) error {
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, query, sourceID, item.GUID, item.Title, item.Link, item.Description, formatStoreTime(item.PubDate), item.Replies, item.Metadata); err != nil {
			return err
		}
	}
	return nil
}

// queryItems reads every Item a query returns. The query selects guid, title,
// link, description, pub_date, replies and metadata, in that order.
func queryItems(ctx context.Context, db queryer, query string, args ...any) ([]Item, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var item Item
		var pubDate string
		if err := rows.Scan(&item.GUID, &item.Title, &item.Link, &item.Description, &pubDate, &item.Replies, &item.Metadata); err != nil {
			return nil, err
		}
		if item.PubDate, err = parseStoreTime(pubDate); err != nil {
			return nil, fmt.Errorf("parse stored pubDate for %s: %w", item.GUID, err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) SaveSnapshot(ctx context.Context, sourceID string, items []Item) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM snapshots WHERE source_id = ?`, sourceID); err != nil {
			return err
		}
		return writeItems(ctx, tx, insertItem, sourceID, items)
	})
}

func (s *SQLiteStore) LoadSnapshot(ctx context.Context, sourceID string) ([]Item, error) {
	return queryItems(ctx, s.db, `SELECT guid, title, link, description, pub_date, replies, metadata FROM snapshots
		WHERE source_id = ? ORDER BY pub_date DESC, guid ASC`, sourceID)
}

// mergeHistoryTx accumulates observed catalog threads for history sources. It
// upserts items (refreshing every field), replaces the live GUID set, then
// prunes non-live rows beyond the keepStored newest (pub_date DESC, guid ASC).
// Live GUIDs are never pruned.
func mergeHistoryTx(ctx context.Context, tx *sql.Tx, sourceID string, items []Item, liveGUIDs []string, keepStored int) error {
	if keepStored <= 0 {
		keepStored = 11
	}
	if err := writeItems(ctx, tx, upsertItem, sourceID, items); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM live_state WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	for _, guid := range liveGUIDs {
		if guid == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO live_state (source_id, guid) VALUES (?, ?)`, sourceID, guid); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM snapshots WHERE source_id = ?
		AND guid NOT IN (SELECT guid FROM live_state WHERE source_id = ?)
		AND guid NOT IN (SELECT guid FROM snapshots WHERE source_id = ? ORDER BY pub_date DESC, guid ASC LIMIT ?)`,
		sourceID, sourceID, sourceID, keepStored)
	return err
}

// CommitHistory atomically rebuilds retained history, merges the current live
// observation, prunes, and advances the HTTP/extraction checkpoint. A row whose
// saved input no longer decodes is preserved and skipped; a rebuild that
// changes identity aborts the transaction.
func (s *SQLiteStore) CommitHistory(ctx context.Context, sourceID string, items []Item, liveGUIDs []string, keepStored int, cache FetchCache, rebuild func(Item) (Item, error)) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if rebuild != nil {
			if err := rebuildHistoryTx(ctx, tx, sourceID, items, rebuild); err != nil {
				return err
			}
		}
		if err := mergeHistoryTx(ctx, tx, sourceID, items, liveGUIDs, keepStored); err != nil {
			return err
		}
		return saveFetchCache(ctx, tx, sourceID, cache)
	})
}

// rebuildHistoryTx re-derives every stored row that this poll did not observe.
// Fresh items supersede old inputs, including incomplete legacy inputs.
func rebuildHistoryTx(ctx context.Context, tx *sql.Tx, sourceID string, fresh []Item, rebuild func(Item) (Item, error)) error {
	stored, err := queryItems(ctx, tx, `SELECT guid, title, link, description, pub_date, replies, metadata FROM snapshots WHERE source_id = ?`, sourceID)
	if err != nil {
		return err
	}
	observed := make(map[string]bool, len(fresh))
	for _, item := range fresh {
		observed[item.GUID] = true
	}
	for _, old := range stored {
		if observed[old.GUID] {
			continue
		}
		item, err := rebuild(old)
		if err != nil {
			// Undecodable inputs stay as they are: one bad row must not
			// wedge the source. Each future version bump retries the
			// rebuild, and a fresh observation still overwrites the row.
			log.Printf("rfs: rebuild %s/%s skipped: %v", sourceID, old.GUID, err)
			continue
		}
		if item.GUID != old.GUID || !item.PubDate.Equal(old.PubDate) {
			return fmt.Errorf("rebuild %s/%s changed identity or publication date", sourceID, old.GUID)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE snapshots SET title = ?, link = ?, description = ?, replies = ?, metadata = ? WHERE source_id = ? AND guid = ?`, item.Title, item.Link, item.Description, item.Replies, item.Metadata, sourceID, old.GUID); err != nil {
			return err
		}
	}
	return nil
}

// LoadVisibleHistory serves history feeds: stored threads minus immature
// live threads (live with replies < minLiveReplies), newest first, capped
// at limit. Dead threads are always visible; mature live threads are visible
// while still live.
func (s *SQLiteStore) LoadVisibleHistory(ctx context.Context, sourceID string, minLiveReplies, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 10
	}
	return queryItems(ctx, s.db, `SELECT s.guid, s.title, s.link, s.description, s.pub_date, s.replies, s.metadata
		FROM snapshots s LEFT JOIN live_state l ON s.source_id = l.source_id AND s.guid = l.guid
		WHERE s.source_id = ? AND (l.guid IS NULL OR s.replies >= ?)
		ORDER BY s.pub_date DESC, s.guid ASC LIMIT ?`, sourceID, minLiveReplies, limit)
}

// LoadLiveGUIDs returns the currently stored live set for a source.
func (s *SQLiteStore) LoadLiveGUIDs(ctx context.Context, sourceID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT guid FROM live_state WHERE source_id = ? ORDER BY guid ASC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var guids []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		guids = append(guids, g)
	}
	return guids, rows.Err()
}

func (s *SQLiteStore) FirstSeen(ctx context.Context, sourceID, guid string, discoveredAt time.Time) (time.Time, error) {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO first_seen (source_id, guid, seen_at) VALUES (?, ?, ?)`, sourceID, guid, formatStoreTime(discoveredAt))
	if err != nil {
		return time.Time{}, err
	}

	var seenAt string
	if err := s.db.QueryRowContext(ctx, `SELECT seen_at FROM first_seen WHERE source_id = ? AND guid = ?`, sourceID, guid).Scan(&seenAt); err != nil {
		return time.Time{}, err
	}
	return parseStoreTime(seenAt)
}

func (s *SQLiteStore) SaveFetchCache(ctx context.Context, sourceID string, cache FetchCache) error {
	return saveFetchCache(ctx, s.db, sourceID, cache)
}

// saveFetchCache upserts a Source's validators and extraction version, on the
// store itself or inside a commit's transaction.
func saveFetchCache(ctx context.Context, db execer, sourceID string, cache FetchCache) error {
	_, err := db.ExecContext(ctx, `INSERT INTO fetch_cache (source_id, etag, last_modified, extract_version) VALUES (?, ?, ?, ?)
		ON CONFLICT(source_id) DO UPDATE SET etag = excluded.etag, last_modified = excluded.last_modified, extract_version = excluded.extract_version`, sourceID, cache.ETag, cache.LastModified, cache.ExtractVersion)
	return err
}

func (s *SQLiteStore) LoadFetchCache(ctx context.Context, sourceID string) (FetchCache, error) {
	var cache FetchCache
	err := s.db.QueryRowContext(ctx, `SELECT etag, last_modified, extract_version FROM fetch_cache WHERE source_id = ?`, sourceID).Scan(&cache.ETag, &cache.LastModified, &cache.ExtractVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return FetchCache{}, nil
	}
	return cache, err
}

func formatStoreTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseStoreTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
