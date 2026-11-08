package app

import (
	"regexp"
	"strings"
	"testing"
)

// Jede Vorlage muss nach dem Ausfüllen der Admin-Platzhalter die normalen Regeln für Verbindungen und Aktionen erfüllen.
func TestConnectorTemplatesAreValid(t *testing.T) {
	templates, err := connectorTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) < 10 {
		t.Fatalf("Nur %d Vorlagen geladen", len(templates))
	}
	fill := func(s string) string { return adminPlaceholder.ReplaceAllString(s, "beispiel") }
	ids, slugs := map[string]bool{}, map[string]bool{}
	for _, tpl := range templates {
		if tpl.ID == "" || ids[tpl.ID] || tpl.Name == "" || tpl.Category == "" || tpl.Help == "" || !strings.HasPrefix(tpl.Docs, "https://") {
			t.Fatalf("Vorlage %q unvollständig", tpl.ID)
		}
		ids[tpl.ID] = true
		switch tpl.Status {
		case "getestet", "teilweise", "doku":
		default:
			t.Fatalf("%s: unbekannter Status %q", tpl.ID, tpl.Status)
		}
		cc := tpl.Connection
		c := Connection{Name: tpl.Name, Tenant: "kunde-a", URL: fill(cc.URL), Auth: cc.Auth, Header: cc.Header, Prefix: cc.Prefix, Headers: cc.Headers}
		if cc.Auth == "none" {
			c.Auth = ""
		}
		if err := validateConnection(c); err != nil {
			t.Fatalf("%s: Verbindung ungültig: %v", tpl.ID, err)
		}
		if len(tpl.Actions) == 0 {
			t.Fatalf("%s: keine Aktionen", tpl.ID)
		}
		for _, ta := range tpl.Actions {
			if slugs[ta.Slug] {
				t.Fatalf("Aktionsname %s doppelt", ta.Slug)
			}
			slugs[ta.Slug] = true
			act := Action{Name: ta.Name, Slug: ta.Slug, Method: ta.Method, Path: fill(ta.Path), Query: ta.Query, Fields: ta.Fields, BodyTemplate: fill(ta.BodyTemplate), BodyFormat: ta.BodyFormat}
			if err := validateAction(&act, c); err != nil {
				t.Fatalf("%s/%s: Aktion ungültig: %v", tpl.ID, ta.Slug, err)
			}
			if ta.BodyTemplate != "" && !regexp.MustCompile(`\{\{[a-z]|\{[A-Z]|^[\[{]`).MatchString(ta.BodyTemplate) {
				t.Fatalf("%s/%s: Body-Vorlage unerwartet", tpl.ID, ta.Slug)
			}
		}
	}
}
