package node

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGO required
	"github.com/google/uuid"
)

// Store persists nodes in an embedded SQLite database.
// Notes:
//   - All mutating operations (AddMany/Update/Delete/Clear/RemoveBySubscription/Move)
//     write through immediately — Save() is kept for API compatibility and is a no-op.
//   - The full node struct (including per-protocol configs and raw_outbound) is stored
//     as JSON in the `data` column; indexed columns (name/protocol/address/port/sub_url)
//     are extracted for querying and display.
//   - Nodes belong to groups (table `groups`); the built-in "默认" group always exists
//     and cannot be renamed or deleted. Move() reorders within the same group only.
type Store struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
	err  error // open/init error, surfaced by Load()
}

// Group is a node group. The default group (ID "default", name "默认") is
// built-in and immutable.
type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

const (
	DefaultGroupID   = "default"
	DefaultGroupName = "默认"
)

func NewStore(path string) *Store {
	s := &Store{path: path}
	// busy_timeout avoids "database is locked"; WAL improves concurrent read/write
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		s.err = fmt.Errorf("打开数据库失败: %v", err)
		return s
	}
	// single connection avoids SQLITE_BUSY between goroutines
	db.SetMaxOpenConns(1)
	s.db = db
	if err := s.init(); err != nil {
		s.err = fmt.Errorf("初始化数据库失败: %v", err)
	}
	return s
}

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS nodes (
			id       TEXT PRIMARY KEY,
			position INTEGER NOT NULL DEFAULT 0,
			name     TEXT    NOT NULL DEFAULT '',
			protocol TEXT    NOT NULL DEFAULT '',
			address  TEXT    NOT NULL DEFAULT '',
			port     INTEGER NOT NULL DEFAULT 0,
			sub_url  TEXT    NOT NULL DEFAULT '',
			group_id TEXT    NOT NULL DEFAULT 'default',
			data     TEXT    NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id        TEXT PRIMARY KEY,
			name      TEXT    NOT NULL,
			position  INTEGER NOT NULL DEFAULT 0,
			is_default INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_position ON nodes(position)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_sub_url  ON nodes(sub_url)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_group    ON nodes(group_id)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	// dev databases created before groups existed: add the column in place
	if err := s.addColumnIfMissing("nodes", "group_id",
		`ALTER TABLE nodes ADD COLUMN group_id TEXT NOT NULL DEFAULT 'default'`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE nodes SET group_id = ? WHERE group_id = ''`, DefaultGroupID); err != nil {
		return err
	}
	// built-in default group (always exists, position 0)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO groups (id, name, position, is_default) VALUES (?, ?, 0, 1)`,
		DefaultGroupID, DefaultGroupName); err != nil {
		return err
	}
	return nil
}

// addColumnIfMissing adds a column via ALTER when it doesn't exist yet.
func (s *Store) addColumnIfMissing(table, column, alterSQL string) error {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // exists
		}
	}
	_, err = s.db.Exec(alterSQL)
	return err
}

// ─── row helpers ──────────────────────────────────────────────────────────────

func nodeData(n Node) (string, error) {
	b, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Store) insertAll(nodes []Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var maxPos sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(position) FROM nodes`).Scan(&maxPos); err != nil {
		return err
	}
	pos := maxPos.Int64 // 0 when table empty (NULL)

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO nodes
		(id, position, name, protocol, address, port, sub_url, group_id, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, n := range nodes {
		data, err := nodeData(n)
		if err != nil {
			return err
		}
		groupID := n.GroupID
		if groupID == "" {
			groupID = DefaultGroupID
		}
		pos++
		if _, err := stmt.Exec(n.ID, pos, n.Name, n.Protocol, n.Address, n.Port, n.SubURL, groupID, data); err != nil {
			return err
		}
		_ = i
	}
	return tx.Commit()
}

// ─── public API (same signatures as the previous JSON store) ─────────────────

// Load kept for API compatibility — the database is ready after NewStore.
func (s *Store) Load() error { return s.err }

// Save kept for API compatibility — writes are immediate; always succeeds.
func (s *Store) Save() error { return nil }

// Close releases the database connection (optional; called on app shutdown).
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) GetAll() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT data, group_id FROM nodes ORDER BY position`)
	if err != nil {
		return []Node{}
	}
	defer rows.Close()
	result := []Node{}
	for rows.Next() {
		var data, groupID string
		if err := rows.Scan(&data, &groupID); err != nil {
			continue
		}
		var n Node
		if err := json.Unmarshal([]byte(data), &n); err != nil {
			continue
		}
		// group_id column is authoritative (kept in sync by group operations)
		if groupID == "" {
			groupID = DefaultGroupID
		}
		n.GroupID = groupID
		result = append(result, n)
	}
	return result
}

