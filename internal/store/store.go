// Package store membungkus akses SQLite: session, preferensi user, bookmark,
// alert threshold, registrasi Docker stack, dan log aktivitas.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS activity_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL,
  event_type TEXT NOT NULL,
  action TEXT,
  detail TEXT,
  ip_address TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_activity_event ON activity_log(event_type, id DESC);

CREATE TABLE IF NOT EXISTS bookmarks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_bookmarks_user ON bookmarks(username);

CREATE TABLE IF NOT EXISTS alert_thresholds (
  metric TEXT PRIMARY KEY,
  warn_pct INTEGER NOT NULL DEFAULT 75,
  crit_pct INTEGER NOT NULL DEFAULT 90,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS docker_stacks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  compose_path TEXT NOT NULL,
  description TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_preferences (
  username TEXT PRIMARY KEY,
  dashboard_layout TEXT,
  polling_interval_ms INTEGER DEFAULT 1000,
  file_manager_view TEXT DEFAULT 'grid',
  timezone TEXT DEFAULT '',
  language TEXT DEFAULT 'id',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS file_operations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL,
  operation TEXT NOT NULL,
  source_path TEXT,
  dest_path TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_fileops_id ON file_operations(id DESC);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  sudo INTEGER NOT NULL DEFAULT 0,
  home TEXT,
  ip_address TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  expires_at DATETIME NOT NULL
);
`

// Ambang default per metrik. Network sengaja tidak ada di sini: tanpa deteksi
// kecepatan link yang konsisten, tidak ada baseline persentase yang wajar
// untuk throughput.
var defaultThresholds = map[string][2]int{
	"cpu":     {75, 90},
	"ram":     {75, 90},
	"storage": {80, 92},
	"gpu":     {75, 90},
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	// WAL + busy_timeout: satu proses penulis, banyak goroutine pembaca.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrasi skema: %w", err)
	}
	// DB yang dibuat versi lama tidak punya kolom baru; CREATE TABLE IF NOT
	// EXISTS tidak menambahkannya. Tambahkan satu per satu dan abaikan error
	// "duplicate column" supaya migrasi aman dijalankan berulang.
	for _, kolom := range []string{
		`ALTER TABLE user_preferences ADD COLUMN timezone TEXT DEFAULT ''`,
		`ALTER TABLE user_preferences ADD COLUMN language TEXT DEFAULT 'id'`,
	} {
		if _, err := db.Exec(kolom); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrasi kolom preferensi: %w", err)
		}
	}

	s := &Store{db: db}
	for metric, v := range defaultThresholds {
		if _, err := db.Exec(
			`INSERT OR IGNORE INTO alert_thresholds(metric, warn_pct, crit_pct) VALUES(?,?,?)`,
			metric, v[0], v[1]); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---- sessions ----

type Session struct {
	ID       string
	Username string
	Sudo     bool
	Home     string
	Expires  time.Time
}

func newID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateSession(username, home, ip string, sudo bool, ttl time.Duration) (Session, error) {
	sess := Session{ID: newID(), Username: username, Sudo: sudo, Home: home, Expires: time.Now().Add(ttl)}
	_, err := s.db.Exec(
		`INSERT INTO sessions(id, username, sudo, home, ip_address, expires_at) VALUES(?,?,?,?,?,?)`,
		sess.ID, username, boolInt(sudo), home, ip, sess.Expires)
	return sess, err
}

func (s *Store) GetSession(id string) (Session, bool) {
	var sess Session
	var sudo int
	err := s.db.QueryRow(
		`SELECT id, username, sudo, home, expires_at FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Username, &sudo, &sess.Home, &sess.Expires)
	if err != nil {
		return Session{}, false
	}
	if time.Now().After(sess.Expires) {
		_ = s.DeleteSession(id)
		return Session{}, false
	}
	sess.Sudo = sudo == 1
	return sess, true
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) PurgeExpiredSessions() {
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP`)
}

// ---- activity log ----

type ActivityEntry struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	EventType string `json:"event_type"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	IP        string `json:"ip_address"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) LogActivity(username, eventType, action string, detail any, ip string) {
	var d string
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			d = string(b)
		}
	}
	_, _ = s.db.Exec(
		`INSERT INTO activity_log(username, event_type, action, detail, ip_address) VALUES(?,?,?,?,?)`,
		username, eventType, action, d, ip)
}

// LoginEventTypes adalah event yang ditampilkan di menu Logs → Activity Logs.
var LoginEventTypes = []string{"login_success", "login_failed", "logout"}

func (s *Store) Activity(eventTypes []string, username string, limit, offset int) ([]ActivityEntry, error) {
	q := `SELECT id, username, event_type, COALESCE(action,''), COALESCE(detail,''), COALESCE(ip_address,''), created_at FROM activity_log WHERE 1=1`
	var args []any
	if len(eventTypes) > 0 {
		q += ` AND event_type IN (` + placeholders(len(eventTypes)) + `)`
		for _, t := range eventTypes {
			args = append(args, t)
		}
	}
	if username != "" {
		q += ` AND username = ?`
		args = append(args, username)
	}
	q += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEntry{}
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.Username, &e.EventType, &e.Action, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}

// ---- file operations ----

type FileOp struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Operation string `json:"operation"`
	Source    string `json:"source_path"`
	Dest      string `json:"dest_path"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) LogFileOp(username, op, src, dst string) {
	_, _ = s.db.Exec(
		`INSERT INTO file_operations(username, operation, source_path, dest_path) VALUES(?,?,?,?)`,
		username, op, src, dst)
}

