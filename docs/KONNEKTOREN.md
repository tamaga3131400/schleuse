# Konnektoren und Aktionen

Eine **Verbindung** beschreibt eine Ziel-API: Adresse, Anmeldeart und Schlüssel. Eine **Aktion** ist ein einzelner erlaubter Aufruf dieser API. Clients rufen ausschließlich `/actions/<aktionsname>` mit einem Schleuse-Token auf.

## Schnellstart in der Oberfläche

1. **API-Verbindungen → Aus Vorlage**: Anbieter wählen, Kunde eintragen, großgeschriebene Platzhalter wie `{TEAM_ID}` durch deine Werte ersetzen, Schlüssel eintragen.
2. **Aktionen**: Leseaktionen mit **Testen** prüfen. Die Vorschau zeigt die echte Antwort der Ziel-API. Danach die gewünschten Aktionen aktivieren.
3. **API-Tokens → Token erstellen**: Aktionen auswählen, Laufzeit „unbegrenzt“ lassen oder befristen.
4. **Vorlage** an der Aktion oder im Token-Dialog: fertiges Skript für Ninox, Einstellungen für Make oder einen curl-Befehl kopieren.

Für den ersten Versuch ohne eigenes Konto eignet sich die Vorlage **WordPress.org (Testquelle)**.

## Was eine Aktion kann

| Baustein | Beispiel | Bedeutung |
|---|---|---|
| Fester Zielpfad | `/crm/v3/objects/contacts` | Wird an die Zieladresse der Verbindung angehängt. `/` steht für genau die Zieladresse, etwa bei Webhooks. |
| Platzhalter im Pfad | `/orders/{id}` | Der Client übergibt `?id=42`. Pflichtwert, erlaubt sind Buchstaben, Ziffern und `. _ ~ @ : + -`, kein `/`. |
| Feste Query-Parameter | `/voucherlist?voucherType=invoice` | Setzt der Admin. Der Client kann sie nicht ändern. |
| Freigegebene Query-Parameter | `page, per_page, search` | Der Client darf genau diese Parameter mitgeben, je einmal und bis 1000 Zeichen. Alle anderen werden mit `400` abgelehnt. |
| Einfache Felder | `name, email` | Für POST, PUT und PATCH: Der Client sendet ein flaches JSON-Objekt nur mit diesen Feldern. |
| Body-Vorlage | `{"properties": {"email": "{{email}}"}}` | Die Struktur bleibt in Schleuse. Der Client sendet nur `{"email": "…"}`. Steht ein Platzhalter allein in einem Wert, bleibt der Typ des Client-Werts erhalten (Zahl, Wahr/Falsch). In längeren Texten wird er als Text eingefügt. Alle Platzhalter sind Pflicht. |
| Formular-Body | Stripe | Schleuse sendet die Werte als `application/x-www-form-urlencoded`. |

Verschachtelte Werte vom Client sind nicht erlaubt. GET und DELETE senden keinen Body.

## Anmeldearten einer Verbindung

| Anmeldeart | Beispiel | Hinweis |
|---|---|---|
| API-Schlüssel im Header | `Authorization: Bearer …`, `X-Shopify-Access-Token`, `x-api-key` | Headername und Präfix frei wählbar, reservierte Header sind gesperrt. |
| Benutzername + Passwort | WooCommerce, WordPress-Anwendungspasswort | Schleuse bildet daraus `Authorization: Basic …`. |
| API-Schlüssel als URL-Parameter | `?api_key=…` | Nur wenn die API nichts anderes anbietet; der Schlüssel kann in Logs des Anbieters landen. |
| Ohne Schlüssel | WordPress.org | Nur für öffentliche HTTPS-APIs. Clients brauchen trotzdem einen Schleuse-Token. |

Feste **Zusatz-Header** (z. B. `Notion-Version: 2022-06-28`) sind nicht geheim und erscheinen in der Oberfläche.

## Vorlagen

„Nach Anbieter-Doku“ heißt: nach der offiziellen Dokumentation erstellt, noch nicht mit einem echten Konto geprüft. Bitte die Leseaktionen nach dem Anlegen testen.

