package installer

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Port of backend/agents/install_command_store.py: short-lived installer
// snapshots shared by server restarts (DATA_DIR/install-commands/snapshots.sqlite3).

const (
	// TTLSeconds is TTL_SECONDS.
	TTLSeconds = 24 * 60 * 60
	// MaxSnapshots is MAX_SNAPSHOTS.
	MaxSnapshots = 128
	// MaxScriptBytes is MAX_SCRIPT_BYTES.
	MaxScriptBytes = 512 * 1024
)

// ErrStoreFull is InstallCommandStoreFull: existing unexpired links stay
// valid instead of being evicted.
var ErrStoreFull = errors.New("InstallCommandStoreFull")

// Store is the install command store of one data directory.
type Store struct {
	DataDir string
	// Now returns the current Unix time (int(time.time())); nil is the clock.
	Now func() int64
	// Token returns a new token (secrets.token_urlsafe(16)); nil is crypto/rand.
	Token func() string
	// Fingerprint is generator_fingerprint(); "" is GeneratorFingerprint.
	Fingerprint string
}

var storeMu sync.Mutex

func (s *Store) now() int64 {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().Unix()
}

func (s *Store) fingerprint() string {
	if s.Fingerprint != "" {
		return s.Fingerprint
	}
	return GeneratorFingerprint
}

// tokenURLSafe is secrets.token_urlsafe(16): 22 base64url characters.
func tokenURLSafe() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DatabasePath is _database_path().
func (s *Store) DatabasePath() string {
	return filepath.Join(s.DataDir, "install-commands", "snapshots.sqlite3")
}

type storeConn struct {
	db   *sql.DB
	conn *sql.Conn
}

func (c *storeConn) close() {
	c.conn.Close()
	c.db.Close()
}

// open is _database(): private directory and file, the table (with the
// generator column), BEGIN IMMEDIATE and the removal of expired links.
func (s *Store) open(ctx context.Context) (*storeConn, error) {
	path := s.DatabasePath()
	dir := filepath.Dir(path)
	// Path.mkdir(mode=0o700, parents=True, exist_ok=True): missing parents get
	// the default mode, only the directory itself 0700.
	if err := os.MkdirAll(filepath.Dir(dir), 0o777); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			return nil, err
		}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	// Create privately before sqlite opens it, even with a permissive umask.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	file.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String() + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	c := &storeConn{db: db, conn: conn}
	if _, err := conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS snapshots "+
		"(token TEXT PRIMARY KEY, script TEXT NOT NULL, expires_at INTEGER NOT NULL)"); err != nil {
		c.close()
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, "PRAGMA table_info(snapshots)")
	if err != nil {
		c.close()
		return nil, err
	}
	hasGenerator := false
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			c.close()
			return nil, err
		}
		if name == "generator" {
			hasGenerator = true
		}
	}
	rows.Close()
	// Links saved before this column existed came from an older version.
	if !hasGenerator {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE snapshots ADD COLUMN generator TEXT"); err != nil {
			c.close()
			return nil, err
		}
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		c.close()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "DELETE FROM snapshots WHERE expires_at <= ?", s.now()); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		c.close()
		return nil, err
	}
	return c, nil
}

// SaveInstallScript is save_install_script(script, expires_at=expiresAt):
// persist the text; only a random capability is public. expiresAt nil is
// now + 24 h; otherwise it must lie within the next 24 hours.
func (s *Store) SaveInstallScript(ctx context.Context, script string, expiresAt *int64) (string, int64, error) {
	if len(script) > MaxScriptBytes {
		return "", 0, valueError("安装脚本过大，请减少自定义参数后重试。")
	}
	token := ""
	if s.Token != nil {
		token = s.Token()
	} else {
		token = tokenURLSafe()
	}
	now := s.now()
	var expires int64
	if expiresAt == nil {
		expires = now + TTLSeconds
	} else if !(now < *expiresAt && *expiresAt <= now+TTLSeconds) {
		return "", 0, valueError("安装链接有效期必须在未来 24 小时内。")
	} else {
		expires = *expiresAt
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	c, err := s.open(ctx)
	if err != nil {
		return "", 0, err
	}
	defer c.close()
	var count int64
	if err := c.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM snapshots").Scan(&count); err != nil {
		c.conn.ExecContext(ctx, "ROLLBACK")
		return "", 0, err
	}
	if count >= MaxSnapshots {
		c.conn.ExecContext(ctx, "ROLLBACK")
		return "", 0, ErrStoreFull
	}
	if _, err := c.conn.ExecContext(ctx, "INSERT INTO snapshots (token, script, expires_at, generator) VALUES (?, ?, ?, ?)",
		token, script, expires, s.fingerprint()); err != nil {
		c.conn.ExecContext(ctx, "ROLLBACK")
		return "", 0, err
	}
	if _, err := c.conn.ExecContext(ctx, "COMMIT"); err != nil {
		return "", 0, err
	}
	return token, expires, nil
}

// isToken is _TOKEN.fullmatch(token): [A-Za-z0-9_-]{22}.
func isToken(token string) bool { return len(token) == 22 && matchesClass(token, "_-") }

// LoadInstallScript is load_install_script(token): the script, StaleScript
// for a link an older generator made, or ok false when unknown or expired.
func (s *Store) LoadInstallScript(ctx context.Context, token string) (string, bool, error) {
	if !isToken(token) {
		return "", false, nil
	}
	if info, err := os.Stat(s.DatabasePath()); err != nil || !info.Mode().IsRegular() {
		return "", false, nil
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	c, err := s.open(ctx)
	if err != nil {
		return "", false, err
	}
	defer c.close()
	var script string
	var generator sql.NullString
	err = c.conn.QueryRowContext(ctx, "SELECT script, generator FROM snapshots WHERE token = ?", token).Scan(&script, &generator)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = c.conn.ExecContext(ctx, "COMMIT")
		return "", false, err
	}
	if err != nil {
		c.conn.ExecContext(ctx, "ROLLBACK")
		return "", false, err
	}
	if _, err := c.conn.ExecContext(ctx, "COMMIT"); err != nil {
		return "", false, err
	}
	if generator.Valid && generator.String == s.fingerprint() {
		return script, true, nil
	}
	return StaleScript, true, nil
}
