# Schleuse zur Veröffentlichung vorbereiten

Das ZIP enthält die Projektseite und den Quellcode des Gateways. Die Projektseite liegt in `site/` (Deutsch in `index.html`, Englisch in `en/index.html`). Sie lässt sich direkt im Browser öffnen und simuliert zwei API-Aufrufe mit Beispieldaten. Der eigentliche Gateway läuft separat in Docker.

## Prüffassung weitergeben

Erstelle das ZIP im Projektordner und prüfe die SHA-256-Datei neben dem Archiv.

```bash
./package-server.sh --format zip
cd dist
sha256sum -c schleuse-server.zip.sha256
```

Auf macOS lautet die Prüfsummen-Anweisung `shasum -a 256 -c schleuse-server.zip.sha256`. Das ZIP verwendet eine feste Dateiliste und prüft die enthaltenen Textdateien auf typische Zugangsdaten und lokale Pfade. Für eigene private Kennzeichen kannst du im Arbeitsordner `.release-private-markers` anlegen. Trage dort je Zeile einen Domainnamen, eine IP-Adresse oder einen Pfad ein. Die Datei bleibt lokal. Prüfe vor der Freigabe auch den tatsächlichen Inhalt des Archivs. Betriebsnotizen, Sicherungen und Zugangsdaten gehören in einen privaten Ordner.

## GitHub und Projektseite

1. Den [GPL-v3-Lizenztext](../LICENSE) im Repository übernehmen und die Rechte an zusätzlich eingebrachten Inhalten vor der Veröffentlichung klären.
2. `schleuse-server.zip` entpacken und den Inhalt des Ordners `schleuse-server` als Stamm eines neuen Repositorys mit Branch `main` verwenden. Die versteckten Dateien `.gitignore` und `.dockerignore` mit übernehmen. In `site/index.html` und `site/en/index.html` `data-repository-url` auf die eigene GitHub-Repository-URL setzen. Dadurch erscheinen der GitHub-Link und der Link zur Installationsanleitung.
3. Die Projektseite besteht nur aus statischen Dateien. Für eine eigene Domain den Inhalt von `site/` in das Webverzeichnis eines Webservers kopieren und HTTPS einrichten. Impressum und Datenschutzerklärung gehören nicht ins Repository: Sie liegen direkt auf dem Webserver neben `index.html` (`impressum.html`, `datenschutz.html`, `en/legal-notice.html`, `en/privacy.html`). Darauf verlinkt der Footer.

Für eine eigene Gateway-Instanz gilt die [Installationsanleitung](INSTALLATION.md). [Architektur und Weiterentwicklung](ARCHITEKTUR.md) beschreibt den Datenfluss und die Schutzgrenzen.