func (s *Store) FileOps(username string, limit, offset int) ([]FileOp, error) {
	q := `SELECT id, username, operation, COALESCE(source_path,''), COALESCE(dest_path,''), created_at FROM file_operations`
	var args []any
	if username != "" {
		q += ` WHERE username = ?`
		args = append(args, username)
	}
	q += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileOp{}
	for rows.Next() {
		var f FileOp
		if err := rows.Scan(&f.ID, &f.Username, &f.Operation, &f.Source, &f.Dest, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- bookmarks ----

type Bookmark struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

func (s *Store) Bookmarks(username string) ([]Bookmark, error) {
	rows, err := s.db.Query(`SELECT id, username, name, path FROM bookmarks WHERE username = ? ORDER BY name`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bookmark{}
	for rows.Next() {
		var b Bookmark
		if err := rows.Scan(&b.ID, &b.Username, &b.Name, &b.Path); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) AddBookmark(username, name, path string) (Bookmark, error) {
	res, err := s.db.Exec(`INSERT INTO bookmarks(username, name, path) VALUES(?,?,?)`, username, name, path)
	if err != nil {
		return Bookmark{}, err
	}
	id, _ := res.LastInsertId()
	return Bookmark{ID: id, Username: username, Name: name, Path: path}, nil
}

func (s *Store) UpdateBookmark(username string, id int64, name, path string) error {
	res, err := s.db.Exec(`UPDATE bookmarks SET name = ?, path = ? WHERE id = ? AND username = ?`, name, path, id, username)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) DeleteBookmark(username string, id int64) error {
	res, err := s.db.Exec(`DELETE FROM bookmarks WHERE id = ? AND username = ?`, id, username)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func mustAffect(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ---- alert thresholds ----

type Threshold struct {
	Metric string `json:"metric"`
	Warn   int    `json:"warn_pct"`
	Crit   int    `json:"crit_pct"`
}

func (s *Store) Thresholds() ([]Threshold, error) {
	rows, err := s.db.Query(`SELECT metric, warn_pct, crit_pct FROM alert_thresholds ORDER BY metric`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Threshold{}
	for rows.Next() {
		var t Threshold
		if err := rows.Scan(&t.Metric, &t.Warn, &t.Crit); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ValidateThreshold dipisah dari SetThreshold supaya pemanggil bisa memvalidasi
// seluruh daftar sebelum satu pun ditulis.
func (s *Store) ValidateThreshold(t Threshold) error {
	if _, ok := defaultThresholds[t.Metric]; !ok {
		return fmt.Errorf("metrik %q tidak punya alert threshold", t.Metric)
	}
	if t.Warn < 1 || t.Warn > 100 || t.Crit < 1 || t.Crit > 100 || t.Warn >= t.Crit {
		return fmt.Errorf("ambang tidak valid: warn harus < crit, keduanya 1-100")
	}
	return nil
}

func (s *Store) SetThreshold(t Threshold) error {
	if err := s.ValidateThreshold(t); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`UPDATE alert_thresholds SET warn_pct = ?, crit_pct = ?, updated_at = CURRENT_TIMESTAMP WHERE metric = ?`,
		t.Warn, t.Crit, t.Metric)
	return err
}

// ---- preferensi user ----

type Preferences struct {
	Username        string `json:"username"`
	PollingInterval int    `json:"polling_interval_ms"`
	FileManagerView string `json:"file_manager_view"`
	DashboardLayout string `json:"dashboard_layout,omitempty"`
	// Timezone kosong = ikut zona waktu server (perilaku sebelum fitur ini).
	Timezone string `json:"timezone"`
	Language string `json:"language"`
}

func (s *Store) Preferences(username string) Preferences {
	p := Preferences{Username: username, PollingInterval: 1000, FileManagerView: "grid", Language: "id"}
	var layout, tz, lang sql.NullString
	err := s.db.QueryRow(
		`SELECT polling_interval_ms, file_manager_view, dashboard_layout, timezone, language
		 FROM user_preferences WHERE username = ?`,
		username).Scan(&p.PollingInterval, &p.FileManagerView, &layout, &tz, &lang)
	if err != nil {
		return p
	}
	p.DashboardLayout = layout.String
	p.Timezone = tz.String
	if lang.String != "" {
		p.Language = lang.String
	}
	return p
}

func (s *Store) SavePreferences(p Preferences) error {
	if p.PollingInterval < 250 || p.PollingInterval > 60000 {
		return fmt.Errorf("interval polling harus antara 250ms dan 60000ms")
	}
	if p.FileManagerView != "grid" && p.FileManagerView != "list" {
		return fmt.Errorf("tampilan file manager harus grid atau list")
	}
	if p.Language != "" && p.Language != "id" && p.Language != "en" {
		return fmt.Errorf("bahasa harus id atau en")
	}
	if p.Language == "" {
		p.Language = "id"
	}
	// Zona waktu divalidasi terhadap database zona waktu sistem, bukan daftar
	// buatan sendiri: nama yang tidak dikenal akan membuat tampilan jam jatuh
	// diam-diam ke UTC di browser.
	if p.Timezone != "" {
		if _, err := time.LoadLocation(p.Timezone); err != nil {
			return fmt.Errorf("zona waktu %q tidak dikenal", p.Timezone)
		}
	}
	_, err := s.db.Exec(`
INSERT INTO user_preferences(username, polling_interval_ms, file_manager_view, dashboard_layout, timezone, language, updated_at)
VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP)
ON CONFLICT(username) DO UPDATE SET
  polling_interval_ms = excluded.polling_interval_ms,
  file_manager_view   = excluded.file_manager_view,
  dashboard_layout    = excluded.dashboard_layout,
  timezone            = excluded.timezone,
  language            = excluded.language,
  updated_at          = CURRENT_TIMESTAMP`,
		p.Username, p.PollingInterval, p.FileManagerView, p.DashboardLayout, p.Timezone, p.Language)
	return err
}

// ---- docker stacks ----

type Stack struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ComposePath string `json:"compose_path"`
	Description string `json:"description"`
}

func (s *Store) Stacks() ([]Stack, error) {
	rows, err := s.db.Query(`SELECT id, name, compose_path, COALESCE(description,'') FROM docker_stacks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Stack{}
	for rows.Next() {
		var st Stack
		if err := rows.Scan(&st.ID, &st.Name, &st.ComposePath, &st.Description); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) Stack(id int64) (Stack, error) {
	var st Stack
	err := s.db.QueryRow(
		`SELECT id, name, compose_path, COALESCE(description,'') FROM docker_stacks WHERE id = ?`, id,
	).Scan(&st.ID, &st.Name, &st.ComposePath, &st.Description)
	return st, err
}

func (s *Store) AddStack(name, path, desc string) (Stack, error) {
	res, err := s.db.Exec(`INSERT INTO docker_stacks(name, compose_path, description) VALUES(?,?,?)`, name, path, desc)
	if err != nil {
		return Stack{}, err
	}
	id, _ := res.LastInsertId()
	return Stack{ID: id, Name: name, ComposePath: path, Description: desc}, nil
}

func (s *Store) UpdateStack(id int64, name, path, desc string) error {
	res, err := s.db.Exec(`UPDATE docker_stacks SET name = ?, compose_path = ?, description = ? WHERE id = ?`,
		name, path, desc, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) DeleteStack(id int64) error {
	res, err := s.db.Exec(`DELETE FROM docker_stacks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
