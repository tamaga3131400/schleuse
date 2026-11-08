#!/usr/bin/env python3
"""Lokaler Compose-Installer. Standardmäßig nur Vorschau; keine externen Portscans."""
import argparse
import errno
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
KEYS = {"COMPOSE_PROJECT_NAME", "GATEWAY_PUBLIC_URL", "GATEWAY_API_PORT",
        "GATEWAY_ADMIN_PORT", "GATEWAY_ADMIN_ORIGIN"}
INSPECT = ('{"id":{{json .Id}},"name":{{json .Name}},'
           '"running":{{json .State.Running}},'
           '"ports":{{json .HostConfig.PortBindings}},'
           '"active_ports":{{json .NetworkSettings.Ports}},'
           '"project":{{json (index .Config.Labels "com.docker.compose.project")}},'
           '"workdir":{{json (index .Config.Labels "com.docker.compose.project.working_dir")}}}')


class InstallError(Exception):
    pass


def run(command, *, env=None, timeout=30):
    try:
        result = subprocess.run(command, text=True, capture_output=True,
                                env=env, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise InstallError(f"Befehl nicht ausführbar: {command[0]} ({exc})") from exc
    if result.returncode:
        detail = (result.stderr or result.stdout).strip()[:600]
        raise InstallError(f"{' '.join(command[:3])} fehlgeschlagen: {detail}")
    return result.stdout.strip()


def port_number(value):
    try:
        port = int(value)
    except (TypeError, ValueError) as exc:
        raise InstallError(f"Ungültiger Port: {value}") from exc
    if not 1024 <= port <= 65535:
        raise InstallError("Host-Ports müssen zwischen 1024 und 65535 liegen.")
    return port


def port_range(value):
    try:
        start, end = value.split("-")
        start, end = port_number(start), port_number(end)
    except (ValueError, InstallError) as exc:
        raise InstallError("Portbereich als START-ENDE angeben, z.B. 18080-19999.") from exc
    if start > end:
        raise InstallError("Der Anfang des Portbereichs liegt hinter dem Ende.")
    return range(start, end + 1)


def domain_name(value):
    try:
        value = value.encode("idna").decode("ascii").lower()
    except UnicodeError as exc:
        raise InstallError("Ungültige Domain.") from exc
    if len(value) > 253 or "." not in value or any(
            not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label)
            for label in value.split(".")):
        raise InstallError("Nur einen Domainnamen ohne Protokoll, Port oder Pfad angeben.")
    return value


def read_config(config):
    if config.is_symlink():
        raise InstallError("install.env darf kein symbolischer Link sein.")
    if not config.exists():
        return {}
    values = {}
    for line in config.read_text().splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        key, sep, value = line.partition("=")
        if not sep or key not in KEYS or key in values:
            raise InstallError("install.env enthält unbekannte oder doppelte Einträge.")
        values[key] = value
    if set(values) != KEYS:
        raise InstallError("install.env ist unvollständig; vorhandene Datei prüfen.")
    return values


def local_docker():
    if not shutil.which("docker"):
        raise InstallError("Docker fehlt. Docker über die vorhandene aaPanel-Installation bereitstellen.")
    if os.environ.get("DOCKER_HOST") and not os.environ.get("DOCKER_CONTEXT"):
        endpoint = os.environ["DOCKER_HOST"]
    else:
        endpoint = json.loads(run(["docker", "context", "inspect", "--format",
                                   "{{json .Endpoints.docker.Host}}"]))
    if not isinstance(endpoint, str) or not endpoint.startswith("unix://"):
        raise InstallError("Lokaler Docker-Unix-Socket erforderlich. Remote-Kontexte werden nicht installiert: Host-Portprüfung muss auf dem Docker-Server laufen.")
    if run(["docker", "info", "--format", "{{.OSType}}"]) != "linux":
        raise InstallError("Dieser Installer benötigt Linux-Container.")
    run(["docker", "compose", "version"])


def containers():
    ids = run(["docker", "container", "ls", "--all", "--quiet"]).split()
    rows = []
    for offset in range(0, len(ids), 100):
        output = run(["docker", "container", "inspect", "--format", INSPECT,
                      *ids[offset:offset + 100]])
        rows.extend(json.loads(line) for line in output.splitlines())
    return rows


