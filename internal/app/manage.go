package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

func validPath(p string) bool {
	for _, c := range p {
		if unicode.IsControl(c) {
			return false
		}
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "%\\?#\r\n\t") || strings.Contains(p, "//") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

var (
	headerNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	queryNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\[\]-]{0,63}$`)
	fieldNamePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,49}$`)
	paramNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)
	placeholderAny    = regexp.MustCompile(`\{([^{}]*)\}`)
	adminPlaceholder  = regexp.MustCompile(`\{[A-Z][A-Z0-9_]{1,40}\}`)
)

// headerUsable prüft, ob ein Header für Zugangsdaten oder feste Zusatzangaben verwendet werden darf.
func headerUsable(name string) error {
	if !headerNamePattern.MatchString(name) {
		return errors.New("Headername ist ungültig")
	}
	switch strings.ToLower(name) {
	case "host", "cookie", "set-cookie", "content-length", "content-type", "connection", "transfer-encoding", "upgrade", "te", "trailer", "proxy-authorization", "proxy-authenticate", "forwarded", "accept", "range", "x-request-id":
		return errors.New("Dieser Header ist reserviert und kann nicht verwendet werden")
	}
	if strings.HasPrefix(strings.ToLower(name), "x-forwarded-") {
		return errors.New("Forwarding-Header sind reserviert")
	}
	return nil
}

func validateConnection(c Connection) error {
	if strings.ContainsAny(c.URL, "{}") {
		return errors.New("Platzhalter in der Zieladresse durch die echten Werte ersetzen")
	}
	u, e := url.Parse(c.URL)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.RawPath != "" || !validPath("/"+strings.TrimPrefix(u.Path, "/"))) {
		return errors.New("Eine feste HTTP(S)-Zieladresse ohne Zugangsdaten, Query oder kodierte Pfade angeben")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowPrivate) {
		return errors.New("HTTPS erforderlich; internes HTTP nur mit ausdrücklicher Netzwerkfreigabe")
	}
	if !ident.MatchString(c.Tenant) || strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 {
		return errors.New("Verbindungsname und Kundenzuordnung (2–50 Zeichen) erforderlich")
	}
	switch c.authMode() {
	case "none":
		if c.Prefix != "" || c.AllowPrivate || u.Scheme != "https" {
			return errors.New("Verbindungen ohne Schlüssel brauchen ein öffentliches HTTPS-Ziel und kein Präfix")
		}
	case "header":
		if e := headerUsable(c.Header); e != nil {
			return e
		}
		if strings.ContainsAny(c.Prefix, "\r\n") || len(c.Prefix) > 100 {
			return errors.New("Header-Präfix ist ungültig")
		}
	case "basic":
		if c.Header != "Authorization" || c.Prefix != "Basic " {
			return errors.New("Basic-Anmeldung verwendet den Header Authorization mit Präfix „Basic “")
		}
	case "query":
		if !queryNamePattern.MatchString(c.Header) || c.Prefix != "" {
			return errors.New("Name des Query-Parameters für den Schlüssel ist ungültig")
		}
	default:
		return errors.New("Unbekannte Anmeldeart")
	}
	if len(c.Headers) > 10 {
		return errors.New("Maximal 10 feste Zusatz-Header")
	}
	seen := map[string]bool{}
	for _, h := range c.Headers {
		if e := headerUsable(h.Name); e != nil {
			return fmt.Errorf("Zusatz-Header „%s“: %w", h.Name, e)
		}
		key := strings.ToLower(h.Name)
		if key == "authorization" || seen[key] || (c.authMode() != "query" && strings.EqualFold(h.Name, c.Header)) {
			return fmt.Errorf("Zusatz-Header „%s“ ist doppelt oder gehört zu den Zugangsdaten", h.Name)
		}
		seen[key] = true
		if h.Value == "" || len(h.Value) > 500 || strings.IndexFunc(h.Value, unicode.IsControl) >= 0 {
			return fmt.Errorf("Wert für Zusatz-Header „%s“: 1–500 Zeichen, keine Steuerzeichen", h.Name)
		}
	}
	return nil
}

