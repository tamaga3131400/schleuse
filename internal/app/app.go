package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"local/lowcode-gateway/internal/ui"
)

// Version der Gateway-Software, sichtbar im User-Agent und in der Verwaltung.
const Version = "0.3.0"

type Config struct {
	DataDir, AdminAddr, APIAddr, AdminOrigin, PublicURL string
	Demo                                                bool
	DemoURL                                             string
}
type bucket struct {
	at time.Time
	n  int
}
type App struct {
	Store      *Store
	Config     Config
	setup      string
	setupMu    sync.Mutex
	mu         sync.Mutex
	limits     map[string]bucket
	authSlots  chan struct{}
	proxySlots chan struct{}
	dummyHash  string
}

func New(c Config) (*App, error) {
	s, e := OpenStore(c.DataDir)
	if e != nil {
		return nil, e
	}
	a := &App{Store: s, Config: c, limits: map[string]bucket{}, authSlots: make(chan struct{}, 4), proxySlots: make(chan struct{}, 8), dummyHash: hashPassword("dummy-not-a-user-password")}
	if !s.hasAdmin() {
		p := filepath.Join(c.DataDir, "setup-code")
		b, e := os.ReadFile(p)
		if errors.Is(e, os.ErrNotExist) {
			b = []byte(randomID())
			e = os.WriteFile(p, b, 0600)
		}
		if e != nil {
			s.DB.Close()
			return nil, e
		}
		a.setup = string(b)
	}
	if c.Demo {
		if e = a.SeedDemo(); e != nil {
			s.DB.Close()
			return nil, e
		}
	}
	if c.Demo {
		a.setup = ""
		_ = os.Remove(filepath.Join(c.DataDir, "setup-code"))
	}
	return a, nil
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	send(w, status, map[string]any{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "Eingaben sind ungültig oder zu groß.")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "Genau ein JSON-Objekt erwartet.")
		return false
	}
	return true
}

var ident = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,49}$`)
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,79}$`)

