# Schleuse installieren und betreiben

Schleuse läuft als Docker-Compose-Dienst. Für den Internetzugriff übernimmt ein Reverse-Proxy die HTTPS-Verbindung zum Browser oder API-Client und leitet die Anfragen lokal an Schleuse weiter. Das funktioniert mit einem bestehenden Proxy oder mit dem mitgelieferten Caddy-Beispiel. Ein Hosting-Panel ist optional.

## 1. Voraussetzungen und Adressen

- Linux-Server mit lokalem Docker Engine, Compose V2 mit `up --wait` und `--wait-timeout`, Python 3.9+, Bash, `curl` und einem Benutzer mit Zugriff auf Docker.
- Netzwerkzugriff auf die Bezugsquellen für Docker-Basisimages und Build-Abhängigkeiten. Go und Node laufen beim Docker-Build in den Build-Containern.
- Für öffentliche Nutzung eigene DNS-Namen, HTTPS-Zertifikate und einen Reverse-Proxy. Der bereits für 80/443 zuständige Dienst kann die neuen Namen übernehmen.

`api.example.com` und `admin.example.com` sind Platzhalter. Ersetze sie durch deine Namen. Die Host-Ports sind Vorschläge. Verwende im weiteren Ablauf die tatsächlichen Werte aus der Installer-Ausgabe.

| Zugang | Öffentliche Adresse | Ziel für einen Proxy auf dem Host |
|---|---|---|
| Client-API | `https://api.example.com` | `http://127.0.0.1:18080` |
| Optionale Webverwaltung | `https://admin.example.com` | `http://127.0.0.1:19090` |

## 2. Paket entpacken und Gateway starten

Entpacke das Archiv in einen eigenen Projektordner außerhalb eines öffentlich ausgelieferten Webverzeichnisses. Die beigefügte SHA-256-Datei erlaubt eine Prüfung mit `sha256sum -c schleuse-server.zip.sha256` auf Linux oder `shasum -a 256 -c schleuse-server.zip.sha256` auf macOS.

```bash
unzip schleuse-server.zip
cd schleuse-server
./install-server.sh --plan --show-used --range 18000-19999
```

Die Vorschau liest Host-Listener und Docker-Portfreigaben. Sie berücksichtigt auch veröffentlichte Ports gestoppter Container und prüft freie Kandidaten lokal. Danach die Auswahl speichern und den Gateway starten:

```bash
./install-server.sh --prepare --range 18000-19999
./install-server.sh --install
```

Die API und die Verwaltung sind zunächst auf dem Loopback des Servers gebunden. Die Datei `install.env` enthält die gewählten Ports, Adressen und den Projektnamen. Für feste Host-Ports gibt es `--api-port` und `--admin-port`. `--exclude 18081,19091` nimmt weitere Ports aus der Auswahl. `--project` ist für eine neue, getrennte Installation gedacht. Bei Updates im selben Projektordner bleiben Projektname und Datenvolume zusammen.

```bash
docker compose --env-file install.env --project-name schleuse-pilot ps gateway
curl -fsS http://127.0.0.1:18080/healthz
```