// validateAction prüft Pfad, Parameter und Body-Vorlage. Bei einer Vorlage werden die Eingabefelder daraus abgeleitet.
func validateAction(act *Action, c Connection) error {
	if !slugPattern.MatchString(act.Slug) || strings.TrimSpace(act.Name) == "" || len(act.Name) > 100 {
		return errors.New("Name und eindeutigen Aktionsnamen prüfen (Kleinbuchstaben, Ziffern, Bindestrich)")
	}
	switch act.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return errors.New("HTTP-Methode nicht freigegeben")
	}
	path, fixed, hasFixed := strings.Cut(act.Path, "?")
	params := map[string]bool{}
	for _, m := range placeholderAny.FindAllStringSubmatch(path, -1) {
		name := m[1]
		if !paramNamePattern.MatchString(name) {
			if name != "" && strings.ToUpper(name) == name {
				return fmt.Errorf("Platzhalter {%s} im Zielpfad durch den echten Wert ersetzen", name)
			}
			return errors.New("Platzhalter im Zielpfad: Kleinbuchstaben, Ziffern und _ verwenden, z. B. {id}")
		}
		if params[name] {
			return fmt.Errorf("Platzhalter {%s} kommt doppelt vor", name)
		}
		params[name] = true
	}
	if plain := placeholderAny.ReplaceAllString(path, "x"); strings.ContainsAny(plain, "{}") || !validPath(plain) {
		return errors.New("Fester Zielpfad ungültig: mit / beginnen, keine Kodierungen, .. oder //")
	}
	fixedKeys := map[string]bool{}
	if hasFixed {
		values, e := url.ParseQuery(fixed)
		if fixed == "" || e != nil || strings.ContainsAny(fixed, "#{} \r\n\t") {
			return errors.New("Feste Query-Parameter im Zielpfad sind ungültig, z. B. /posts?status=publish")
		}
		for k := range values {
			if !queryNamePattern.MatchString(k) || (c.authMode() == "query" && strings.EqualFold(k, c.Header)) {
				return fmt.Errorf("Fester Query-Parameter „%s“ ist nicht erlaubt", k)
			}
			fixedKeys[k] = true
		}
	}
	if len(act.Query) > 20 {
		return errors.New("Maximal 20 freigegebene Query-Parameter")
	}
	seen := map[string]bool{}
	for _, q := range act.Query {
		switch {
		case !queryNamePattern.MatchString(q):
			return fmt.Errorf("Query-Parameter „%s“ ist ungültig", q)
		case params[q]:
			return fmt.Errorf("„%s“ ist bereits Platzhalter im Pfad", q)
		case fixedKeys[q]:
			return fmt.Errorf("„%s“ ist im Zielpfad fest gesetzt", q)
		case seen[q] || (c.authMode() == "query" && strings.EqualFold(q, c.Header)):
			return fmt.Errorf("Query-Parameter „%s“ ist doppelt oder für den Schlüssel reserviert", q)
		}
		seen[q] = true
	}
	if act.Query == nil {
		act.Query = []string{}
	}
	if act.BodyFormat != "" && act.BodyFormat != "form" {
		return errors.New("Body-Format: JSON oder Formular")
	}
	if act.Method == "GET" || act.Method == "DELETE" {
		if strings.TrimSpace(act.BodyTemplate) != "" || len(act.Fields) > 0 || act.BodyFormat != "" {
			return errors.New("GET und DELETE senden keinen Body. Werte über Platzhalter oder Query-Parameter übergeben")
		}
		act.BodyTemplate, act.Fields = "", []string{}
		return nil
	}
	if strings.TrimSpace(act.BodyTemplate) == "" {
		act.BodyTemplate = ""
		if len(act.Fields) > 32 {
			return errors.New("Maximal 32 Eingabefelder")
		}
		for _, f := range act.Fields {
			if !fieldNamePattern.MatchString(f) {
				return fmt.Errorf("Eingabefeldname „%s“ ist ungültig", f)
			}
		}
		if act.Fields == nil {
			act.Fields = []string{}
		}
		return nil
	}
	if m := adminPlaceholder.FindString(act.BodyTemplate); m != "" {
		return fmt.Errorf("Platzhalter %s in der Body-Vorlage durch den echten Wert ersetzen", m)
	}
	fields, e := templateFields(act.BodyTemplate, act.BodyFormat == "form")
	if e != nil {
		return e
	}
	act.Fields = fields
	return nil
}

