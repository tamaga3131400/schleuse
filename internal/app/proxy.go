package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func allowedIP(ip net.IP, private, plainHTTP bool) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	local := addr.IsLoopback() || addr.IsPrivate()
	return (!local || private) && (!plainHTTP || local)
}
func targetTransport(u *url.URL, private bool) *http.Transport {
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 28 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 2, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		h, p, e := net.SplitHostPort(address)
		if e != nil || !strings.EqualFold(h, u.Hostname()) || p != port {
			return nil, errors.New("Ziel nicht freigegeben")
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ips, e := net.DefaultResolver.LookupIP(ctx, "ip", h)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, errors.New("DNS ohne Adresse")
		}
		for _, ip := range ips {
			if !allowedIP(ip, private, u.Scheme == "http") {
				return nil, errors.New("Zieladresse durch Netzwerkregel gesperrt")
			}
		}
		var last error
		for _, ip := range ips {
			c, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), p))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}}
}
func containsSecret(b []byte, secret, header string) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		if value, ok := tok.(string); ok && (strings.Contains(value, secret) || strings.Contains(value, header)) {
			return true
		}
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(s int) {
	if w.status == 0 {
		w.status = s
		w.ResponseWriter.WriteHeader(s)
	}
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (a *App) forward(w http.ResponseWriter, r *http.Request, act Action, c Connection) {
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	defer func() {
		if sw.status == 0 {
			sw.status = 500
		}
		a.Store.event(act.Slug, sw.Header().Get("X-Gateway-Source"), sw.status, time.Since(start))
	}()
	sw.Header().Set("X-Gateway-Source", "gateway")
	sw.Header().Set("X-Request-ID", randomID())
	sw.Header().Set("Cache-Control", "no-store")
	if r.Method != act.Method {
		fail(sw, 405, "HTTP-Methode für diese Aktion nicht freigegeben.")
		return
	}
	path, fixedQuery, _ := strings.Cut(act.Path, "?")
	query, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		fail(sw, 400, "Query-Parameter sind ungültig kodiert.")
		return
	}
	values := map[string]string{}
	for _, name := range pathParams(path) {
		v := query.Get(name)
		if len(query[name]) != 1 || !pathValuePattern.MatchString(v) || v == "." || v == ".." {
			fail(sw, 400, "Pflichtwert „"+name+"“ fehlt oder enthält unerlaubte Zeichen.")
			return
		}
		values[name] = v
		query.Del(name)
	}
	upstreamPath := pathPlaceholder.ReplaceAllStringFunc(path, func(m string) string { return values[m[1:len(m)-1]] })
	upstreamQuery, _ := url.ParseQuery(fixedQuery)
	allowedQuery := map[string]bool{}
	for _, q := range act.Query {
		allowedQuery[q] = true
	}
	for k, vs := range query {
		if !allowedQuery[k] {
			if queryNamePattern.MatchString(k) {
				fail(sw, 400, "Nicht freigegebener Parameter: "+k)
			} else {
				fail(sw, 400, "Nicht freigegebener Parameter.")
			}
			return
		}
		if len(vs) != 1 || len(vs[0]) > 1000 || strings.IndexFunc(vs[0], unicode.IsControl) >= 0 {
			fail(sw, 400, "Parameter "+k+": genau ein Wert mit höchstens 1000 Zeichen.")
			return
		}
		upstreamQuery.Set(k, vs[0])
	}
	var outBody []byte
	contentType := ""
	var data map[string]any
	if r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(sw, r.Body, 512*1024)
		body, e := io.ReadAll(r.Body)
		if e != nil {
			fail(sw, 413, "Anfrage zu groß.")
			return
		}
		if len(body) > 0 {
			if r.Method == "GET" || r.Method == "DELETE" {
				fail(sw, 400, "Diese Aktion akzeptiert keinen Request-Body.")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || decodeObject(body, &data) != nil || data == nil {
				fail(sw, 400, "Ein JSON-Objekt ist erforderlich.")
				return
			}
			allowed := map[string]bool{}
			for _, f := range act.Fields {
				allowed[f] = true
			}
			for k, v := range data {
				if !allowed[k] {
					fail(sw, 400, "Nicht freigegebenes Eingabefeld.")
					return
				}
				switch v.(type) {
				case nil, bool, string, json.Number:
				default:
					fail(sw, 400, "Verschachtelte Eingaben sind nicht freigegeben. Die Struktur legt die Body-Vorlage fest.")
					return
				}
			}
		}
	}
	if act.BodyTemplate != "" {
		for _, f := range act.Fields {
			if _, ok := data[f]; !ok {
				fail(sw, 400, "Pflichtfeld „"+f+"“ fehlt.")
				return
			}
		}
		if outBody, e = renderTemplate(act.BodyTemplate, data); e != nil {
			fail(sw, 503, "Body-Vorlage der Aktion ist ungültig.")
			return
		}
	} else if data != nil {
		outBody, _ = json.Marshal(data)
	}
	if outBody != nil {
		contentType = "application/json"
		if act.BodyFormat == "form" {
			var flat map[string]any
			if decodeObject(outBody, &flat) != nil {
				fail(sw, 503, "Formular-Body muss ein einfaches Objekt sein.")
				return
			}
			form := url.Values{}
			for k, v := range flat {
				form.Set(k, scalarText(v))
			}
			outBody = []byte(form.Encode())
			contentType = "application/x-www-form-urlencoded"
		}
		r.Body = io.NopCloser(bytes.NewReader(outBody))
		r.ContentLength = int64(len(outBody))
	} else {
		r.Body = http.NoBody
		r.ContentLength = 0
	}
	select {
	case a.proxySlots <- struct{}{}:
		defer func() { <-a.proxySlots }()
	default:
		fail(sw, 429, "Gateway ausgelastet. Bitte später erneut versuchen.")
		return
	}
	var secret string
	if c.authMode() != "none" {
		var e error
		secret, e = a.Store.secret(c.ID)
		if e != nil || secret == "" {
			fail(sw, 503, "Zugangsdaten nicht verfügbar.")
			return
		}
	}
	target, e := url.Parse(c.URL)
	if e != nil {
		fail(sw, 503, "Zielkonfiguration ungültig.")
		return
	}
	transport := targetTransport(target, c.AllowPrivate)
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	proxy := &httputil.ReverseProxy{Transport: transport, ErrorLog: log.New(io.Discard, "", 0), Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		if upstreamPath == "/" && strings.Trim(target.Path, "/") != "" {
			p.Out.URL.Path = target.Path
		} else {
			p.Out.URL.Path = strings.TrimRight(target.Path, "/") + upstreamPath
		}
		p.Out.URL.RawPath = ""
		q := url.Values{}
		for k, v := range upstreamQuery {
			q[k] = v
		}
		if c.authMode() == "query" {
			q.Set(c.Header, secret)
		}
		p.Out.URL.RawQuery = q.Encode()
		p.Out.Header = make(http.Header)
		for _, h := range c.Headers {
			p.Out.Header.Set(h.Name, h.Value)
		}
		p.Out.Header.Set("Accept", "application/json")
		if contentType != "" {
			p.Out.Header.Set("Content-Type", contentType)
		}
		if mode := c.authMode(); mode == "header" || mode == "basic" {
			p.Out.Header.Set(c.Header, c.Prefix+secret)
		}
		p.Out.Header.Set("X-Request-ID", sw.Header().Get("X-Request-ID"))
		p.Out.Header.Set("User-Agent", "Schleuse/"+Version)
		p.Out.Close = true
	}, ModifyResponse: func(resp *http.Response) error {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			resp.Body.Close()
			return errors.New("Redirect gesperrt")
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
		resp.Body.Close()
		if e != nil {
			return e
		}
		if len(b) > 2*1024*1024 {
			return errors.New("Antwort zu groß")
		}
		if len(b) > 0 {
			if !json.Valid(b) {
				// Kurze Textantworten (z. B. „Accepted“ von Webhooks) als JSON verpacken; HTML und Binärdaten bleiben gesperrt.
				if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || len(b) > 64*1024 || !utf8.Valid(b) {
					return errors.New("Nur JSON- oder kurze Textantworten freigegeben")
				}
				b, _ = json.Marshal(map[string]string{"text": string(b)})
			}
			if secret != "" && containsSecret(b, secret, c.Prefix+secret) {
				return errors.New("Antwort enthält Zugangsdaten")
			}
		}
		resp.Header = make(http.Header)
		resp.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
		resp.Header.Set("Cache-Control", "no-store")
		sw.Header().Set("X-Gateway-Source", "upstream")
		resp.Body = io.NopCloser(bytes.NewReader(b))
		resp.ContentLength = int64(len(b))
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		source := "gateway"
		msg := "Zielantwort nicht verfügbar oder durch Antwortregel gesperrt."
		status := 502
		var dns *net.DNSError
		var cert x509.UnknownAuthorityError
		var host x509.HostnameError
		var tlsErr *tls.CertificateVerificationError
		var neterr net.Error
		switch {
		case errors.As(err, &dns):
			source = "dns"
			msg = "Zielname konnte nicht aufgelöst werden."
		case errors.As(err, &cert) || errors.As(err, &host) || errors.As(err, &tlsErr):
			source = "tls"
			msg = "Zertifikat der Ziel-API konnte nicht bestätigt werden."
		case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &neterr) && neterr.Timeout()):
			source = "timeout"
			status = 504
			msg = "Zeitlimit der Zielverbindung erreicht."
		}
		w.Header().Set("X-Gateway-Source", source)
		fail(w, status, msg)
	}}
	proxy.ServeHTTP(sw, r)
}

