package app

import (
	"net/http"
	"strings"
)

func (a *App) SeedDemo() error {
	if !a.Store.hasAdmin() {
		if e := a.Store.saveUser(User{ID: randomID(), Name: "Lokaler Administrator", Username: "demo", Role: "admin", Active: true}, "Lokal-testen-2026!"); e != nil {
			return e
		}
	}
	if _, e := a.Store.connection("demo-connection"); e != nil {
		if e = a.Store.saveConnection(Connection{ID: "demo-connection", Name: "Beispiel · Kundenverwaltung", Tenant: "kunde-demo", URL: a.Config.DemoURL, Header: "Authorization", Prefix: "Bearer ", AllowPrivate: true}, "dummy-provider-key-local-only"); e != nil {
			return e
		}
	}
	if _, e := a.Store.action("demo-action"); e != nil {
		if e = a.Store.saveAction(Action{ID: "demo-action", Name: "Kundenliste lesen", Slug: "kunden-liste", ConnectionID: "demo-connection", Method: "GET", Path: "/customers", Fields: []string{}, Active: true}); e != nil {
			return e
		}
	}
	var n int
	if e := a.Store.DB.QueryRow("SELECT count(*) FROM users WHERE username='tester'").Scan(&n); e != nil {
		return e
	}
	if n == 0 {
		return a.Store.saveUser(User{ID: randomID(), Name: "Testnutzer", Username: "tester", Tenant: "kunde-demo", Role: "client", Active: true, Grants: []string{"demo-action"}}, "Testzugang-2026!")
	}
	return nil
}
func (a *App) DemoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, e := a.Store.secret("demo-connection")
		if e != nil || r.Header.Get("Authorization") != "Bearer "+s {
			send(w, 401, map[string]string{"error": "Beispiel-API erwartet ihren eigenen Schlüssel."})
			return
		}
		switch r.URL.Path {
		case "/customers":
			send(w, 200, map[string]any{"customers": []any{map[string]any{"id": 1, "name": "Beispiel GmbH", "city": "Berlin"}, map[string]any{"id": 2, "name": "Nordlicht Studio", "city": "Hamburg"}}, "note": "Ausschließlich Dummy-Daten. Anbieter-Schlüssel wurde serverseitig geprüft."})
		case "/echo":
			send(w, 200, map[string]string{"echo": strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")})
		default:
			send(w, 404, map[string]string{"error": "Beispielroute nicht gefunden."})
		}
	})
}