func (s *Store) Get(id string) *Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var data, groupID string
	if err := s.db.QueryRow(`SELECT data, group_id FROM nodes WHERE id = ?`, id).Scan(&data, &groupID); err != nil {
		return nil
	}
	var n Node
	if err := json.Unmarshal([]byte(data), &n); err != nil {
		return nil
	}
	if groupID == "" {
		groupID = DefaultGroupID
	}
	n.GroupID = groupID
	return &n
}

func (s *Store) AddMany(nodes []Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil || len(nodes) == 0 {
		return
	}
	if err := s.insertAll(nodes); err != nil {
		fmt.Println("[store] AddMany error:", err)
	}
}

func (s *Store) Update(n Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	data, err := nodeData(n)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`UPDATE nodes SET
		name = ?, protocol = ?, address = ?, port = ?, sub_url = ?, group_id = ?, data = ?
		WHERE id = ?`,
		n.Name, n.Protocol, n.Address, n.Port, n.SubURL, n.GroupID, data, n.ID)
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
}

// Move swaps the node position by delta (-1 = up, +1 = down) within its own group.
func (s *Store) Move(id string, delta int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil || delta == 0 {
		return false
	}
	// find the node's group
	var groupID string
	if err := s.db.QueryRow(`SELECT group_id FROM nodes WHERE id = ?`, id).Scan(&groupID); err != nil {
		return false
	}
	// ordered (id, position) within the group
	rows, err := s.db.Query(`SELECT id, position FROM nodes WHERE group_id = ? ORDER BY position`, groupID)
	if err != nil {
		return false
	}
	type idPos struct {
		id  string
		pos int64
	}
	var entries []idPos
	for rows.Next() {
		var e idPos
		if rows.Scan(&e.id, &e.pos) == nil {
			entries = append(entries, e)
		}
	}
	rows.Close()

	idx := -1
	for i, e := range entries {
		if e.id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	j := idx + delta
	if j < 0 || j >= len(entries) {
		return false
	}
	// swap the positions of the two entries (both belong to the same group,
	// so other groups' ordering is untouched)
	if _, err := s.db.Exec(`UPDATE nodes SET position = ? WHERE id = ?`, entries[j].pos, entries[idx].id); err != nil {
		return false
	}
	if _, err := s.db.Exec(`UPDATE nodes SET position = ? WHERE id = ?`, entries[idx].pos, entries[j].id); err != nil {
		return false
	}
	return true
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM nodes`)
}

func (s *Store) RemoveBySubscription(subURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM nodes WHERE sub_url = ?`, subURL)
}

// ─── Group management ─────────────────────────────────────────────────────────

