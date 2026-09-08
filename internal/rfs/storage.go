package rfs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenInMemorySQLiteStore() (*SQLiteStore, error) {
	return OpenSQLiteStore(":memory:")
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
		`CREATE TABLE IF NOT EXISTS fetch_cache (
			source_id TEXT PRIMARY KEY,
			etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			extract_version INTEGER NOT NULL DEFAULT 0
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
	if err := s.addColumnIfMissing(ctx, "fetch_cache", "extract_version", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "snapshots", "replies", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_state (
			source_id TEXT NOT NULL,
			guid TEXT NOT NULL,
			PRIMARY KEY (source_id, guid)
		)`); err != nil {
		return err
	}
	return nil
}

// addColumnIfMissing adds column to table with the given SQLite definition when
// it is not already present. table and column are internal constants, not user
// input, so interpolating them into the pragma/ALTER is safe.
func (s *SQLiteStore) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			colType string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *SQLiteStore) SaveSnapshot(ctx context.Context, sourceID string, items []Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM snapshots WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO snapshots (source_id, guid, title, link, description, pub_date, replies) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		if _, err := stmt.ExecContext(ctx, sourceID, item.GUID, item.Title, item.Link, item.Description, formatStoreTime(item.PubDate), item.Replies); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) LoadSnapshot(ctx context.Context, sourceID string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT guid, title, link, description, pub_date, replies FROM snapshots WHERE source_id = ? ORDER BY pub_date DESC, guid ASC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		var pubDate string
		if err := rows.Scan(&item.GUID, &item.Title, &item.Link, &item.Description, &pubDate, &item.Replies); err != nil {
			return nil, err
		}
		parsed, err := parseStoreTime(pubDate)
		if err != nil {
			return nil, fmt.Errorf("parse stored pubDate for %s: %w", item.GUID, err)
		}
		item.PubDate = parsed
		items = append(items, item)
	}
	return items, rows.Err()
}

// MergeHistory accumulates observed catalog threads for history sources.
// It upserts items (refreshing title/link/description/pub_date/replies),
// replaces the live GUID set, then prunes non-live rows beyond keepStored
// newest (pub_date DESC, guid ASC). Live GUIDs are never pruned. The three
// steps run in one transaction.
func (s *SQLiteStore) MergeHistory(ctx context.Context, sourceID string, items []Item, liveGUIDs []string, keepStored int) error {
	if keepStored <= 0 {
		keepStored = 11
	}
	deduped := dedupeLiveGUIDs(liveGUIDs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)

	if len(items) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO snapshots (source_id, guid, title, link, description, pub_date, replies) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_id, guid) DO UPDATE SET title = excluded.title, link = excluded.link, description = excluded.description, pub_date = excluded.pub_date, replies = excluded.replies`)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := stmt.ExecContext(ctx, sourceID, item.GUID, item.Title, item.Link, item.Description, formatStoreTime(item.PubDate), item.Replies); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM live_state WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	if len(deduped) > 0 {
		liveStmt, err := tx.PrepareContext(ctx, `INSERT INTO live_state (source_id, guid) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		for _, guid := range deduped {
			if _, err := liveStmt.ExecContext(ctx, sourceID, guid); err != nil {
				_ = liveStmt.Close()
				return err
			}
		}
		_ = liveStmt.Close()
	}

	if err := pruneHistoryTx(ctx, tx, sourceID, deduped, keepStored); err != nil {
		return err
	}
	return tx.Commit()
}

func dedupeLiveGUIDs(guids []string) []string {
	seen := make(map[string]struct{}, len(guids))
	out := make([]string, 0, len(guids))
	for _, g := range guids {
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	return out
}

func pruneHistoryTx(ctx context.Context, tx *sql.Tx, sourceID string, liveGUIDs []string, keepStored int) error {
	if len(liveGUIDs) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM snapshots WHERE source_id = ? AND guid NOT IN (
			SELECT guid FROM snapshots WHERE source_id = ? ORDER BY pub_date DESC, guid ASC LIMIT ?
		)`, sourceID, sourceID, keepStored)
		return err
	}
	placeholders := strings.Repeat("?,", len(liveGUIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]any, 0, len(liveGUIDs)+3)
	args = append(args, sourceID)
	for _, g := range liveGUIDs {
		args = append(args, g)
	}
	args = append(args, sourceID, keepStored)
	// SQLite does not allow a bound LIMIT parameter in every build, so keepStored
	// is an internal constant (not user input) interpolated after a sanity clamp.
	if keepStored < 1 {
		keepStored = 1
	}
	query := `DELETE FROM snapshots WHERE source_id = ? AND guid NOT IN (` + placeholders + `) AND guid NOT IN (
		SELECT guid FROM snapshots WHERE source_id = ? ORDER BY pub_date DESC, guid ASC LIMIT ` + itoa(keepStored) + `
	)`
	// Rebuild args without the trailing keepStored bound value.
	args = args[:len(args)-1]
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// LoadVisibleHistory serves history feeds: stored threads minus immature
// live threads (live with replies < minLiveReplies), newest first, capped
// at limit. Dead threads are always visible; mature live threads are visible
// while still live.
func (s *SQLiteStore) LoadVisibleHistory(ctx context.Context, sourceID string, minLiveReplies, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.guid, s.title, s.link, s.description, s.pub_date, s.replies
		FROM snapshots s LEFT JOIN live_state l ON s.source_id = l.source_id AND s.guid = l.guid
		WHERE s.source_id = ? AND (l.guid IS NULL OR s.replies >= ?)
		ORDER BY s.pub_date DESC, s.guid ASC LIMIT ?`, sourceID, minLiveReplies, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var item Item
		var pubDate string
		if err := rows.Scan(&item.GUID, &item.Title, &item.Link, &item.Description, &pubDate, &item.Replies); err != nil {
			return nil, err
		}
		parsed, err := parseStoreTime(pubDate)
		if err != nil {
			return nil, fmt.Errorf("parse stored pubDate for %s: %w", item.GUID, err)
		}
		item.PubDate = parsed
		items = append(items, item)
	}
	return items, rows.Err()
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO fetch_cache (source_id, etag, last_modified, extract_version) VALUES (?, ?, ?, ?)
		ON CONFLICT(source_id) DO UPDATE SET etag = excluded.etag, last_modified = excluded.last_modified, extract_version = excluded.extract_version`, sourceID, cache.ETag, cache.LastModified, cache.ExtractVersion)
	return err
}

func (s *SQLiteStore) LoadFetchCache(ctx context.Context, sourceID string) (FetchCache, error) {
	var cache FetchCache
	err := s.db.QueryRowContext(ctx, `SELECT etag, last_modified, extract_version FROM fetch_cache WHERE source_id = ?`, sourceID).Scan(&cache.ETag, &cache.LastModified, &cache.ExtractVersion)
	if err == sql.ErrNoRows {
		return FetchCache{}, nil
	}
	return cache, err
}

func rollbackUnlessCommitted(tx *sql.Tx) {
	_ = tx.Rollback()
}

func formatStoreTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseStoreTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
