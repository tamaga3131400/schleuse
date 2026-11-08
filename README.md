# Schleuse · API-Gateway

**Deine API-Schlüssel bleiben auf deinem Server.** Ninox, Make, n8n oder ein Skript bekommen nur einen eigenen Schleuse-Token für genau freigegebene Aktionen. Schleuse prüft jeden Aufruf, setzt erst dann den Schlüssel der Ziel-API ein und leitet weiter.

```text
Ninox · Make · Skript  ──Schleuse-Token──▶  Schleuse  ──API-Schlüssel──▶  Ziel-API
                                           prüft Token, Aktion, Eingaben
```

Alles wird über die Admin-Oberfläche eingerichtet: Verbindung aus einer Vorlage wählen, Aktion testen und aktivieren, Token erstellen, fertige Vorlage für Ninox, Make oder curl kopieren. Am Server sind nach der Installation keine Handgriffe mehr nötig.

Projektseite: [schleuse.link](https://schleuse.link) · Lizenz: [GNU GPL v3](LICENSE)

## Was du damit machen kannst

| Szenario | Beispiel |
|---|---|
| **Ninox → KI** | Ein Button in Ninox lässt Claude oder OpenAI einen Text formulieren. Der KI-Schlüssel steht nie im Ninox-Skript. |
| **Make → Schleuse → Ninox** | Ein Make-Szenario liest oder schreibt Ninox-Datensätze. Make kennt nur den Schleuse-Token für diese Aktionen. |
| **Ninox → Shop** | Bestellungen aus WooCommerce oder Shopify lesen, Bestellstatus ändern. |
| **Ninox → CRM und Buchhaltung** | Kontakte in HubSpot oder Pipedrive suchen und anlegen, offene Rechnungen aus Lexware Office oder sevDesk lesen. |
| **Ninox → Automatisierung** | Daten an einen geschützten Make- oder n8n-Webhook senden. |

## Konnektor-Vorlagen

Die Vorlagen füllen Zieladresse, Anmeldeart und passende Aktionen vor. Du ergänzt nur deine IDs und den Schlüssel.

| Bereich | Vorlagen |
|---|---|
| Datenbank & Low-Code | Ninox (Cloud und On-Premise), Airtable, Notion |
| Automatisierung | Make (Webhook), n8n (Webhook) |
| CRM & Vertrieb | HubSpot, Pipedrive |
| Buchhaltung & Rechnungen | Lexware Office (lexoffice), sevDesk |
| Shop & Website | WooCommerce, Shopify, WordPress |
| Zahlungen, Kommunikation, Entwicklung | Stripe, Slack, GitHub |
| KI | Claude (Anthropic), OpenAI |
| Zum Ausprobieren | WordPress.org ohne Konto |

Getestet mit echten Systemen sind bisher WordPress.org und Ninox On-Premise (Lesen, Anlegen und Ändern über Schleuse). Die übrigen Vorlagen folgen der offiziellen Dokumentation der Anbieter. Details und eigene Vorlagen: [Konnektoren und Aktionen](docs/KONNEKTOREN.md).

## Was eine Aktion kann

- **Feste Ziele.** Der Client wählt nur die Aktion, nie die Ziel-URL.
- **Platzhalter im Pfad.** `/records/{id}` füllt der Client mit `?id=42`. Erlaubt sind nur URL-sichere Werte ohne `/`.
- **Freigegebene Query-Parameter.** Zum Beispiel `page` und `search` für Seiten und Filter. Feste Werte wie `?status=publish` setzt der Admin im Zielpfad, der Client kann sie nicht überschreiben.
- **Body-Vorlagen.** Die JSON-Struktur liegt in Schleuse, der Client liefert nur die Werte für `{{platzhalter}}`. So funktionieren Ninox-Datensätze, GraphQL-Abfragen (Shopify) und KI-Anfragen, ohne dass der Client beliebige Anfragen bauen kann.
- **Anmeldearten.** API-Schlüssel im Header (z. B. `Authorization: Bearer …`, `X-Shopify-Access-Token`), Benutzername und Passwort (Basic, z. B. WooCommerce, WordPress), API-Schlüssel als URL-Parameter oder öffentliche API ohne Schlüssel. Dazu feste Zusatz-Header wie `Notion-Version`.
- **Formular-Body** für APIs wie Stripe.
- **Antworten:** JSON oder kurzer Text bis 2 MiB, Zeitlimit 30 Sekunden. Antworten, die den Schlüssel enthalten, werden gesperrt.

## API-Tokens

Ein Token gilt für eine oder mehrere Aktionen eines Kunden. Standard ist **unbegrenzt bis zum Widerruf**. Wahlweise befristet, bei weniger als 30 Tagen fragt Schleuse nach. Ein Widerruf wirkt sofort. Schleuse zeigt den Token nur einmal an und speichert nur seinen Hash.

Für Skripte, die sich selbst anmelden, gibt es zusätzlich persönliche Zugänge mit Benutzername und Passwort (`POST /auth/login`, Token 15 Minuten).

## Installation

Linux-Server mit Docker Compose. Der Installer prüft freie Ports und richtet API und Verwaltung auf getrennten Adressen ein. Schritt für Schritt: [Installation und Betrieb](docs/INSTALLATION.md).

```bash
./install-server.sh --plan --show-used
./install-server.sh --prepare
./install-server.sh --install
```

Beim ersten Aufruf der Verwaltung richtest du mit dem Einrichtungscode aus dem Datenordner den Administrator ein.

## Lokal ausprobieren

Mit Go 1.27.3+, Node 22.12+ und npm:

```bash
./start-local.sh --demo
```

- Verwaltung: http://127.0.0.1:19090 mit `demo` / `Lokal-testen-2026!`
- Client-API: http://127.0.0.1:18080
- Beispiel-Client: `python3 scripts/demo-client.py`

Die Demo-Zugänge sind öffentlich bekannte Testwerte und laufen nur auf Loopback. **Keine echten Schlüssel in die Demo eintragen.** Für eigene Tests `./start-local.sh` ohne `--demo` starten; der Einrichtungscode liegt dann in `.data/setup-code`.

## Sicherheit

- Anbieter-Schlüssel AES-GCM-verschlüsselt in SQLite, Master-Key getrennt mit Rechten 0600. Keine Anzeige, kein Export im Klartext.
- Verwaltung und Client-API auf getrennten Listenern. Admin mit HttpOnly-Cookie, Origin- und CSRF-Prüfung. Passwörter mit Argon2id, Tokens nur als Hash gespeichert.
- Netzwerkprüfung direkt vor dem Verbindungsaufbau: interne Adressen nur nach ausdrücklicher Freigabe, Metadaten- und Link-Local-Adressen immer gesperrt, keine Redirects.
- Keine Client-Header, Cookies oder Anbieter-Header werden durchgereicht. Eingaben werden gegen die Freigabe geprüft und neu aufgebaut.
- Grenzen: 512 KiB Anfrage, 2 MiB Antwort, 30 Sekunden, acht gleichzeitige Weiterleitungen, 60 Aufrufe pro Token und Minute, zehn Anmeldeversuche pro IP und Minute.

Wer ein Ninox-Skript oder ein Make-Szenario einsehen kann, sieht den Schleuse-Token. Er erlaubt aber nur die freigegebenen Aktionen und lässt sich sofort widerrufen. Ein kompromittierter Server kann die Schlüssel zur Laufzeit lesen.

## Noch nicht enthalten

Automatische OAuth-Erneuerung (Google, Microsoft 365), Datei-Uploads und -Downloads, Streaming, CORS für direkte Browseraufrufe, Änderungsprotokoll der Verwaltung, Backup und Restore per Oberfläche.

## Speicher und Wiederherstellung

Daten liegen in `.data/` beziehungsweise im Docker-Volume `gateway-data`:

- `gateway.db`: Konfiguration, Hashes, Freigaben und verschlüsselte Schlüssel.
- `master.key`: 32 Bytes, zum Entschlüsseln zwingend nötig.

Für ein Backup den Container stoppen und beide Dateien sichern, den Master-Key getrennt und geschützt. **Ohne den passenden Master-Key sind die Schlüssel nicht wiederherstellbar.** Der Export in der Oberfläche enthält nur Metadaten und ist kein Backup. Optional zeigt `GATEWAY_MASTER_KEY_FILE` auf eine externe Schlüsseldatei.

## Entwicklung und Prüfung

```bash
./test.sh
```

Baut die Oberfläche, führt Installer- und Pakettests sowie die Go-Tests mit Race Detector aus und baut das Programm. Aufbau und Datenfluss: [Architektur](docs/ARCHITEKTUR.md) · Prüfstand: [Validierung](docs/VALIDIERUNG.md) · Ninox-Beispiele: [Ninox](docs/NINOX.md) · Weitergabepaket: `./package-server.sh --format zip`.