// GetGroups returns all groups ordered by position.
func (s *Store) GetGroups() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT id, name, is_default FROM groups ORDER BY position`)
	if err != nil {
		return []Group{}
	}
	defer rows.Close()
	result := []Group{}
	for rows.Next() {
		var g Group
		var isDefault int
		if err := rows.Scan(&g.ID, &g.Name, &isDefault); err != nil {
			continue
		}
		g.IsDefault = isDefault == 1
		result = append(result, g)
	}
	return result
}

// GroupExists reports whether a group with the given id exists.
func (s *Store) GroupExists(id string) bool {
	var one int
	return s.db.QueryRow(`SELECT 1 FROM groups WHERE id = ?`, id).Scan(&one) == nil
}

// groupIDValid returns a usable group id: the given one if the group exists,
// otherwise the default group.
func (s *Store) groupIDValid(groupID string) string {
	if groupID != "" && groupID != DefaultGroupID && s.GroupExists(groupID) {
		return groupID
	}
	return DefaultGroupID
}

// AddGroup creates a new group with the given name, positioned right after
// afterID ("": append at the end). Rejects duplicate names.
func (s *Store) AddGroup(name, afterID string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, fmt.Errorf("分组名称不能为空")
	}
	// duplicate name check
	var one int
	if err := s.db.QueryRow(`SELECT 1 FROM groups WHERE name = ?`, name).Scan(&one); err == nil {
		return Group{}, fmt.Errorf("分组名称已存在: %s", name)
	}
	// ordered ids
	rows, err := s.db.Query(`SELECT id FROM groups ORDER BY position`)
	if err != nil {
		return Group{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	insertAt := len(ids) // default: append at end
	if afterID != "" {
		for i, id := range ids {
			if id == afterID {
				insertAt = i + 1
				break
			}
		}
	}
	newID := uuid.New().String()
	ids = append(ids, "")
	copy(ids[insertAt+1:], ids[insertAt:])
	ids[insertAt] = newID

	tx, err := s.db.Begin()
	if err != nil {
		return Group{}, err
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE groups SET position = ? WHERE id = ?`, i+1, id); err != nil {
			tx.Rollback()
			return Group{}, err
		}
	}
	isDefault := 0
	if _, err := tx.Exec(`INSERT INTO groups (id, name, position, is_default) VALUES (?, ?, ?, ?)`,
		newID, name, insertAt+1, isDefault); err != nil {
		tx.Rollback()
		return Group{}, err
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	return Group{ID: newID, Name: name, IsDefault: false}, nil
}

// RenameGroup renames a group. The default group cannot be renamed.
func (s *Store) RenameGroup(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("分组名称不能为空")
	}
	if id == DefaultGroupID {
		return fmt.Errorf("默认分组不可重命名")
	}
	var one int
	if err := s.db.QueryRow(`SELECT 1 FROM groups WHERE name = ? AND id != ?`, name, id).Scan(&one); err == nil {
		return fmt.Errorf("分组名称已存在: %s", name)
	}
	res, err := s.db.Exec(`UPDATE groups SET name = ? WHERE id = ? AND is_default = 0`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("分组不存在")
	}
	return nil
}

// DeleteGroup removes a group and moves its nodes into the default group.
// The default group cannot be deleted.
func (s *Store) DeleteGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == DefaultGroupID {
		return fmt.Errorf("默认分组不可删除")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// move nodes to default group
	if _, err := tx.Exec(`UPDATE nodes SET group_id = ? WHERE group_id = ?`, DefaultGroupID, id); err != nil {
		tx.Rollback()
		return err
	}
	res, err := tx.Exec(`DELETE FROM groups WHERE id = ? AND is_default = 0`, id)
	if err != nil {
		tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		tx.Rollback()
		return fmt.Errorf("分组不存在")
	}
	// compact positions
	rows, err := tx.Query(`SELECT id FROM groups ORDER BY position`)
	if err != nil {
		tx.Rollback()
		return err
	}
	var ids []string
	for rows.Next() {
		var gid string
		if rows.Scan(&gid) == nil {
			ids = append(ids, gid)
		}
	}
	rows.Close()
	for i, gid := range ids {
		if _, err := tx.Exec(`UPDATE groups SET position = ? WHERE id = ?`, i+1, gid); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Count returns the number of stored nodes.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&count); err != nil {
		return 0
	}
	return count
}

// DBPath returns the database file path (useful for display/debug).
func (s *Store) DBPath() string {
	return s.path
}