Erwartet wird `{"status":"ok"}`. Der Installer wartet nach dem Start auf den Docker-Healthcheck. Für manuelle Compose-Befehle dieselbe Datei und den gespeicherten Projektnamen verwenden. [Docker Compose up](https://docs.docker.com/reference/cli/docker/compose/up/)

## 3. Ersten Administrator einrichten

Auf dem Server aus dem Projektordner den einmaligen Einrichtungscode in einem privaten temporären Ordner lesen:

```bash
setup_dir=$(mktemp -d)
docker compose --env-file install.env --project-name schleuse-pilot \
  cp gateway:/data/setup-code "$setup_dir/setup-code"
chmod 600 "$setup_dir/setup-code"
cat "$setup_dir/setup-code"
```

Für den ersten Browserzugriff auf deinem Rechner einen Tunnel öffnen:

```bash
ssh -N -L 127.0.0.1:19090:127.0.0.1:19090 BENUTZER@SERVER
```

Der lokale Tunnelport muss zur gespeicherten Admin-Adresse passen. Öffne `http://127.0.0.1:19090`, gib den Einrichtungscode ein und vergib ein eigenes Administratorpasswort mit mindestens zwölf Zeichen. Schleuse entfernt den Code im Container nach erfolgreicher Einrichtung. Lösche anschließend deine temporäre Kopie. Wenn der Administrator bereits eingerichtet ist, erscheint direkt die Anmeldung.

## 4. Client-API über eine Domain erreichbar machen

1. DNS für `api.example.com` auf den Proxy zeigen lassen. A- und AAAA-Einträge müssen zur tatsächlich erreichbaren Infrastruktur passen.
2. Im bestehenden Proxy einen eigenen HTTPS-Host konfigurieren. Der Proxy leitet den gesamten Pfad unverändert an den **API-Port** weiter. TLS am Proxy beenden, HTTP auf HTTPS umleiten, API-Cache ausschalten, `Authorization` und Fehlercodes erhalten. Bei einem Proxy im Docker-Netz ist stattdessen `gateway:8080` das Ziel, wenn beide Dienste gezielt dieses Netz teilen.
3. Öffentliche API-Adresse in Schleuse speichern und den Gateway mit dem vorhandenen Image neu erstellen:

   ```bash
   ./install-server.sh --prepare --domain api.example.com &&
     docker compose --env-file install.env --project-name schleuse-pilot up -d --no-build --wait gateway
   curl -fsS https://api.example.com/healthz
   ```

Beispiel für den `location`-Block im HTTPS-Server von Nginx. Der umgebende Serverblock und das Zertifikat kommen aus deiner Nginx-Installation oder deinem Hosting-Panel.

```nginx
location / {
    proxy_pass http://127.0.0.1:18080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Authorization $http_authorization;
    proxy_cache off;
    proxy_intercept_errors off;
    proxy_next_upstream off;
}
```

Bei Nginx vor dem Neuladen mit `nginx -t` prüfen. Caddy, Traefik und Hosting-Panels können dieselbe Zuordnung übernehmen. Bei einem Container-Proxy zeigt dessen `127.0.0.1` auf den Proxy-Container selbst. [Nginx Proxy-Modul](https://nginx.org/en/docs/http/ngx_http_proxy_module.html) · [Docker Compose-Netzwerke](https://docs.docker.com/compose/how-tos/networking/)

### Optionaler Caddy-Proxy

Wenn die Host-Ports 80/443 frei sind und der DNS-Name auf den Server zeigt, kannst du anstelle des bestehenden Proxys das Overlay verwenden. Zuerst den Gateway wie oben installieren und mit `--prepare --domain api.example.com` vorbereiten. Danach:

```bash
GATEWAY_DOMAIN=api.example.com \
  docker compose --env-file install.env --project-name schleuse-pilot \
  -f compose.yaml -f compose/caddy.yaml up -d --no-build --wait
```

Das Overlay verbindet Caddy mit `gateway:8080` und speichert seine Zertifikatsdaten in eigenen Volumes. Bei weiteren Starts mit dem Overlay dieselben beiden Compose-Dateien und `GATEWAY_DOMAIN` verwenden. Für diesen Aufbau bleibt die Verwaltung zunächst per SSH-Tunnel erreichbar. [Caddy Automatic HTTPS](https://caddyserver.com/docs/automatic-https)

## 5. Optional die Verwaltung über HTTPS öffnen

1. Nach der Ersteinrichtung eine **eigene** Admin-Subdomain mit gültigem Zertifikat einrichten. Für den gesamten Host einen Zugangsschutz vor dem Gateway aktivieren, beispielsweise VPN, Identitätsproxy oder HTTP Basic Auth über HTTPS. Cache ausschalten und HTTP vor der Anmeldemaske auf HTTPS umleiten.
2. Den geschützten Proxy auf den **Admin-Port** richten. Den öffentlichen Admin-Hostnamen als `Host` weitergeben. Der Proxy soll den für die Schleuse-Anmeldung erforderlichen `Origin` und die Cookies unverändert durchreichen.
3. Die Admin-Adresse speichern und den Gateway neu erstellen:

   ```bash
   ./install-server.sh --prepare --admin-domain admin.example.com &&
     docker compose --env-file install.env --project-name schleuse-pilot up -d --no-build --wait gateway
   ```

Beispiel für den geschützten `location`-Block im HTTPS-Server von Nginx. Lege für `auth_basic_user_file` eine eigene Passwortdatei mit gehashten Kennwörtern an, etwa mit `htpasswd`, und speichere sie außerhalb des Webverzeichnisses.

```nginx
location / {
    auth_basic "Schleuse-Verwaltung";
    auth_basic_user_file /etc/nginx/schleuse-admin.htpasswd;
    proxy_pass http://127.0.0.1:19090;
    proxy_set_header Host admin.example.com;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Authorization "";
    proxy_cache off;
    proxy_intercept_errors off;
    proxy_next_upstream off;
}
```

Die leere `Authorization`-Weiterleitung betrifft nur die vorgeschaltete Nginx-Anmeldung im Admin-Host. Schleuse verwendet für die eigene Verwaltung einen Sitzungscookie. Bei Caddy im selben Compose-Netz entspricht die geschützte Weiterleitung `basic_auth` mit einem Passwort-Hash und `reverse_proxy gateway:9090`. [Nginx Basic Auth](https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html) · [Caddy Basic Auth](https://caddyserver.com/docs/caddyfile/directives/basic_auth)

Prüfe die HTTP-Umleitung, anschließend die Sperre ohne Zugangsdaten und dann beide Anmeldungen im Browser. Nach Proxy-Änderungen erneut testen. Manche Verwaltungspanels erzeugen Regeln beim Speichern neu. Die öffentliche API muss weiter auf `https://api.example.com/healthz` antworten. Für den Rückweg `--prepare --admin-local` verwenden, den Container mit Compose neu erstellen und die externe Admin-Weiterleitung deaktivieren.

## 6. Erster API-Aufruf, Diagnose und Betrieb

Lege in der Verwaltung eine Verbindung zu einem kontrollierten JSON-Testdienst an, dann eine feste GET-Aktion. Aktiviere die Aktion und erteile einem persönlichen Testzugang desselben Kunden die Freigabe. Der Client meldet sich mit `POST /auth/login` an und sendet den zurückgegebenen `access_token` als Bearer-Token an `/actions/<aktionsname>`. Eine vollständige Testsequenz mit Dummy-Zugängen gibt es im [Demo-Client](../scripts/demo-client.py). Für eine reale Ninox-Integration den Aufruf zunächst in einem getrennten Testkontext erproben.

| Beobachtung | Prüfung |
|---|---|
| API-Startadresse `/` liefert 404 | `/healthz` prüfen. Die API-Startadresse hat keine Webseite. |
| `401` am Admin-Host vor dem Login | Erwartete Sperre des vorgeschalteten Proxys bei Basic Auth. |
| `403` bei der Schleuse-Verwaltung | Browser-Adresse, `GATEWAY_ADMIN_ORIGIN`, weitergeleiteten `Host` und `Origin` vergleichen. |
| `502` | Healthcheck und Proxy-Ziel prüfen. Bei einer Aktion zusätzlich Statusquelle und Zielverbindung prüfen. |
| Zertifikat passt nicht zum Namen | DNS, gewählten Proxy-Host und dessen Zertifikatszuordnung prüfen. |

Für Logs `docker compose --env-file install.env --project-name schleuse-pilot logs --tail 100 gateway` nutzen. Vor einem Update ein Backup anlegen. Gateway für eine konsistente SQLite-Sicherung stoppen und die Datenbank samt eventuell vorhandenen Begleitdateien aus `/data` sichern. Den passenden `master.key` getrennt geschützt aufbewahren. Das benannte Volume gehört zum Compose-Projekt. Für einen Wiederherstellungstest ein separates Volume verwenden und Lese-/Schreibrechte für den Laufzeitbenutzer des Images prüfen. Das ZIP ist das Quellpaket. Das Betriebsbackup bleibt ein separates, geschütztes Paket. [Docker Container-Dateien kopieren](https://docs.docker.com/reference/cli/docker/container/cp/)