| Anbieter | Stand | Anmeldung | Aktionen |
|---|---|---|---|
| [WordPress.org (Testquelle)](https://developer.wordpress.org/rest-api/reference/posts/) | getestet | ohne Schlüssel | `GET /posts?per_page=5&_fields=id,date,link,title` News-Beiträge lesen |
| [Ninox](https://docs.ninox.com/en/api/introduction) | getestet | Header `Authorization` | `GET /tables` Tabellen auflisten<br>`GET /tables/{TABLE_ID}/records` Datensätze lesen<br>`GET /tables/{TABLE_ID}/records/{id}` Datensatz lesen<br>`POST /tables/{TABLE_ID}/records` Datensatz anlegen<br>`POST /tables/{TABLE_ID}/records` Datensatz ändern |
| [Airtable](https://airtable.com/developers/web/api/introduction) | nach Anbieter-Doku | Header `Authorization` | `GET /{TABLE_ID}` Datensätze lesen<br>`GET /{TABLE_ID}/{id}` Datensatz lesen<br>`POST /{TABLE_ID}` Datensatz anlegen |
| [Notion](https://developers.notion.com/reference/intro) | nach Anbieter-Doku | Header `Authorization` | `POST /databases/{DATABASE_ID}/query` Datenbank abfragen<br>`GET /pages/{id}` Seite lesen<br>`POST /pages` Eintrag anlegen |
| [Make (Webhook)](https://help.make.com/webhooks) | nach Anbieter-Doku | Header `x-make-apikey` | `POST /` Daten senden |
| [n8n (Webhook)](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.webhook/) | nach Anbieter-Doku | Header `X-Schleuse-Key` | `POST /` Daten senden |
| [HubSpot CRM](https://developers.hubspot.com/docs/api/crm/contacts) | nach Anbieter-Doku | Header `Authorization` | `GET /crm/v3/objects/contacts` Kontakte lesen<br>`GET /crm/v3/objects/contacts/{id}` Kontakt lesen<br>`POST /crm/v3/objects/contacts/search` Kontakt per E-Mail suchen<br>`POST /crm/v3/objects/contacts` Kontakt anlegen |
| [Pipedrive](https://developers.pipedrive.com/docs/api/v1) | nach Anbieter-Doku | Header `x-api-token` | `GET /persons` Personen lesen<br>`GET /persons/search` Person suchen<br>`GET /deals` Deals lesen<br>`POST /persons` Person anlegen |
| [Lexware Office (lexoffice)](https://developers.lexware.io/docs/) | nach Anbieter-Doku | Header `Authorization` | `GET /contacts` Kontakte lesen<br>`GET /contacts/{id}` Kontakt lesen<br>`GET /voucherlist?voucherType=invoice&voucherStatus=open` Offene Rechnungen lesen<br>`GET /invoices/{id}` Rechnung lesen |
| [sevDesk](https://api.sevdesk.de/) | nach Anbieter-Doku | Header `Authorization` | `GET /Contact` Kontakte lesen<br>`GET /Invoice` Rechnungen lesen<br>`GET /Invoice/{id}` Rechnung lesen |
| [WooCommerce](https://woocommerce.github.io/woocommerce-rest-api-docs/) | nach Anbieter-Doku | Benutzername + Passwort | `GET /orders` Bestellungen lesen<br>`GET /orders/{id}` Bestellung lesen<br>`PUT /orders/{id}` Bestellstatus ändern<br>`GET /products` Produkte lesen |
| [Shopify](https://shopify.dev/docs/api/admin-graphql/latest) | nach Anbieter-Doku | Header `X-Shopify-Access-Token` | `POST /graphql.json` Letzte Bestellungen lesen<br>`POST /graphql.json` Produkte suchen |
| [WordPress (eigene Website)](https://developer.wordpress.org/rest-api/reference/posts/) | nach Anbieter-Doku | Benutzername + Passwort | `GET /posts` Beiträge lesen<br>`GET /posts/{id}` Beitrag lesen<br>`POST /posts` Entwurf anlegen |
| [Stripe](https://docs.stripe.com/api) | nach Anbieter-Doku | Header `Authorization` | `GET /customers` Kunden lesen<br>`GET /customers/{id}` Kunde lesen<br>`GET /balance` Kontostand lesen<br>`POST /customers` Kunde anlegen |
| [Slack](https://api.slack.com/methods/chat.postMessage) | nach Anbieter-Doku | Header `Authorization` | `POST /chat.postMessage` Nachricht senden |
| [GitHub](https://docs.github.com/en/rest) | nach Anbieter-Doku | Header `Authorization` | `GET /repos/{OWNER}/{REPO}` Repository lesen<br>`GET /repos/{OWNER}/{REPO}/issues` Issues lesen<br>`POST /repos/{OWNER}/{REPO}/issues` Issue anlegen |
| [Claude (Anthropic)](https://platform.claude.com/docs/en/api/messages) | nach Anbieter-Doku | Header `x-api-key` | `POST /messages` Text erzeugen |
| [OpenAI](https://platform.openai.com/docs/api-reference/responses) | nach Anbieter-Doku | Header `Authorization` | `POST /responses` Text erzeugen |

Die KI-Vorlagen halten Antworten kurz, weil Schleuse höchstens 30 Sekunden auf die Ziel-API wartet. Modell und Anweisung lassen sich in der Body-Vorlage anpassen.

## Eigene Vorlage beitragen

Jede Vorlage ist eine JSON-Datei in [`internal/app/connectors`](../internal/app/connectors). Großgeschriebene Platzhalter wie `{SHOP}` füllt der Admin aus, kleingeschriebene wie `{id}` und `{{email}}` liefert der Client. `status` ist `getestet`, `teilweise` oder `doku`. Der Test `TestConnectorTemplatesAreValid` prüft jede Vorlage gegen dieselben Regeln wie die Oberfläche. Eine neue Vorlage bitte mindestens mit einer Leseaktion gegen ein echtes Testkonto prüfen.

## Grenzen

Noch nicht unterstützt: automatische OAuth-Erneuerung (Google, Microsoft 365, Shopify-Apps mit kurzlebigen Tokens), Datei-Uploads und -Downloads, Streaming, Antworten über 2 MiB, Aufrufe über 30 Sekunden.