func (a *App) limited(key string, max int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if len(a.limits) > 1024 {
		for k, v := range a.limits {
			if now.Sub(v.at) > time.Minute {
				delete(a.limits, k)
			}
		}
		if len(a.limits) > 2048 {
			return true
		}
	}
	b := a.limits[key]
	if now.Sub(b.at) > time.Minute {
		b = bucket{at: now}
	}
	b.n++
	a.limits[key] = b
	return b.n > max
}
func (a *App) login(w http.ResponseWriter, r *http.Request, kind string) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a.limited("login:"+ip, 10) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "Zu viele Anmeldeversuche. Bitte eine Minute warten.")
		return
	}
	select {
	case a.authSlots <- struct{}{}:
		defer func() { <-a.authSlots }()
	default:
		fail(w, 429, "Anmeldung ausgelastet. Bitte erneut versuchen.")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	var id, h, role string
	e := a.Store.DB.QueryRow("SELECT id,hash,role FROM users WHERE username=? AND active=1", in.Username).Scan(&id, &h, &role)
	if e != nil {
		h = a.dummyHash
	}
	ok := verifyPassword(in.Password, h)
	expected := "client"
	ttl := 15 * time.Minute
	if kind == "admin" {
		expected = "admin"
		ttl = 8 * time.Hour
	}
	if !ok || e != nil || role != expected {
		fail(w, 401, "Anmeldung fehlgeschlagen.")
		return
	}
	token, e := a.Store.newSession(id, kind, ttl)
	if e != nil {
		fail(w, 500, "Sitzung konnte nicht erstellt werden.")
		return
	}
	if kind == "admin" {
		http.SetCookie(w, &http.Cookie{Name: "gateway_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: strings.HasPrefix(a.Config.AdminOrigin, "https://"), MaxAge: int(ttl.Seconds())})
		send(w, 200, map[string]string{"csrf": tokenHash(token + "csrf")})
	} else {
		send(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(ttl.Seconds())})
	}
}
func (a *App) AdminHandler() http.Handler {
	assets, _ := fs.Sub(ui.Files, "dist")
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		origin, e := url.Parse(a.Config.AdminOrigin)
		if e != nil || r.Host != origin.Host {
			fail(w, 403, "Verwaltungsadresse ist nicht freigegeben.")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != a.Config.AdminOrigin {
			fail(w, 403, "Ursprung der Anfrage ist nicht freigegeben.")
			return
		}
		if r.URL.Path == "/api/status" && r.Method == "GET" {
			send(w, 200, map[string]any{"setup_required": !a.Store.hasAdmin(), "demo": a.Config.Demo, "version": Version})
			return
		}
		if r.URL.Path == "/api/setup" && r.Method == "POST" {
			a.setupMu.Lock()
			defer a.setupMu.Unlock()
			if a.Store.hasAdmin() {
				fail(w, 409, "Einrichtung bereits abgeschlossen.")
				return
			}
			var in struct{ Name, Username, Password, Code string }
			if !decode(w, r, &in) {
				return
			}
			if subtle.ConstantTimeCompare([]byte(in.Code), []byte(a.setup)) != 1 {
				fail(w, 403, "Einrichtungscode stimmt nicht.")
				return
			}
			if !ident.MatchString(in.Username) || len(in.Password) < 12 || len(in.Password) > 256 {
				fail(w, 400, "Benutzername: 2–50 Zeichen. Passwort: 12–256 Zeichen.")
				return
			}
			if e := a.Store.saveUser(User{ID: randomID(), Name: in.Name, Username: in.Username, Role: "admin", Active: true}, in.Password); e != nil {
				fail(w, 500, "Einrichtung fehlgeschlagen.")
				return
			}
			a.setup = ""
			_ = os.Remove(filepath.Join(a.Config.DataDir, "setup-code"))
			send(w, 201, map[string]bool{"ok": true})
			return
		}
		if r.URL.Path == "/api/login" && r.Method == "POST" {
			a.login(w, r, "admin")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				fail(w, 405, "Methode nicht erlaubt.")
				return
			}
			if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		cookie, e := r.Cookie("gateway_session")
		if e != nil {
			fail(w, 401, "Bitte anmelden.")
			return
		}
		u, e := a.Store.session(cookie.Value, "admin")
		if e != nil || u.Role != "admin" {
			fail(w, 401, "Sitzung abgelaufen. Bitte erneut anmelden.")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(tokenHash(cookie.Value+"csrf"))) != 1 {
			fail(w, 403, "Anfrage konnte nicht bestätigt werden.")
			return
		}
		switch {
		case r.URL.Path == "/api/session" && r.Method == "GET":
			send(w, 200, map[string]any{"user": u, "csrf": tokenHash(cookie.Value + "csrf"), "demo": a.Config.Demo, "api_url": a.Config.PublicURL})
		case r.URL.Path == "/api/logout" && r.Method == "POST":
			_, _ = a.Store.DB.Exec("DELETE FROM sessions WHERE hash=?", tokenHash(cookie.Value))
			http.SetCookie(w, &http.Cookie{Name: "gateway_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			send(w, 200, map[string]bool{"ok": true})
		case r.URL.Path == "/api/state" && r.Method == "GET":
			c, e := a.Store.connections()
			if e != nil {
				fail(w, 500, "Speicherfehler.")
				return
			}
			actions, e := a.Store.actions()
			if e != nil {
				fail(w, 500, "Speicherfehler.")
				return
			}
			users, e := a.Store.users()
			if e != nil {
				fail(w, 500, "Speicherfehler.")
				return
			}
			events, e := a.Store.events()
			if e != nil {
				fail(w, 500, "Speicherfehler.")
				return
			}
			tokens, e := a.Store.integrationTokens()
			if e != nil {
				fail(w, 500, "Speicherfehler.")
				return
			}
			send(w, 200, map[string]any{"connections": c, "actions": actions, "users": users, "events": events, "integration_tokens": tokens})
		case strings.HasPrefix(r.URL.Path, "/api/connections"):
			a.manageConnection(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/actions"):
			a.manageAction(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/users"):
			a.manageUser(w, r)
		case r.URL.Path == "/api/integration-tokens" || strings.HasPrefix(r.URL.Path, "/api/integration-tokens/"):
			a.manageIntegrationToken(w, r)
		case r.URL.Path == "/api/templates" && r.Method == "GET":
			templates, e := connectorTemplates()
			if e != nil {
				fail(w, 500, "Vorlagen konnten nicht geladen werden.")
				return
			}
			send(w, 200, map[string]any{"templates": templates})
		case r.URL.Path == "/api/export" && r.Method == "GET":
			c, _ := a.Store.connections()
			acts, _ := a.Store.actions()
			send(w, 200, map[string]any{"format": "gateway-metadata-v1", "connections": c, "actions": acts, "note": "Keine Zugangsdaten. Kein vollständiges Backup."})
		default:
			http.NotFound(w, r)
		}
	})
}
func (a *App) APIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == "GET" {
			send(w, 200, map[string]string{"status": "ok"})
			return
		}
		if r.URL.Path == "/auth/login" && r.Method == "POST" {
			a.login(w, r, "client")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/actions/") {
			http.NotFound(w, r)
			return
		}
		auth := r.Header.Values("Authorization")
		if len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer ") {
			fail(w, 401, "Benutzer-Access-Token erforderlich.")
			return
		}
		bearer := strings.TrimPrefix(auth[0], "Bearer ")
		slug := strings.TrimPrefix(r.URL.Path, "/actions/")
		var id, tenant string
		isIntegration := strings.HasPrefix(bearer, integrationPrefix)
		if isIntegration {
			tokenID, actionID, err := a.Store.integrationScope(bearer, slug)
			if errors.Is(err, errTokenInvalid) {
				fail(w, 401, "Token ungültig, abgelaufen oder widerrufen.")
				return
			}
			if a.apiLimit(w, "integration:"+tokenID) {
				return
			}
			if err != nil {
				fail(w, 403, "Diese Aktion ist für den Token nicht freigegeben.")
				return
			}
			id = actionID
		} else {
			u, err := a.Store.session(bearer, "client")
			if err != nil || u.Role != "client" {
				fail(w, 401, "Zugang abgelaufen oder gesperrt.")
				return
			}
			if a.apiLimit(w, "api:"+u.ID) {
				return
			}
			tenant = u.Tenant
			if err = a.Store.DB.QueryRow("SELECT action_id FROM grants g JOIN actions a ON a.id=g.action_id WHERE g.user_id=? AND a.slug=?", u.ID, slug).Scan(&id); err != nil {
				fail(w, 403, "Diese Aktion ist nicht freigegeben.")
				return
			}
		}
		act, e := a.Store.action(id)
		if e == nil && act.Slug != slug {
			fail(w, 403, "Diese Aktion ist nicht freigegeben.")
			return
		}
		if e != nil || !act.Active {
			fail(w, 403, "Aktion ist nicht aktiv.")
			return
		}
		conn, e := a.Store.connection(act.ConnectionID)
		if e != nil || (!isIntegration && tenant != conn.Tenant) {
			fail(w, 403, "Kundenzuordnung stimmt nicht.")
			return
		}
		a.forward(w, r, act, conn)
	})
}

func (a *App) apiLimit(w http.ResponseWriter, key string) bool {
	if !a.limited(key, 60) {
		return false
	}
	w.Header().Set("Retry-After", "60")
	fail(w, 429, "Abruflimit für diesen Zugang erreicht.")
	return true
}
