package app

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

// Konnektor-Vorlagen liegen als JSON-Dateien in connectors/ und werden in das Programm eingebettet.
// Großgeschriebene Platzhalter wie {TEAM_ID} trägt der Admin ein, kleingeschriebene wie {id} liefert der Client.
//
//go:embed connectors/*.json
var connectorFiles embed.FS

type ConnectorTemplate struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Category    string             `json:"category"`
	Description string             `json:"description"`
	Status      string             `json:"status"` // getestet, teilweise oder doku (nach Anbieter-Dokumentation)
	Connection  TemplateConnection `json:"connection"`
	Help        string             `json:"help"`
	Docs        string             `json:"docs"`
	Actions     []TemplateAction   `json:"actions"`
}
type TemplateConnection struct {
	URL         string        `json:"url"`
	URLHint     string        `json:"url_hint"`
	Auth        string        `json:"auth"`
	Header      string        `json:"header"`
	Prefix      string        `json:"prefix"`
	Headers     []HeaderValue `json:"headers"`
	SecretLabel string        `json:"secret_label"`
}
type TemplateAction struct {
	Name         string          `json:"name"`
	Slug         string          `json:"slug"`
	Method       string          `json:"method"`
	Path         string          `json:"path"`
	Query        []string        `json:"query"`
	Fields       []string        `json:"fields"`
	Body         json.RawMessage `json:"body,omitempty"`
	BodyTemplate string          `json:"body_template"`
	BodyFormat   string          `json:"body_format"`
	Note         string          `json:"note"`
}

func connectorTemplates() ([]ConnectorTemplate, error) {
	names, err := fs.Glob(connectorFiles, "connectors/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	all := []ConnectorTemplate{}
	for _, name := range names {
		raw, err := connectorFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var t ConnectorTemplate
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		for i := range t.Actions {
			if len(t.Actions[i].Body) > 0 {
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, t.Actions[i].Body, "", "  "); err != nil {
					return nil, err
				}
				t.Actions[i].BodyTemplate = pretty.String()
				t.Actions[i].Body = nil
			}
		}
		all = append(all, t)
	}
	return all, nil
}