func decodeObject(b []byte, data *map[string]any) error {
	if !json.Valid(b) {
		return errors.New("Ungültiges JSON")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(data)
}

var (
	pathPlaceholder  = regexp.MustCompile(`\{([a-z][a-z0-9_]{0,49})\}`)
	pathValuePattern = regexp.MustCompile(`^[A-Za-z0-9._~@:+-]{1,200}$`)
	templateVar      = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)
	templateWhole    = regexp.MustCompile(`^\{\{\s*([a-z][a-z0-9_]{0,49})\s*\}\}$`)
)

func pathParams(path string) []string {
	names := []string{}
	for _, m := range pathPlaceholder.FindAllStringSubmatch(path, -1) {
		names = append(names, m[1])
	}
	return names
}

func scalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// templateFields prüft eine Body-Vorlage und liefert die Namen ihrer {{platzhalter}}.
func templateFields(tpl string, flat bool) ([]string, error) {
	if len(tpl) > 64*1024 {
		return nil, errors.New("Body-Vorlage ist zu groß (maximal 64 KB)")
	}
	var root any
	dec := json.NewDecoder(strings.NewReader(tpl))
	dec.UseNumber()
	if dec.Decode(&root) != nil || dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("Body-Vorlage ist kein gültiges JSON")
	}
	switch root.(type) {
	case map[string]any, []any:
	default:
		return nil, errors.New("Body-Vorlage muss ein JSON-Objekt oder eine Liste sein")
	}
	found := map[string]bool{}
	var walk func(v any, depth int) error
	walk = func(v any, depth int) error {
		if depth > 20 {
			return errors.New("Body-Vorlage ist zu tief verschachtelt")
		}
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if strings.Contains(k, "{{") {
					return errors.New("Platzhalter sind nur in Werten erlaubt, nicht in Feldnamen")
				}
				if flat && depth > 0 {
					return errors.New("Formular-Vorlagen müssen ein einfaches Objekt sein")
				}
				if e := walk(e, depth+1); e != nil {
					return e
				}
			}
		case []any:
			if flat {
				return errors.New("Formular-Vorlagen müssen ein einfaches Objekt sein")
			}
			for _, e := range x {
				if e := walk(e, depth+1); e != nil {
					return e
				}
			}
		case string:
			for _, m := range templateVar.FindAllStringSubmatch(x, -1) {
				if !paramNamePattern.MatchString(m[1]) {
					return fmt.Errorf("Platzhalter {{%s}}: Kleinbuchstaben, Ziffern und _ verwenden", m[1])
				}
				found[m[1]] = true
			}
		}
		return nil
	}
	if flat {
		if _, ok := root.(map[string]any); !ok {
			return nil, errors.New("Formular-Vorlagen müssen ein einfaches Objekt sein")
		}
	}
	if e := walk(root, 0); e != nil {
		return nil, e
	}
	if len(found) > 32 {
		return nil, errors.New("Maximal 32 Platzhalter")
	}
	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// renderTemplate setzt Client-Werte in die Vorlage ein. Ein Wert, der nur aus einem Platzhalter besteht,
// behält den JSON-Typ des Client-Werts; sonst wird der Wert als Text eingefügt.
func renderTemplate(tpl string, data map[string]any) ([]byte, error) {
	var root any
	dec := json.NewDecoder(strings.NewReader(tpl))
	dec.UseNumber()
	if e := dec.Decode(&root); e != nil {
		return nil, e
	}
	var fill func(v any) any
	fill = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				x[k] = fill(e)
			}
		case []any:
			for i := range x {
				x[i] = fill(x[i])
			}
		case string:
			if m := templateWhole.FindStringSubmatch(x); m != nil {
				return data[m[1]]
			}
			return templateVar.ReplaceAllStringFunc(x, func(s string) string {
				return scalarText(data[templateVar.FindStringSubmatch(s)[1]])
			})
		}
		return v
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if e := enc.Encode(fill(root)); e != nil {
		return nil, e
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}