func itemID(path, base string) string { return strings.TrimPrefix(path, base+"/") }
func (a *App) manageConnection(w http.ResponseWriter, r *http.Request) {
	id := itemID(r.URL.Path, "/api/connections")
	if r.Method == "DELETE" {
		acts, e := a.Store.actions()
		if e != nil {
			fail(w, 500, "Speicherfehler.")
			return
		}
		for _, v := range acts {
			if v.ConnectionID == id {
				fail(w, 409, "Zuerst die zugehörigen Aktionen löschen.")
				return
			}
		}
		_, e = a.Store.DB.Exec("DELETE FROM connections WHERE id=?", id)
		if e != nil {
			fail(w, 500, "Löschen fehlgeschlagen.")
			return
		}
		send(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" && r.Method != "PUT" {
		fail(w, 405, "Methode nicht erlaubt.")
		return
	}
	var in struct {
		Connection
		Secret    string `json:"secret"`
		BasicUser string `json:"basic_user"`
	}
	if !decode(w, r, &in) {
		return
	}
	switch in.Auth {
	case "none":
		in.Auth, in.Header, in.Prefix = "", "", ""
	case "basic":
		in.Header, in.Prefix = "Authorization", "Basic "
		if in.BasicUser != "" || in.Secret != "" {
			if in.BasicUser == "" || in.Secret == "" || strings.Contains(in.BasicUser, ":") || len(in.BasicUser) > 256 || len(in.Secret) > 1024 || strings.ContainsAny(in.BasicUser+in.Secret, "\r\n") {
				fail(w, 400, "Für Basic-Anmeldung Benutzername (ohne Doppelpunkt) und Passwort gemeinsam angeben.")
				return
			}
			in.Secret = base64.StdEncoding.EncodeToString([]byte(in.BasicUser + ":" + in.Secret))
		}
	case "", "header", "query":
		if in.Header == "" {
			in.Auth = ""
		} else if in.Auth == "" {
			in.Auth = "header"
		}
	default:
		fail(w, 400, "Unbekannte Anmeldeart.")
		return
	}
	if in.Headers == nil {
		in.Headers = []HeaderValue{}
	}
	if r.Method == "POST" {
		in.ID = randomID()
	} else {
		old, e := a.Store.connection(id)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		if old.Tenant != in.Tenant {
			fail(w, 400, "Kundenzuordnung einer bestehenden Verbindung ist unveränderlich.")
			return
		}
		if old.authMode() != in.authMode() {
			fail(w, 400, "Für einen Wechsel der Anmeldeart eine neue Verbindung anlegen.")
			return
		}
		in.ID = id
	}
	if e := validateConnection(in.Connection); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if in.Header == "" && in.Secret != "" {
		fail(w, 400, "Für diese Verbindung ist kein Anbieter-Schlüssel vorgesehen.")
		return
	}
	if in.Secret != "" && in.Auth != "basic" && (len(in.Secret) < 8 || len(in.Secret) > 8192 || strings.ContainsAny(in.Secret, "\r\n")) {
		fail(w, 400, "Schlüssel: 8–8192 Zeichen, keine Zeilenumbrüche.")
		return
	}
	if e := a.Store.saveConnection(in.Connection, in.Secret); e != nil {
		fail(w, 400, "Verbindung konnte nicht gespeichert werden. Für neue Verbindungen ist ein Schlüssel erforderlich.")
		return
	}
	send(w, 200, map[string]string{"id": in.ID})
}
func (a *App) manageAction(w http.ResponseWriter, r *http.Request) {
	id := itemID(r.URL.Path, "/api/actions")
	if strings.HasSuffix(id, "/test") && r.Method == "POST" {
		act, e := a.Store.action(strings.TrimSuffix(id, "/test"))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Params map[string]string `json:"params"`
		}
		if !decode(w, r, &in) {
			return
		}
		if act.Method != "GET" {
			fail(w, 405, "Der Test in der Verwaltung ist nur für Leseaktionen (GET) verfügbar.")
			return
		}
		c, e := a.Store.connection(act.ConnectionID)
		if e != nil {
			fail(w, 400, "Verbindung fehlt.")
			return
		}
		query := url.Values{}
		for k, v := range in.Params {
			if v != "" {
				query.Set(k, v)
			}
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://gateway/actions/"+act.Slug, nil).WithContext(r.Context())
		req.URL.RawQuery = query.Encode()
		start := time.Now()
		a.forward(rec, req, act, c)
		preview := rec.Body.String()
		if len(preview) > 4000 {
			preview = preview[:4000] + " …"
		}
		send(w, 200, map[string]any{"status": rec.Code, "source": rec.Header().Get("X-Gateway-Source"), "duration_ms": time.Since(start).Milliseconds(), "ok": rec.Code >= 200 && rec.Code < 300, "preview": preview})
		return
	}
	if r.Method == "DELETE" {
		if e := a.Store.deleteAction(id); e != nil {
			fail(w, 500, "Löschen fehlgeschlagen.")
			return
		}
		send(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" && r.Method != "PUT" {
		fail(w, 405, "Methode nicht erlaubt.")
		return
	}
	var act Action
	if !decode(w, r, &act) {
		return
	}
	if r.Method == "POST" {
		act.ID = randomID()
		act.Active = false
	} else {
		old, e := a.Store.action(id)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		if old.ConnectionID != act.ConnectionID {
			fail(w, 400, "Für einen anderen Anbieter eine neue Aktion erstellen.")
			return
		}
		act.ID = id
	}
	c, e := a.Store.connection(act.ConnectionID)
	if e != nil {
		fail(w, 400, "Verbindung auswählen.")
		return
	}
	if e := validateAction(&act, c); e != nil {
		fail(w, 400, e.Error()+".")
		return
	}
	if e := a.Store.saveAction(act); e != nil {
		fail(w, 409, "Aktionsname ist bereits vergeben oder konnte nicht gespeichert werden.")
		return
	}
	send(w, 200, map[string]string{"id": act.ID})
}
func (a *App) manageUser(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		id := itemID(r.URL.Path, "/api/users")
		_, e := a.Store.DB.Exec("UPDATE users SET active=0 WHERE id=? AND role='client'", id)
		if e != nil {
			fail(w, 500, "Sperren fehlgeschlagen.")
			return
		}
		send(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "Methode nicht erlaubt.")
		return
	}
	var in struct {
		User
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.ID = randomID()
	in.Role = "client"
	in.Active = true
	if !ident.MatchString(in.Username) || !ident.MatchString(in.Tenant) || len(in.Password) < 12 || len(in.Password) > 256 || in.Name == "" || len(in.Name) > 100 {
		fail(w, 400, "Name, Kunde und Benutzername erforderlich; Passwort mindestens 12 Zeichen.")
		return
	}
	for _, id := range in.Grants {
		act, e := a.Store.action(id)
		if e != nil {
			fail(w, 400, "Freigabe nicht gefunden.")
			return
		}
		c, e := a.Store.connection(act.ConnectionID)
		if e != nil || c.Tenant != in.Tenant {
			fail(w, 400, "Freigaben müssen zum gleichen Kunden gehören.")
			return
		}
	}
	if e := a.Store.saveUser(in.User, in.Password); e != nil {
		fail(w, 409, "Benutzername bereits vergeben oder Speichern fehlgeschlagen.")
		return
	}
	send(w, 201, map[string]string{"id": in.ID})
}

func (a *App) manageIntegrationToken(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/integration-tokens" && r.Method == "POST":
		var input struct {
			Name      string   `json:"name"`
			ActionIDs []string `json:"action_ids"`
			Days      int      `json:"days"`
		}
		if !decode(w, r, &input) {
			return
		}
		name := strings.TrimSpace(input.Name)
		if name == "" || len(name) > 100 || input.Days < 0 || input.Days > 3650 {
			fail(w, 400, "Name erforderlich. Laufzeit: unbegrenzt oder 1 bis 3650 Tage.")
			return
		}
		if len(input.ActionIDs) == 0 || len(input.ActionIDs) > 20 {
			fail(w, 400, "Eine bis 20 aktive Aktionen auswählen.")
			return
		}
		tenant := ""
		seen := map[string]bool{}
		for _, id := range input.ActionIDs {
			action, err := a.Store.action(id)
			if err != nil || !action.Active || seen[id] {
				fail(w, 400, "Nur aktive Aktionen auswählen, jede einmal.")
				return
			}
			seen[id] = true
			c, err := a.Store.connection(action.ConnectionID)
			if err != nil || (tenant != "" && c.Tenant != tenant) {
				fail(w, 400, "Alle Aktionen eines Tokens müssen zum selben Kunden gehören.")
				return
			}
			tenant = c.Tenant
		}
		token, secret, err := a.Store.newIntegrationToken(name, input.ActionIDs, input.Days)
		if err != nil {
			fail(w, 500, "Token konnte nicht erstellt werden.")
			return
		}
		send(w, 201, struct {
			IntegrationToken
			Token string `json:"token"`
		}{token, secret})
	case strings.HasPrefix(r.URL.Path, "/api/integration-tokens/") && r.Method == "DELETE":
		id := strings.TrimPrefix(r.URL.Path, "/api/integration-tokens/")
		if _, err := a.Store.DB.Exec("DELETE FROM integration_tokens WHERE id=?", id); err != nil {
			fail(w, 500, "Token konnte nicht widerrufen werden.")
			return
		}
		send(w, 200, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}