def docker_ports(rows, project, root):
    reserved, own_running = {}, set()
    for row in rows:
        own = row.get("project") == project
        if own and Path(row.get("workdir") or "/").resolve() != root.resolve():
            raise InstallError(f"Compose-Projekt {project} gehört zu einem anderen Verzeichnis. Anderen --project-Namen verwenden; keine Übernahme fremder Container.")
        for mapping in (row.get("ports"), row.get("active_ports")):
            for bindings in (mapping or {}).values():
                for binding in bindings or []:
                    raw = binding.get("HostPort", "")
                    if not raw.isdigit() or not 0 < int(raw) <= 65535:
                        continue
                    port = int(raw)
                    if own:
                        if row.get("running"):
                            own_running.add(port)
                    else:
                        state = "läuft" if row.get("running") else "gestoppt, reserviert"
                        reserved.setdefault(port, set()).add(
                            f"Docker {row.get('name', '').lstrip('/')} ({state})")
    return reserved, own_running


def host_ports():
    """Inventar ohne Prozess-/Credential-Ausgabe; Bind-Proben bleiben die zweite Prüfung."""
    used = set()
    if shutil.which("ss"):
        for flag in ("-lnt", "-lnu"):
            output = run(["ss", "-H", flag])
            for line in output.splitlines():
                parts = line.split()
                if len(parts) >= 4:
                    match = re.search(r":(\d+)$", parts[3])
                    if match:
                        used.add(int(match.group(1)))
    elif shutil.which("lsof"):
        for args in (["-iTCP", "-sTCP:LISTEN"], ["-iUDP"]):
            result = subprocess.run(["lsof", "-nP", *args, "-Fn"],
                                    capture_output=True, text=True, check=False)
            if result.returncode not in (0, 1):
                raise InstallError("Host-Portinventar mit lsof fehlgeschlagen.")
            for line in result.stdout.splitlines():
                if line.startswith("n"):
                    match = re.search(r":(\d+)$", line.split("->", 1)[0])
                    if match:
                        used.add(int(match.group(1)))
    return used


def bind_available(port):
    """Kein Connect-Scan: lokale TCP/UDP-Bind-Proben für IPv4 und IPv6, ohne Listen."""
    for family, address in ((socket.AF_INET, "0.0.0.0"), (socket.AF_INET6, "::")):
        for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
            try:
                with socket.socket(family, kind) as sock:
                    if family == socket.AF_INET6:
                        sock.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
                    sock.bind((address, port))
            except OSError as exc:
                if family == socket.AF_INET6 and exc.errno in (
                        errno.EAFNOSUPPORT, errno.EPROTONOSUPPORT, errno.EADDRNOTAVAIL):
                    continue
                return False
    return True


def choose_ports(candidates, fixed, reserved, listeners, own_running, excluded):
    selected = []

    def available(port, reuse=False):
        if port in reserved or port in excluded or port in selected:
            return False
        if reuse and port in own_running:
            return True
        return port not in listeners and bind_available(port)

    # Feste/gespeicherte Werte zuerst reservieren, damit Autowahl sie nicht verbraucht.
    for raw in fixed:
        if raw is not None:
            port = port_number(raw)
            if not available(port, reuse=True):
                raise InstallError(f"Host-Port {port} ist belegt, reserviert oder nicht bindbar. Freien Port ausdrücklich wählen; gespeicherte Ports werden nicht still geändert.")
            selected.append(port)
    result = []
    for raw, preferred in zip(fixed, (18080, 19090)):
        if raw is not None:
            result.append(port_number(raw))
            continue
        order = ([preferred] if preferred in candidates else [])
        port = next((p for p in (*order, *candidates) if available(p)), None)
        if port is None:
            raise InstallError("Nicht genügend freie Ports im gewählten Bereich. Größeren --range angeben.")
        selected.append(port)
        result.append(port)
    return result


