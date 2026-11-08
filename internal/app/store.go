package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

type Connection struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Tenant       string        `json:"tenant"`
	URL          string        `json:"url"`
	Auth         string        `json:"auth"` // "", "header", "basic" oder "query"; leer = aus Header abgeleitet
	Header       string        `json:"header"`
	Prefix       string        `json:"prefix"`
	Headers      []HeaderValue `json:"headers"`
	AllowPrivate bool          `json:"allow_private"`
	SecretSet    bool          `json:"secret_set"`
}
type HeaderValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// authMode liefert die Anmeldeart, auch für Verbindungen aus Version 0.2.
func (c Connection) authMode() string {
	if c.Header == "" {
		return "none"
	}
	if c.Auth == "" {
		return "header"
	}
	return c.Auth
}

type Action struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	ConnectionID string   `json:"connection_id"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	Fields       []string `json:"fields"`
	Query        []string `json:"query"`
	BodyTemplate string   `json:"body_template"`
	BodyFormat   string   `json:"body_format"` // "" = JSON, "form" = application/x-www-form-urlencoded
	Active       bool     `json:"active"`
}
type User struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Username string   `json:"username"`
	Tenant   string   `json:"tenant"`
	Role     string   `json:"role"`
	Active   bool     `json:"active"`
	Grants   []string `json:"grants"`
}
type Event struct {
	ID       int64  `json:"id"`
	At       string `json:"at"`
	Action   string `json:"action"`
	Source   string `json:"source"`
	Status   int    `json:"status"`
	Duration int64  `json:"duration_ms"`
}
type IntegrationToken struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	ActionIDs []string `json:"action_ids"`
	ExpiresAt int64    `json:"expires_at"` // 0 = unbegrenzt bis zum Widerruf
}
type Store struct {
	DB   *sql.DB
	aead cipher.AEAD
}

func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func tokenHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "master.key")
	if external := os.Getenv("GATEWAY_MASTER_KEY_FILE"); external != "" {
		keyPath = external
	}
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) && os.Getenv("GATEWAY_MASTER_KEY_FILE") == "" {
		if _, existsErr := os.Stat(filepath.Join(dir, "gateway.db")); existsErr == nil {
			return nil, errors.New("Datenbank vorhanden, Master-Key fehlt. Passenden Schlüssel aus Sicherung wiederherstellen")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, err = f.Write(key)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return nil, fmt.Errorf("Master-Key nicht lesbar: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("Master-Key muss genau 32 Bytes enthalten")
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("Master-Key darf nur für den Dateieigentümer lesbar sein (0600)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "gateway.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS connections(id TEXT PRIMARY KEY, config TEXT NOT NULL, secret BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS actions(id TEXT PRIMARY KEY, slug TEXT UNIQUE NOT NULL, config TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, name TEXT NOT NULL, username TEXT UNIQUE NOT NULL, hash TEXT NOT NULL, tenant TEXT NOT NULL, role TEXT NOT NULL, active INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS grants(user_id TEXT REFERENCES users(id) ON DELETE CASCADE, action_id TEXT REFERENCES actions(id) ON DELETE CASCADE, PRIMARY KEY(user_id,action_id));
 CREATE TABLE IF NOT EXISTS sessions(hash TEXT PRIMARY KEY,user_id TEXT REFERENCES users(id) ON DELETE CASCADE,kind TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS integration_tokens(id TEXT PRIMARY KEY,hash TEXT UNIQUE NOT NULL,name TEXT NOT NULL,action_id TEXT NOT NULL REFERENCES actions(id) ON DELETE CASCADE,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS integration_token_actions(token_id TEXT NOT NULL REFERENCES integration_tokens(id) ON DELETE CASCADE,action_id TEXT NOT NULL REFERENCES actions(id) ON DELETE CASCADE,PRIMARY KEY(token_id,action_id));
 INSERT OR IGNORE INTO integration_token_actions(token_id,action_id) SELECT id,action_id FROM integration_tokens;
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY, at TEXT NOT NULL, action TEXT NOT NULL, source TEXT NOT NULL, status INTEGER NOT NULL, duration INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(filepath.Join(dir, "gateway.db"), 0600); err != nil {
		db.Close()
		return nil, err
	}
	rows, e := db.Query("SELECT id,secret FROM connections")
	if e != nil {
		db.Close()
		return nil, e
	}
	for rows.Next() {
		var id string
		var data []byte
		if e = rows.Scan(&id, &data); e != nil {
			rows.Close()
			db.Close()
			return nil, e
		}
		n := aead.NonceSize()
		if len(data) < n {
			rows.Close()
			db.Close()
			return nil, errors.New("Beschädigte Zugangsdaten")
		}
		if _, e = aead.Open(nil, data[:n], data[n:], []byte(id)); e != nil {
			rows.Close()
			db.Close()
			return nil, errors.New("Master-Key passt nicht zu gespeicherten Zugangsdaten oder Datenbank ist beschädigt")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db, aead}, nil
}
func (s *Store) seal(id, secret string) []byte {
	n := make([]byte, s.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		panic(e)
	}
	return s.aead.Seal(n, n, []byte(secret), []byte(id))
}
func (s *Store) secret(id string) (string, error) {
	var b []byte
	if err := s.DB.QueryRow("SELECT secret FROM connections WHERE id=?", id).Scan(&b); err != nil {
		return "", err
	}
	n := s.aead.NonceSize()
	if len(b) < n {
		return "", errors.New("Ungültige Zugangsdaten")
	}
	p, err := s.aead.Open(nil, b[:n], b[n:], []byte(id))
	return string(p), err
}
func hashPassword(p string) string {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		panic(e)
	}
	key := argon2.IDKey([]byte(p), salt, 2, 32*1024, 1, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(key)
}
func verifyPassword(p, h string) bool {
	parts := strings.Split(h, ":")
	if len(parts) != 2 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[0])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[1])
	if e != nil || len(expected) != 32 {
		return false
	}
	key := argon2.IDKey([]byte(p), salt, 2, 32*1024, 1, 32)
	return subtle.ConstantTimeCompare(key, expected) == 1
}
func (s *Store) hasAdmin() bool {
	var n int
	_ = s.DB.QueryRow("SELECT count(*) FROM users WHERE role='admin'").Scan(&n)
	return n > 0
}
func (s *Store) connection(id string) (Connection, error) {
	var c Connection
	var raw string
	err := s.DB.QueryRow("SELECT config FROM connections WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &c)
	}
	return c, err
}
func (s *Store) connections() ([]Connection, error) {
	rows, e := s.DB.Query("SELECT config FROM connections ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	all := []Connection{}
	for rows.Next() {
		var raw string
		var c Connection
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &c); e != nil {
			return nil, e
		}
		all = append(all, c)
	}
	return all, rows.Err()
}
func (s *Store) saveConnection(c Connection, secret string) error {
	if c.ID == "" {
		c.ID = randomID()
	}
	var encrypted []byte
	if c.Header == "" || secret != "" {
		encrypted = s.seal(c.ID, secret)
	} else {
		if e := s.DB.QueryRow("SELECT secret FROM connections WHERE id=?", c.ID).Scan(&encrypted); e != nil {
			return errors.New("Für neue Verbindungen ist ein Schlüssel erforderlich")
		}
	}
	c.SecretSet = c.Header != ""
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO connections(id,config,secret) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET config=excluded.config,secret=excluded.secret", c.ID, string(b), encrypted)
	return e
}
func (s *Store) actions() ([]Action, error) {
	rows, e := s.DB.Query("SELECT config FROM actions ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	all := []Action{}
	for rows.Next() {
		var b string
		var a Action
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &a); e != nil {
			return nil, e
		}
		all = append(all, a)
	}
	return all, rows.Err()
}
func (s *Store) action(id string) (Action, error) {
	var raw string
	var a Action
	e := s.DB.QueryRow("SELECT config FROM actions WHERE id=?", id).Scan(&raw)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &a)
	}
	return a, e
}
func (s *Store) saveAction(a Action) error {
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO actions(id,slug,config) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET slug=excluded.slug,config=excluded.config", a.ID, a.Slug, string(b))
	return e
}
func (s *Store) user(id string) (User, error) {
	var u User
	e := s.DB.QueryRow("SELECT id,name,username,tenant,role,active FROM users WHERE id=?", id).Scan(&u.ID, &u.Name, &u.Username, &u.Tenant, &u.Role, &u.Active)
	return u, e
}
func (s *Store) users() ([]User, error) {
	rows, e := s.DB.Query("SELECT id,name,username,tenant,role,active FROM users ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	all := []User{}
	for rows.Next() {
		var u User
		if e = rows.Scan(&u.ID, &u.Name, &u.Username, &u.Tenant, &u.Role, &u.Active); e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, u)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range all {
		all[i].Grants = []string{}
		rs, e := s.DB.Query("SELECT action_id FROM grants WHERE user_id=?", all[i].ID)
		if e != nil {
			return nil, e
		}
		for rs.Next() {
			var g string
			if e = rs.Scan(&g); e != nil {
				rs.Close()
				return nil, e
			}
			all[i].Grants = append(all[i].Grants, g)
		}
		rs.Close()
	}
	return all, nil
}
func (s *Store) saveUser(u User, password string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT INTO users(id,name,username,hash,tenant,role,active) VALUES(?,?,?,?,?,?,?)", u.ID, u.Name, u.Username, hashPassword(password), u.Tenant, u.Role, u.Active)
	if e != nil {
		return e
	}
	for _, a := range u.Grants {
		if _, e = tx.Exec("INSERT INTO grants(user_id,action_id) VALUES(?,?)", u.ID, a); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) newSession(id, kind string, ttl time.Duration) (string, error) {
	t := randomID()
	_, e := s.DB.Exec("DELETE FROM sessions WHERE expires<?", time.Now().Unix())
	if e != nil {
		return "", e
	}
	_, e = s.DB.Exec("INSERT INTO sessions VALUES(?,?,?,?)", tokenHash(t), id, kind, time.Now().Add(ttl).Unix())
	return t, e
}
func (s *Store) session(t, kind string) (User, error) {
	var id string
	e := s.DB.QueryRow("SELECT s.user_id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.hash=? AND s.kind=? AND s.expires>? AND u.active=1", tokenHash(t), kind, time.Now().Unix()).Scan(&id)
	if e != nil {
		return User{}, e
	}
	return s.user(id)
}

const integrationPrefix = "slt_"

var (
	errTokenInvalid    = errors.New("Token ungültig, abgelaufen oder widerrufen")
	errTokenNotGranted = errors.New("Aktion für diesen Token nicht freigegeben")
)

// newIntegrationToken erstellt einen Token für eine oder mehrere Aktionen; days=0 bedeutet unbegrenzt.
func (s *Store) newIntegrationToken(name string, actionIDs []string, days int) (IntegrationToken, string, error) {
	token := IntegrationToken{ID: randomID(), Name: name, ActionIDs: actionIDs}
	if days > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}
	secret := integrationPrefix + randomID()
	tx, err := s.DB.Begin()
	if err != nil {
		return token, "", err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO integration_tokens(id,hash,name,action_id,expires) VALUES(?,?,?,?,?)",
		token.ID, tokenHash(secret), token.Name, actionIDs[0], token.ExpiresAt); err != nil {
		return token, "", err
	}
	for _, id := range actionIDs {
		if _, err = tx.Exec("INSERT INTO integration_token_actions(token_id,action_id) VALUES(?,?)", token.ID, id); err != nil {
			return token, "", err
		}
	}
	return token, secret, tx.Commit()
}

func (s *Store) integrationTokens() ([]IntegrationToken, error) {
	rows, err := s.DB.Query("SELECT id,name,expires FROM integration_tokens ORDER BY rowid DESC")
	if err != nil {
		return nil, err
	}
	tokens := []IntegrationToken{}
	for rows.Next() {
		token := IntegrationToken{ActionIDs: []string{}}
		if err := rows.Scan(&token.ID, &token.Name, &token.ExpiresAt); err != nil {
			rows.Close()
			return nil, err
		}
		tokens = append(tokens, token)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range tokens {
		grants, err := s.DB.Query("SELECT action_id FROM integration_token_actions WHERE token_id=? ORDER BY rowid", tokens[i].ID)
		if err != nil {
			return nil, err
		}
		for grants.Next() {
			var id string
			if err := grants.Scan(&id); err != nil {
				grants.Close()
				return nil, err
			}
			tokens[i].ActionIDs = append(tokens[i].ActionIDs, id)
		}
		grants.Close()
	}
	return tokens, nil
}

// integrationScope prüft Token und Freigabe für eine Aktion.
func (s *Store) integrationScope(secret, slug string) (tokenID, actionID string, err error) {
	if err = s.DB.QueryRow("SELECT id FROM integration_tokens WHERE hash=? AND (expires=0 OR expires>?)",
		tokenHash(secret), time.Now().Unix()).Scan(&tokenID); err != nil {
		return "", "", errTokenInvalid
	}
	if err = s.DB.QueryRow("SELECT a.id FROM integration_token_actions g JOIN actions a ON a.id=g.action_id WHERE g.token_id=? AND a.slug=?",
		tokenID, slug).Scan(&actionID); err != nil {
		return tokenID, "", errTokenNotGranted
	}
	return tokenID, actionID, nil
}

// deleteAction entfernt eine Aktion. Tokens mit weiteren Aktionen bleiben erhalten.
func (s *Store) deleteAction(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE integration_tokens SET action_id=(SELECT g.action_id FROM integration_token_actions g WHERE g.token_id=integration_tokens.id AND g.action_id<>?1 LIMIT 1)
 WHERE action_id=?1 AND EXISTS(SELECT 1 FROM integration_token_actions g WHERE g.token_id=integration_tokens.id AND g.action_id<>?1)`, id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM actions WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) event(a, source string, status int, d time.Duration) {
	_, _ = s.DB.Exec("INSERT INTO events(at,action,source,status,duration) VALUES(?,?,?,?,?)", time.Now().UTC().Format(time.RFC3339), a, source, status, d.Milliseconds())
	_, _ = s.DB.Exec("DELETE FROM events WHERE id < (SELECT coalesce(max(id),0)-999 FROM events)")
}
func (s *Store) events() ([]Event, error) {
	rs, e := s.DB.Query("SELECT id,at,action,source,status,duration FROM events ORDER BY id DESC LIMIT 60")
	if e != nil {
		return nil, e
	}
	defer rs.Close()
	all := []Event{}
	for rs.Next() {
		var v Event
		if e = rs.Scan(&v.ID, &v.At, &v.Action, &v.Source, &v.Status, &v.Duration); e != nil {
			return nil, e
		}
		all = append(all, v)
	}
	return all, rs.Err()
}
