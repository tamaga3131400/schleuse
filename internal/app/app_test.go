package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func invoke(h http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	var b io.Reader
	if body != nil {
		x, _ := json.Marshal(body)
		b = bytes.NewReader(x)
	}
	r := httptest.NewRequest(method, "http://gateway"+path, b)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func expect(t *testing.T, r *httptest.ResponseRecorder, status int) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("HTTP %d, erwartet %d: %s", r.Code, status, r.Body.String())
	}
}
func TestGatewayIsolationAndSecrets(t *testing.T) {
	secret := "upstream-secret-only-on-server"
	var receivedAuth, receivedCookie, receivedBody string
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		payload, _ := io.ReadAll(r.Body)
		receivedBody = string(payload)
		receivedAuth = r.Header.Get("Authorization")
		receivedCookie = r.Header.Get("Cookie")
		w.Header().Set("Set-Cookie", "private=upstream-session")
		w.Header().Set("X-Secret", secret)
		switch r.URL.Path {
		case "/echo":
			_ = json.NewEncoder(w).Encode(map[string]string{"leak": secret})
		case "/redirect":
			w.Header().Set("Location", "http://127.0.0.1/other")
			w.WriteHeader(302)
		case "/unauthorized":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"denied"}`))
		case "/not-json":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<b>not json</b>"))
		case "/text":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("Accepted"))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "method": r.Method})
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	a, e := New(Config{DataDir: dir, AdminOrigin: "http://admin.local", PublicURL: "http://gateway"})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Store.DB.Close()
	c := Connection{ID: "conn", Name: "ERP A", Tenant: "kunde-a", URL: upstream.URL, Header: "Authorization", Prefix: "Bearer ", AllowPrivate: true}
	if e = a.Store.saveConnection(c, secret); e != nil {
		t.Fatal(e)
	}
	act := Action{ID: "act", Name: "Lesen", Slug: "kunden-liste", ConnectionID: c.ID, Method: "GET", Path: "/records", Active: true}
	if e = a.Store.saveAction(act); e != nil {
		t.Fatal(e)
	}
	for _, u := range []User{{ID: "alice", Name: "Alice", Username: "alice", Tenant: "kunde-a", Role: "client", Active: true, Grants: []string{"act"}}, {ID: "bob", Name: "Bob", Username: "bob", Tenant: "kunde-b", Role: "client", Active: true, Grants: []string{"act"}}, {ID: "admin", Name: "Admin", Username: "admin", Role: "admin", Active: true}} {
		if e = a.Store.saveUser(u, "Personal-password-123"); e != nil {
			t.Fatal(e)
		}
	}
	api := a.APIHandler()
	getToken := func(user string) string {
		r := invoke(api, "POST", "/auth/login", map[string]string{"username": user, "password": "Personal-password-123"}, "")
		expect(t, r, 200)
		var data map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &data)
		return data["access_token"].(string)
	}
	alice := getToken("alice")
	bob := getToken("bob")
	t.Run("upstream-only-key-and-no-cookies", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://gateway/actions/kunden-liste", nil)
		r.Header.Set("Authorization", "Bearer "+alice)
		r.Header.Set("Cookie", "bad=client-cookie")
		r.Header.Set("X-Api-Key", "spoof")
		r.Header.Set("Connection", "Authorization")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		expect(t, w, 200)
		if receivedAuth != "Bearer "+secret || receivedCookie != "" {
			t.Fatal("Anbieter-Auth oder Cookie-Isolation verletzt")
		}
		if strings.Contains(w.Body.String(), secret) || w.Header().Get("Set-Cookie") != "" || w.Header().Get("X-Secret") != "" {
			t.Fatal("Secret-/Cookie-Leck")
		}
	})
	t.Run("anonymous-no-upstream", func(t *testing.T) {
		before := calls
		expect(t, invoke(api, "GET", "/actions/kunden-liste", nil, ""), 401)
		if calls != before {
			t.Fatal("Unauthentifiziertes Upstream-Request")
		}
	})
	t.Run("tenant-checked-even-with-forged-grant", func(t *testing.T) {
		before := calls
		expect(t, invoke(api, "GET", "/actions/kunden-liste", nil, bob), 403)
		if calls != before {
			t.Fatal("Fremder Kunde erreicht Upstream")
		}
	})
	t.Run("method-query-and-unknown-action", func(t *testing.T) {
		before := calls
		expect(t, invoke(api, "POST", "/actions/kunden-liste", map[string]string{}, alice), 405)
		expect(t, invoke(api, "GET", "/actions/kunden-liste?url=http://evil.invalid", nil, alice), 400)
		expect(t, invoke(api, "GET", "/actions/unknown", nil, alice), 403)
		if calls != before {
			t.Fatal("Nicht freigegebener Aufruf")
		}
	})
	t.Run("duplicate-auth-rejected", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://gateway/actions/kunden-liste", nil)
		r.Header["Authorization"] = []string{"Bearer " + alice, "Bearer attacker"}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		expect(t, w, 401)
	})
	for path, status := range map[string]int{"/echo": 502, "/redirect": 502, "/unauthorized": 401, "/not-json": 502, "/text": 200} {
		t.Run("response-"+path, func(t *testing.T) {
			act.Path = path
			_ = a.Store.saveAction(act)
			w := invoke(api, "GET", "/actions/kunden-liste", nil, alice)
			expect(t, w, status)
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("Schlüssel in Antwort")
			}
			if path == "/text" && w.Body.String() != `{"text":"Accepted"}` {
				t.Fatalf("Textantwort nicht als JSON verpackt: %s", w.Body.String())
			}
			if path == "/unauthorized" && w.Header().Get("X-Gateway-Source") != "upstream" {
				t.Fatal("Statusquelle verloren")
			}
		})
	}
	act.Path = "/records"
	_ = a.Store.saveAction(act)
	t.Run("write-body-allowlist", func(t *testing.T) {
		act.Method = "POST"
		act.Fields = []string{"name"}
		_ = a.Store.saveAction(act)
		expect(t, invoke(api, "POST", "/actions/kunden-liste", map[string]string{"query": "delete all"}, alice), 400)
		expect(t, invoke(api, "POST", "/actions/kunden-liste", map[string]any{"name": map[string]string{"nested": "bad"}}, alice), 400)
		expect(t, invoke(api, "POST", "/actions/kunden-liste", map[string]string{"name": "Ada"}, alice), 200)
		expect(t, invoke(api, "POST", "/actions/kunden-liste", json.RawMessage(`{"name":{"hidden":true},"name":"ok"}`), alice), 200)
		if receivedBody != `{"name":"ok"}` {
			t.Fatal("Mehrdeutiges JSON unverändert weitergegeben")
		}
		expect(t, invoke(api, "POST", "/actions/kunden-liste", json.RawMessage(`{"name":9007199254740993}`), alice), 200)
		if !strings.Contains(receivedBody, "9007199254740993") {
			t.Fatal("Zahlenpräzision verloren")
		}

		act.Method = "GET"
		_ = a.Store.saveAction(act)
	})
	t.Run("private-default-and-linklocal-denied", func(t *testing.T) {
		c.AllowPrivate = false
		_ = a.Store.saveConnection(c, "")
		expect(t, invoke(api, "GET", "/actions/kunden-liste", nil, alice), 502)
		c.AllowPrivate = true
		_ = a.Store.saveConnection(c, "")
		for _, s := range []string{"169.254.169.254", "::ffff:169.254.169.254", "0.0.0.0", "224.0.0.1", "fe80::1"} {
			if allowedIP(net.ParseIP(s), true, false) {
				t.Fatalf("Nicht erlaubte Adresse %s", s)
			}
		}
	})
	t.Run("at-rest-and-tamper-detection", func(t *testing.T) {
		var blob []byte
		_ = a.Store.DB.QueryRow("SELECT secret FROM connections WHERE id='conn'").Scan(&blob)
		if bytes.Contains(blob, []byte(secret)) {
			t.Fatal("Schlüssel unverschlüsselt gespeichert")
		}
		original := append([]byte(nil), blob...)
		blob[len(blob)-1] ^= 1
		_, _ = a.Store.DB.Exec("UPDATE connections SET secret=? WHERE id='conn'", blob)
		expect(t, invoke(api, "GET", "/actions/kunden-liste", nil, alice), 503)
		_, _ = a.Store.DB.Exec("UPDATE connections SET secret=? WHERE id='conn'", original)
	})
	t.Run("client-cannot-access-admin", func(t *testing.T) {
		expect(t, invoke(api, "GET", "/api/state", nil, alice), 404)
		expect(t, invoke(api, "POST", "/auth/login", map[string]string{"username": "admin", "password": "Personal-password-123"}, ""), 401)
	})
	t.Run("disable-is-immediate", func(t *testing.T) {
		_, _ = a.Store.DB.Exec("UPDATE users SET active=0 WHERE id='alice'")
		expect(t, invoke(api, "GET", "/actions/kunden-liste", nil, alice), 401)
	})
	t.Run("permissions-and-expiry", func(t *testing.T) {
		_, _ = a.Store.DB.Exec("UPDATE sessions SET expires=?", time.Now().Unix()-1)
		expect(t, invoke(api, "GET", "/actions/kunden-liste", nil, bob), 401)
		info, e := os.Stat(filepath.Join(dir, "master.key"))
		if e != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("Master-Key-Rechte")
		}
	})
}
func TestAdminSetupCRUDAndCSRF(t *testing.T) {
	a, e := New(Config{DataDir: t.TempDir(), AdminOrigin: "http://admin.local"})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Store.DB.Close()
	h := a.AdminHandler()
	cookie := ""
	csrf := ""
	req := func(method, path string, body any, origin bool, proof bool) *httptest.ResponseRecorder {
		var b io.Reader
		if body != nil {
			x, _ := json.Marshal(body)
			b = bytes.NewReader(x)
		}
		r := httptest.NewRequest(method, "http://admin.local"+path, b)
		if origin {
			r.Header.Set("Origin", "http://admin.local")
		}
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		if proof {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	expect(t, req("POST", "/api/setup", map[string]string{"Username": "owner", "Password": "Personal-password-123", "Code": a.setup}, false, false), 403)
	expect(t, req("POST", "/api/setup", map[string]string{"Username": "owner", "Password": "Personal-password-123", "Code": a.setup}, true, false), 201)
	expect(t, req("POST", "/api/setup", map[string]string{}, true, false), 409)
	w := req("POST", "/api/login", map[string]string{"username": "owner", "password": "Personal-password-123"}, true, false)
	expect(t, w, 200)
	cookie = w.Header().Get("Set-Cookie")
	var login map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &login)
	csrf = login["csrf"]
	expect(t, req("POST", "/api/connections", map[string]any{}, true, false), 403)
	c := map[string]any{"name": "ERP", "tenant": "kunde-a", "url": "https://example.invalid/", "header": "Authorization", "prefix": "Bearer ", "secret": "my-secret-server-only"}
	w = req("POST", "/api/connections", c, true, true)
	expect(t, w, 200)
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	id := res["id"]
	act := Action{Name: "Read", Slug: "records-read", ConnectionID: id, Method: "GET", Path: "/records", Active: true}
	w = req("POST", "/api/actions", act, true, true)
	expect(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	actionID := res["id"]
	saved, _ := a.Store.action(actionID)
	if saved.Active {
		t.Fatal("Neue Aktion ohne Aktivierung aktiv")
	}
	w = req("GET", "/api/state", nil, false, false)
	expect(t, w, 200)
	if strings.Contains(w.Body.String(), "my-secret-server-only") {
		t.Fatal("Schlüssel über Admin-Lesezugriff")
	}
	expect(t, req("GET", "/api/export", nil, false, false), 200)
	expect(t, req("POST", "/api/users", map[string]any{"name": "Other", "username": "other", "tenant": "kunde-b", "password": "Personal-password-123", "grants": []string{actionID}}, true, true), 400)
	t.Run("public-target-no-upstream-key", func(t *testing.T) {
		public := map[string]any{"name": "WordPress", "tenant": "kunde-b", "url": "https://example.invalid", "allow_private": true}
		expect(t, req("POST", "/api/connections", public, true, true), 400)
		public["allow_private"] = false
		public["secret"] = "unnecessary-secret"
		expect(t, req("POST", "/api/connections", public, true, true), 400)
		delete(public, "secret")
		created := req("POST", "/api/connections", public, true, true)
		expect(t, created, 200)
		var result map[string]string
		if e := json.Unmarshal(created.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		saved, e := a.Store.connection(result["id"])
		if e != nil || saved.SecretSet || saved.Header != "" {
			t.Fatal("Öffentliche Verbindung fälschlich als authentifiziert gespeichert")
		}
		public["tenant"] = "kunde-a"
		expect(t, req("PUT", "/api/connections/"+id, public, true, true), 400)
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				w.WriteHeader(401)
			}
			_, _ = w.Write([]byte(`{"name":"WordPress"}`))
		}))
		defer upstream.Close()
		// Use a local HTTP test double without changing the HTTPS restriction in the admin API.
		saved.URL, saved.AllowPrivate = upstream.URL, true
		if err := a.Store.saveConnection(saved, ""); err != nil {
			t.Fatal(err)
		}
		publicAction := Action{ID: "public-action", Name: "Lesen", Slug: "wordpress-leseprobe", ConnectionID: saved.ID, Method: "GET", Path: "/posts", Active: true}
		if err := a.Store.saveAction(publicAction); err != nil {
			t.Fatal(err)
		}
		_, token, err := a.Store.newIntegrationToken("WordPress", []string{publicAction.ID}, 1)
		if err != nil {
			t.Fatal(err)
		}
		expect(t, invoke(a.APIHandler(), "GET", "/actions/wordpress-leseprobe", nil, ""), 401)
		forwarded := invoke(a.APIHandler(), "GET", "/actions/wordpress-leseprobe", nil, token)
		expect(t, forwarded, 200)
		if forwarded.Header().Get("X-Gateway-Source") != "upstream" {
			t.Fatal("Ziel-API wurde nicht erreicht")
		}
	})
	t.Run("integration-token-issue-scope-expiry-and-revoke", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer my-secret-server-only" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"customers":[{"name":"Beispiel"}]}`))
		}))
		defer upstream.Close()
		c["url"] = upstream.URL
		c["allow_private"] = true
		expect(t, req("PUT", "/api/connections/"+id, c, true, true), 200)
		tokenRequest := map[string]any{"name": "Ninox", "action_ids": []string{actionID}, "days": 3}
		expect(t, req("POST", "/api/integration-tokens", tokenRequest, true, false), 403)
		expect(t, req("POST", "/api/integration-tokens", tokenRequest, true, true), 400)
		act.ID = actionID
		expect(t, req("PUT", "/api/actions/"+actionID, act, true, true), 200)
		for _, days := range []int{-1, 3651} {
			tokenRequest["days"] = days
			expect(t, req("POST", "/api/integration-tokens", tokenRequest, true, true), 400)
		}
		tokenRequest["days"] = 3
		issue := func() (string, string) {
			t.Helper()
			response := req("POST", "/api/integration-tokens", tokenRequest, true, true)
			expect(t, response, 201)
			var result struct {
				IntegrationToken
				Token string `json:"token"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(result.Token, integrationPrefix) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("Token oder Cache-Schutz fehlt")
			}
			return result.ID, result.Token
		}
		tokenID, token := issue()
		state := req("GET", "/api/state", nil, false, false)
		expect(t, state, 200)
		if strings.Contains(state.Body.String(), token) || !strings.Contains(state.Body.String(), tokenID) {
			t.Fatal("Token im Klartext oder Metadaten fehlen")
		}
		api := a.APIHandler()
		expect(t, invoke(api, "GET", "/actions/records-read", nil, token), 200)
		expect(t, invoke(api, "GET", "/actions/other", nil, token), 403)
		expect(t, invoke(api, "POST", "/actions/records-read", nil, token), 405)
		expiredID, expiredToken := issue()
		if _, err := a.Store.DB.Exec("UPDATE integration_tokens SET expires=? WHERE id=?", time.Now().Unix(), expiredID); err != nil {
			t.Fatal(err)
		}
		expect(t, invoke(api, "GET", "/actions/records-read", nil, expiredToken), 401)
		expect(t, req("DELETE", "/api/integration-tokens/"+tokenID, nil, true, true), 200)
		expect(t, invoke(api, "GET", "/actions/records-read", nil, token), 401)
		tokenRequest["days"] = 0
		unlimitedID, unlimited := issue()
		var expires int64
		_ = a.Store.DB.QueryRow("SELECT expires FROM integration_tokens WHERE id=?", unlimitedID).Scan(&expires)
		if expires != 0 {
			t.Fatal("Unbegrenzter Token hat ein Ablaufdatum")
		}
		expect(t, invoke(api, "GET", "/actions/records-read", nil, unlimited), 200)
	})
	t.Run("basic-auth-encoded-on-server", func(t *testing.T) {
		basic := map[string]any{"name": "Shop", "tenant": "kunde-a", "url": "https://shop.invalid/wp-json/wc/v3", "auth": "basic", "basic_user": "ck_user", "secret": "cs_password"}
		w := req("POST", "/api/connections", basic, true, true)
		expect(t, w, 200)
		var created map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		stored, _ := a.Store.secret(created["id"])
		saved, _ := a.Store.connection(created["id"])
		if stored != "Y2tfdXNlcjpjc19wYXNzd29yZA==" || saved.Header != "Authorization" || saved.Prefix != "Basic " || saved.authMode() != "basic" {
			t.Fatalf("Basic-Anmeldung falsch gespeichert: %q %+v", stored, saved)
		}
		delete(basic, "secret")
		expect(t, req("PUT", "/api/connections/"+created["id"], basic, true, true), 400)
		delete(basic, "basic_user")
		expect(t, req("PUT", "/api/connections/"+created["id"], basic, true, true), 200)
		basic["auth"] = "header"
		basic["header"] = "X-Api-Key"
		expect(t, req("PUT", "/api/connections/"+created["id"], basic, true, true), 400)
	})
	for _, bad := range []string{"/../admin", "/a%2fb", "//evil", "/x?url=a#b", "/tables/{TABLE_ID}/records", "/a/{id}/{id}", "/a/{Id}"} {
		act.Path = bad
		expect(t, req("POST", "/api/actions", act, true, true), 400)
	}
	expect(t, req("POST", "/api/logout", map[string]any{}, true, true), 200)
	expect(t, req("GET", "/api/state", nil, false, false), 401)
}
func TestConnectionValidation(t *testing.T) {
	for _, raw := range []string{"http://public.invalid", "https://u:p@example.invalid", "https://example.invalid/?secret=x", "https://example.invalid/a%2fb"} {
		c := Connection{Name: "Test", Tenant: "kunde-a", URL: raw, Header: "Authorization"}
		if validateConnection(c) == nil {
			t.Fatalf("Adresse unerwartet erlaubt %s", raw)
		}
	}
	for _, h := range []string{"Host", "Connection", "Cookie", "X-Forwarded-For"} {
		if validateConnection(Connection{Name: "Test", Tenant: "kunde-a", URL: "https://example.invalid", Header: h}) == nil {
			t.Fatalf("Header %s nicht gesperrt", h)
		}
	}
}

func TestJSONSecretDetection(t *testing.T) {
	for _, data := range []string{`{"a":"\u0073ecret-value-123","a":"safe"}`, `{"secret-value-123":1}`, `["secret-value-123"]`} {
		if !containsSecret([]byte(data), "secret-value-123", "Bearer secret-value-123") {
			t.Fatal("Verdecktes Secret nicht erkannt")
		}
	}
}
func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	c := Connection{ID: "persist", Name: "Test", Tenant: "kunde-a", URL: "https://example.invalid", Header: "Authorization"}
	if e = s.saveConnection(c, "persistent-secret-key"); e != nil {
		t.Fatal(e)
	}
	s.DB.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	value, e := s.secret("persist")
	if e != nil || value != "persistent-secret-key" {
		t.Fatal("Zugangsdaten nach Neustart nicht verfügbar")
	}
}

func TestMissingOrWrongMasterKeyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.saveConnection(Connection{ID: "key-test", Name: "Test", Tenant: "kunde-a", URL: "https://example.invalid", Header: "Authorization"}, "secret-key-to-keep"); e != nil {
		t.Fatal(e)
	}
	s.DB.Close()
	path := filepath.Join(dir, "master.key")
	key, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if reopened, e := OpenStore(dir); e == nil {
		reopened.DB.Close()
		t.Fatal("Fehlender Schlüssel wurde automatisch ersetzt")
	}
	if e = os.WriteFile(path, bytes.Repeat([]byte{1}, 32), 0600); e != nil {
		t.Fatal(e)
	}
	if reopened, e := OpenStore(dir); e == nil {
		reopened.DB.Close()
		t.Fatal("Falscher Schlüssel akzeptiert")
	}
	if e = os.WriteFile(path, key, 0600); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	reopened.DB.Close()
}

type upstreamRecord struct {
	calls                    int
	path, query, body, ctype string
	auth, extra              string
}

func recordingUpstream(rec *upstreamRecord) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		b, _ := io.ReadAll(r.Body)
		rec.path, rec.query, rec.body, rec.ctype = r.URL.Path, r.URL.RawQuery, string(b), r.Header.Get("Content-Type")
		rec.auth, rec.extra = r.Header.Get("Authorization"), r.Header.Get("Notion-Version")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

func TestParametersTemplatesAndAuthModes(t *testing.T) {
	var rec upstreamRecord
	upstream := recordingUpstream(&rec)
	defer upstream.Close()
	a, e := New(Config{DataDir: t.TempDir(), AdminOrigin: "http://admin.local", PublicURL: "http://gateway"})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Store.DB.Close()
	conn := Connection{ID: "c1", Name: "CRM", Tenant: "kunde-a", URL: upstream.URL + "/v1", Auth: "header", Header: "Authorization", Prefix: "Bearer ", AllowPrivate: true,
		Headers: []HeaderValue{{Name: "Notion-Version", Value: "2022-06-28"}}}
	if e = a.Store.saveConnection(conn, "server-only-secret"); e != nil {
		t.Fatal(e)
	}
	save := func(act Action) {
		t.Helper()
		if err := validateAction(&act, conn); err != nil {
			t.Fatal(err)
		}
		act.Active = true
		if err := a.Store.saveAction(act); err != nil {
			t.Fatal(err)
		}
	}
	save(Action{ID: "read", Name: "Datensatz", Slug: "datensatz", ConnectionID: "c1", Method: "GET", Path: "/records/{id}?view=kurz", Query: []string{"page"}})
	save(Action{ID: "create", Name: "Anlegen", Slug: "anlegen", ConnectionID: "c1", Method: "POST", Path: "/records",
		BodyTemplate: `[{"fields":{"Name":"{{name}}","Info":"Kunde {{name}} / {{nr}}","Nr":"{{nr}}","Fest":true}}]`})
	save(Action{ID: "form", Name: "Formular", Slug: "formular", ConnectionID: "c1", Method: "POST", Path: "/customers", Fields: []string{"email"}, BodyFormat: "form"})
	save(Action{ID: "hook", Name: "Webhook", Slug: "webhook", ConnectionID: "c1", Method: "POST", Path: "/", Fields: []string{"text"}})
	_, token, err := a.Store.newIntegrationToken("Alle", []string{"read", "create", "form", "hook"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	api := a.APIHandler()
	t.Run("path-and-query", func(t *testing.T) {
		expect(t, invoke(api, "GET", "/actions/datensatz?id=42&page=2", nil, token), 200)
		if rec.path != "/v1/records/42" || rec.query != "page=2&view=kurz" || rec.extra != "2022-06-28" || rec.auth != "Bearer server-only-secret" {
			t.Fatalf("Weiterleitung falsch: %+v", rec)
		}
		before := rec.calls
		for _, q := range []string{"", "?id=..", "?id=a/b", "?id=1&id=2", "?id=1&view=lang", "?id=1&other=x", "?id=1&page=a&page=b", "?id=%zz"} {
			expect(t, invoke(api, "GET", "/actions/datensatz"+q, nil, token), 400)
		}
		if rec.calls != before {
			t.Fatal("Ungültige Parameter erreichten die Ziel-API")
		}
	})
	t.Run("body-template", func(t *testing.T) {
		expect(t, invoke(api, "POST", "/actions/anlegen", json.RawMessage(`{"name":"Ada <GmbH>","nr":7}`), token), 200)
		if rec.body != `[{"fields":{"Fest":true,"Info":"Kunde Ada <GmbH> / 7","Name":"Ada <GmbH>","Nr":7}}]` || rec.ctype != "application/json" {
			t.Fatalf("Vorlage falsch gefüllt: %s", rec.body)
		}
		before := rec.calls
		expect(t, invoke(api, "POST", "/actions/anlegen", map[string]any{"name": "Ada"}, token), 400)
		expect(t, invoke(api, "POST", "/actions/anlegen", map[string]any{"name": "Ada", "nr": 1, "extra": 1}, token), 400)
		expect(t, invoke(api, "POST", "/actions/anlegen", map[string]any{"name": []string{"x"}, "nr": 1}, token), 400)
		if rec.calls != before {
			t.Fatal("Ungültiger Body erreichte die Ziel-API")
		}
	})
	t.Run("form-and-webhook-root", func(t *testing.T) {
		expect(t, invoke(api, "POST", "/actions/formular", map[string]string{"email": "a+b@example.org"}, token), 200)
		if rec.ctype != "application/x-www-form-urlencoded" || rec.body != "email=a%2Bb%40example.org" {
			t.Fatalf("Formular falsch: %s %s", rec.ctype, rec.body)
		}
		expect(t, invoke(api, "POST", "/actions/webhook", map[string]string{"text": "Hallo"}, token), 200)
		if rec.path != "/v1" {
			t.Fatalf("Webhook-Pfad: %s", rec.path)
		}
	})
	t.Run("query-auth", func(t *testing.T) {
		q := Connection{ID: "c2", Name: "Pipedrive", Tenant: "kunde-a", URL: upstream.URL, Auth: "query", Header: "api_token", AllowPrivate: true}
		if err := validateConnection(q); err != nil {
			t.Fatal(err)
		}
		_ = a.Store.saveConnection(q, "query-secret-123")
		act := Action{ID: "deals", Name: "Deals", Slug: "deals", ConnectionID: "c2", Method: "GET", Path: "/deals", Query: []string{"api_token"}}
		if validateAction(&act, q) == nil {
			t.Fatal("Schlüssel-Parameter als Client-Parameter erlaubt")
		}
		act.Query = []string{"limit"}
		if err := validateAction(&act, q); err != nil {
			t.Fatal(err)
		}
		act.Active = true
		_ = a.Store.saveAction(act)
		_, qt, _ := a.Store.newIntegrationToken("Deals", []string{"deals"}, 0)
		expect(t, invoke(api, "GET", "/actions/deals?limit=5", nil, qt), 200)
		if rec.query != "api_token=query-secret-123&limit=5" || rec.auth != "" {
			t.Fatalf("Query-Schlüssel falsch: %s", rec.query)
		}
		expect(t, invoke(api, "GET", "/actions/deals?api_token=x", nil, qt), 400)
		expect(t, invoke(api, "GET", "/actions/datensatz?id=1", nil, qt), 403)
	})
	t.Run("delete-action-keeps-token-for-others", func(t *testing.T) {
		if err := a.Store.deleteAction("read"); err != nil {
			t.Fatal(err)
		}
		expect(t, invoke(api, "GET", "/actions/datensatz?id=1", nil, token), 403)
		expect(t, invoke(api, "POST", "/actions/webhook", map[string]string{"text": "Hallo"}, token), 200)
	})
}

func TestActionAndConnectionRules(t *testing.T) {
	c := Connection{Name: "X", Tenant: "kunde-a", URL: "https://api.example.invalid", Auth: "header", Header: "Authorization", Prefix: "Bearer "}
	bad := []Action{
		{Path: "/tables/{TABLE_ID}"},
		{Path: "/x/{id}", Query: []string{"id"}},
		{Path: "/x?limit=5", Query: []string{"limit"}},
		{Path: "/x", Method: "GET", BodyTemplate: `{"a":"{{b}}"}`},
		{Path: "/x", Method: "POST", BodyTemplate: `{"a":`},
		{Path: "/x", Method: "POST", BodyTemplate: `"text"`},
		{Path: "/x", Method: "POST", BodyTemplate: `{"{{key}}":1}`},
		{Path: "/x", Method: "POST", BodyTemplate: `{"a":"{{Gross}}"}`},
		{Path: "/x", Method: "POST", BodyFormat: "form", BodyTemplate: `{"a":{"b":"{{c}}"}}`},
		{Path: "/x", Method: "POST", BodyFormat: "xml"},
	}
	for i, act := range bad {
		act.Name, act.Slug = "Test", "test-aktion"
		if act.Method == "" {
			act.Method = "GET"
		}
		if validateAction(&act, c) == nil {
			t.Fatalf("Regel %d nicht geprüft: %+v", i, act)
		}
	}
	good := Action{Name: "Test", Slug: "test-aktion", Method: "PATCH", Path: "/orders/{id}.json", BodyTemplate: `{"order":{"note":"{{notiz}}","tags":["schleuse","{{tag}}"]}}`}
	if err := validateAction(&good, c); err != nil || strings.Join(good.Fields, ",") != "notiz,tag" {
		t.Fatalf("Gültige Vorlage abgelehnt: %v %v", err, good.Fields)
	}
	for _, h := range []HeaderValue{{"Authorization", "x"}, {"Host", "x"}, {"X-Test", "a\nb"}, {"X-Test", ""}} {
		c.Headers = []HeaderValue{h}
		if validateConnection(c) == nil {
			t.Fatalf("Zusatz-Header erlaubt: %+v", h)
		}
	}
	c.Headers = nil
	c.URL = "https://{SHOP}.myshopify.com"
	if validateConnection(c) == nil {
		t.Fatal("Platzhalter in Zieladresse erlaubt")
	}
}

func TestLegacyTokenMigration(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.saveConnection(Connection{ID: "c", Name: "Alt", Tenant: "kunde-a", URL: "https://example.invalid", Header: "Authorization", Prefix: "Bearer "}, "legacy-secret-123")
	_ = s.saveAction(Action{ID: "a", Name: "Alt", Slug: "alt", ConnectionID: "c", Method: "GET", Path: "/x", Active: true})
	_, _ = s.DB.Exec("DELETE FROM integration_token_actions")
	_, e = s.DB.Exec("INSERT INTO integration_tokens(id,hash,name,action_id,expires) VALUES('t',?,'Alt','a',?)", tokenHash("slt_legacy"), time.Now().Add(time.Hour).Unix())
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if _, id, err := s.integrationScope("slt_legacy", "alt"); err != nil || id != "a" {
		t.Fatalf("Bestehender Token nach Update unbrauchbar: %v", err)
	}
	c, _ := s.connection("c")
	if c.authMode() != "header" {
		t.Fatal("Bestehende Verbindung ohne Anmeldeart falsch gelesen")
	}
}