def save_config(config, values):
    fd, temporary = tempfile.mkstemp(prefix=".install-env-", dir=config.parent)
    try:
        with os.fdopen(fd, "w") as handle:
            handle.write("# Vom Schleuse-Installer verwaltet; enthält keine Anbieter-Schlüssel.\n")
            for key in sorted(values):
                handle.write(f"{key}={values[key]}\n")
        os.replace(temporary, config)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main(argv=None, root=ROOT):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--plan", action="store_true", help="nur prüfen und anzeigen (Standard)")
    mode.add_argument("--prepare", action="store_true", help="prüfen und install.env schreiben, nicht starten")
    mode.add_argument("--install", action="store_true", help="prüfen, bauen und nur Gateway-Stack starten")
    parser.add_argument("--range", default="18080-19999", help="Bereich für neue Host-Ports")
    parser.add_argument("--api-port", help="API-Host-Port ausdrücklich festlegen")
    parser.add_argument("--admin-port", help="Admin-Host-Port ausdrücklich festlegen")
    parser.add_argument("--exclude", default="", help="zusätzlich freihalten, z.B. 3500,3511,3521")
    parser.add_argument("--show-used", action="store_true", help="alle erfassten belegten/reservierten Portnummern anzeigen")
    parser.add_argument("--domain", help="optionale öffentliche API-Domain ohne https://")
    admin_access = parser.add_mutually_exclusive_group()
    admin_access.add_argument("--admin-domain", help="separate HTTPS-Domain für die Verwaltung ohne https://")
    admin_access.add_argument("--admin-local", action="store_true", help="Verwaltungsadresse auf den lokalen SSH-Tunnel zurücksetzen")
    parser.add_argument("--project", help="eigener Compose-Projektname")
    args = parser.parse_args(argv)
    if sys.version_info < (3, 9):
        raise InstallError("Python 3.9 oder neuer erforderlich.")
    root = Path(root).resolve()
    config = root / "install.env"
    values = read_config(config)
    candidates = port_range(args.range)
    excluded = {port_number(v.strip()) for v in args.exclude.split(",") if v.strip()}
    project = args.project or values.get("COMPOSE_PROJECT_NAME", "schleuse-pilot")
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,62}", project):
        raise InstallError("Projektname: 1–63 kleine Buchstaben, Ziffern, Bindestriche oder Unterstriche.")
    if values and project != values["COMPOSE_PROJECT_NAME"]:
        raise InstallError("Gespeicherten Projektnamen nicht ändern: Datenvolume ist daran gebunden. Für eine zweite Instanz eigenen Projektordner verwenden.")
    saved_public_url = values.get("GATEWAY_PUBLIC_URL", "")
    api_domain = args.domain
    if api_domain is None and saved_public_url.startswith("https://"):
        api_domain = saved_public_url.removeprefix("https://")
    if api_domain is None and saved_public_url and not re.fullmatch(r"http://127\.0\.0\.1:[0-9]+", saved_public_url):
        raise InstallError("Gespeicherte API-Adresse muss HTTPS oder lokales HTTP auf 127.0.0.1 verwenden.")
    domain = domain_name(api_domain) if api_domain is not None else None
    admin_domain = args.admin_domain
    saved_admin_origin = values.get("GATEWAY_ADMIN_ORIGIN", "")
    if admin_domain is None and not args.admin_local and saved_admin_origin.startswith("https://"):
        admin_domain = saved_admin_origin.removeprefix("https://")
    if admin_domain is not None:
        admin_domain = domain_name(admin_domain)
        if admin_domain == domain:
            raise InstallError("Verwaltung und Client-API brauchen getrennte Domains.")
    local_docker()
    rows = containers()
    reserved, own_running = docker_ports(rows, project, root)
    if not values:
        existing = any(row.get("project") == project for row in rows)
        volumes = run(["docker", "volume", "ls", "--quiet", "--filter",
                       f"label=com.docker.compose.project={project}"])
        if existing or volumes:
            raise InstallError("Gleichnamiger Stack oder Datenvolume vorhanden, aber install.env fehlt. Bestehende Installation prüfen oder anderen --project-Namen wählen.")
    listeners = host_ports()
    fixed = (args.api_port or values.get("GATEWAY_API_PORT"),
             args.admin_port or values.get("GATEWAY_ADMIN_PORT"))
    api, admin = choose_ports(candidates, fixed, reserved, listeners, own_running, excluded)
    public_url = f"https://{domain}" if domain else f"http://127.0.0.1:{api}"
    admin_origin = f"https://{admin_domain}" if admin_domain else f"http://127.0.0.1:{admin}"
    new_values = {"COMPOSE_PROJECT_NAME": project, "GATEWAY_PUBLIC_URL": public_url,
                  "GATEWAY_API_PORT": str(api), "GATEWAY_ADMIN_PORT": str(admin),
                  "GATEWAY_ADMIN_ORIGIN": admin_origin}
    # Shell-Variablen dürfen die geprüften Ports nicht bei Compose übersteuern.
    env = dict(os.environ, **new_values)
    env.pop("COMPOSE_PROFILES", None)
    compose = ["docker", "compose", "--project-directory", str(root),
               "--file", str(root / "compose.yaml"), "--project-name", project,
               "--env-file", os.devnull]
    run([*compose, "config", "--quiet"], env=env)
    print(f"Lokaler Portcheck: {len(listeners)} Host-Portnummern und {len(reserved)} fremde Docker-Portnummern erfasst.")
    print("Auch gestoppte Container mit konfigurierten Host-Ports werden berücksichtigt.")
    interesting = sorted((set(reserved) | listeners | excluded) if args.show_used
                         else (p for p in reserved if p in candidates))
    shown = interesting if args.show_used else interesting[:25]
    for port in shown:
        reasons = set(reserved.get(port, set()))
        if port in listeners:
            reasons.add("Host-Listener TCP/UDP")
        if port in excluded:
            reasons.add("ausdrücklich ausgeschlossen")
        print(f"  {port}: {', '.join(sorted(reasons))}")
    if not args.show_used and len(interesting) > 25:
        print(f"  … weitere {len(interesting) - 25} reservierte Ports im Bereich")
    print(f"\nProjekt: {project}\nAPI: 127.0.0.1:{api} → Container 8080")
    print(f"Admin: 127.0.0.1:{admin} → Container 9090")
    print(f"API-Adresse: {public_url}\nReverse-Proxy-Ziel für API: http://127.0.0.1:{api}")
    if admin_domain:
        print(f"Admin-Domain: {admin_origin}\nReverse-Proxy-Ziel für Admin: http://127.0.0.1:{admin}")
        print("Für diese Admin-Domain sind ein eigener HTTPS-Proxy und eine Zugangsbeschränkung erforderlich.")
    else:
        print(f"Admin über SSH-Tunnel: {admin_origin}")
    if not (args.prepare or args.install):
        print("\nNur Vorschau. Keine Datei geschrieben, kein Container gestartet. --prepare speichert, --install baut und startet.")
        return 0
    lock_path = root / ".install.lock"
    if lock_path.is_symlink():
        raise InstallError("Installationslock darf kein symbolischer Link sein.")
    with lock_path.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise InstallError("Ein anderer Installer arbeitet bereits in diesem Verzeichnis.") from exc
        if read_config(config) != values:
            raise InstallError("install.env wurde zwischenzeitlich geändert; Prüfung erneut ausführen.")
        if args.install:
            help_text = run([*compose, "up", "--help"], env=env)
            if "--wait" not in help_text or "--wait-timeout" not in help_text:
                raise InstallError("Compose V2 mit up --wait und --wait-timeout erforderlich.")
        save_config(config, new_values)
        print(f"\nGespeichert: {config}")
        if not args.install:
            print("Keine Container gestartet. Die Ports sind noch nicht reserviert; beim Installieren werden sie erneut geprüft.")
            return 0
        print("Baue ausschließlich das Gateway-Image …", flush=True)
        build = subprocess.run([*compose, "build", "gateway"], env=env, check=False)
        if build.returncode:
            raise InstallError("Build fehlgeschlagen; install.env bleibt erhalten. Keine fremden Container wurden verändert.")
        # Ein Build kann dauern: fremde Neubelegungen vor dem Start erneut prüfen.
        latest_reserved, latest_own = docker_ports(containers(), project, root)
        choose_ports(candidates, (str(api), str(admin)), latest_reserved,
                     host_ports(), latest_own, excluded)
        print("Starte ausschließlich diesen Gateway-Stack …", flush=True)
        started = subprocess.run([*compose, "up", "--no-build", "--detach", "--wait",
                                  "--wait-timeout", "90", "gateway"], env=env, check=False)
        if started.returncode:
            raise InstallError("Start oder Healthcheck fehlgeschlagen. Datenvolume und Konfiguration bleiben erhalten. Nur die Logs dieses Compose-Projekts prüfen.")
        print("Gateway bereit. DNS, Zertifikat und Reverse Proxy werden separat eingerichtet.")
        print("Einrichtungscode privat lesen: docker compose --env-file install.env "
              f"--project-name {project} cp gateway:/data/setup-code ./setup-code")
        print("Nur beim Erststart vorhanden; lokal geschützt behandeln und danach entfernen.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (InstallError, OSError, ValueError) as exc:
        print(f"Abbruch: {exc}", file=sys.stderr)
        sys.exit(1)
