#!/usr/bin/env python3
"""Quellpakete für Installation und Weitergabe erstellen."""
import argparse
import hashlib
from pathlib import Path
import re
import stat
import tarfile
import tempfile
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo

ROOT = Path(__file__).resolve().parents[1]
FILES = ["Dockerfile", "compose.yaml", "go.mod", "go.sum", ".dockerignore",
         ".gitignore", "README.md", "LICENSE", "install-server.sh", "package-server.sh",
         "start-local.sh", "Demo-starten.command", "test.sh", "web/package.json", "web/package-lock.json",
         "web/tsconfig.json", "web/vite.config.ts", "web/index.html"]
DIRS = ["cmd", "internal/app", "web/src", "scripts", "tests", "compose"]
SHARED_DOCS = ["docs/INSTALLATION.md", "docs/ARCHITEKTUR.md", "docs/VALIDIERUNG.md",
               "docs/VEROEFFENTLICHUNG.md", "docs/NINOX.md", "docs/KONNEKTOREN.md"]
PUBLIC_FILES = [
    *FILES, *SHARED_DOCS,
    "cmd/gateway/main.go", "compose/Caddyfile", "compose/caddy.yaml",
    "internal/app/app.go", "internal/app/app_test.go", "internal/app/demo.go",
    "internal/app/manage.go", "internal/app/proxy.go", "internal/app/store.go", "internal/ui/ui.go",
    "internal/app/connectors.go", "internal/app/connectors_test.go",
    *sorted(p.relative_to(ROOT).as_posix() for p in (ROOT / "internal/app/connectors").glob("*.json")),
    *sorted(p.relative_to(ROOT).as_posix() for p in (ROOT / "site").rglob("*") if p.is_file() and p.name != "index.html" and p.name != ".DS_Store"),
    "scripts/demo-client.py", "scripts/install_server.py", "scripts/package_server.py",
    "tests/test_install_server.py", "tests/test_package_server.py",
    "web/src/App.vue", "web/src/main.ts", "web/src/style.css",
    "site/index.html", "site/en/index.html",
]
BINARY_SUFFIXES = {".png", ".jpg", ".jpeg", ".webp"}
PRIVATE_MARKERS_FILE = ".release-private-markers"
PRIVATE_PATTERNS = re.compile(
    r"^-----BEGIN [A-Z ]*PRIVATE KEY-----\r?$"
    r"|\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|"
    r"(?:AKIA|ASIA)[A-Z0-9]{16}|xox[baprs]-[A-Za-z0-9-]{20,}|apsess_[A-Za-z0-9]{20,})\b"
    r"|(?:/Users/|/home/|/www/wwwroot/)[A-Za-z0-9_.-]+/"
    r"|Authorization:\s*Bearer\s+[A-Za-z0-9_.-]{28,}",
    re.MULTILINE | re.IGNORECASE,
)


def source_mode(mode):
    return 0o755 if mode & 0o111 else 0o644


def source_metadata(info):
    info.uid = info.gid = 0
    info.uname = info.gname = ""
    info.mode = source_mode(info.mode)
    return info


def package(root=ROOT, archive_format="tar.gz"):
    root = Path(root).resolve()
    if archive_format == "zip":
        members = [root / name for name in PUBLIC_FILES]
    else:
        members = [root / name for name in FILES]
        members.append(root / "internal/ui/ui.go")
        members.extend((root / "docs").glob("*.md"))
        members.extend((root / "internal/app/connectors").glob("*.json"))
        for name in DIRS:
            members.extend(p for p in (root / name).rglob("*")
                           if p.is_file() and "__pycache__" not in p.parts
                           and (p.suffix in {".go", ".py", ".sh", ".vue", ".ts", ".css", ".yaml"}
                                or p.name == "Caddyfile"))
    for member in members:
        if member.is_symlink() or any(p.is_symlink() for p in member.parents if p != root.parent):
            raise RuntimeError(f"Symbolischen Link nicht paketieren: {member}")
        if not member.is_file():
            raise RuntimeError(f"Quelldatei fehlt: {member.relative_to(root)}")
    archive_name = "schleuse-server.zip" if archive_format == "zip" else "schleuse-server-pilot.tar.gz"
    target = root / "dist" / archive_name
    checksum = target.with_name(target.name + ".sha256")
    if target.is_symlink() or checksum.is_symlink() or target.parent.is_symlink():
        raise RuntimeError("Paket-Ziel darf kein symbolischer Link sein.")
    target.parent.mkdir(exist_ok=True)
    entries = [(member, (Path("schleuse-server") / member.relative_to(root)).as_posix())
               for member in sorted(set(members))]
    markers = []
    marker_file = root / PRIVATE_MARKERS_FILE
    if archive_format == "zip" and marker_file.exists():
        markers = [line.strip().casefold() for line in marker_file.read_text(encoding="utf-8").splitlines()
                   if line.strip() and not line.lstrip().startswith("#")]
    with tempfile.TemporaryDirectory(dir=target.parent) as temporary:
        packed = Path(temporary) / target.name
        if archive_format == "zip":
            with ZipFile(packed, "w") as archive:
                for member, name in entries:
                    payload = member.read_bytes()
                    if member.suffix in BINARY_SUFFIXES:
                        info = ZipInfo.from_file(member, name)
                        info.external_attr = (stat.S_IFREG | 0o644) << 16
                        archive.writestr(info, payload, compress_type=ZIP_DEFLATED)
                        continue
                    content = payload.decode("utf-8")
                    folded = content.casefold()
                    if PRIVATE_PATTERNS.search(content) or any(marker in folded for marker in markers):
                        raise RuntimeError(f"Privater Inhalt im öffentlichen Paket: {member.relative_to(root)}")
                    info = ZipInfo.from_file(member, name)
                    info.external_attr = (stat.S_IFREG | source_mode(member.stat().st_mode)) << 16
                    archive.writestr(info, payload, compress_type=ZIP_DEFLATED)
        else:
            with tarfile.open(packed, "w:gz") as archive:
                for member, name in entries:
                    archive.add(member, arcname=name, recursive=False, filter=source_metadata)
        digest = hashlib.sha256(packed.read_bytes()).hexdigest()
        checked = Path(temporary) / checksum.name
        checked.write_text(f"{digest}  {target.name}\n")
        packed.chmod(0o600)
        packed.replace(target)
        checked.replace(checksum)
    print(f"Quellpaket: {target}\nSHA-256: {digest}")
    print("Kein gebautes Docker-Image. Der Serverbuild benötigt Internetzugang für Basisimages und Abhängigkeiten.")
    return target


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--format", choices=("tar.gz", "zip"), default="tar.gz")
    package(archive_format=parser.parse_args().format)
