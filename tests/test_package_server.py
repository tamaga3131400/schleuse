import contextlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from zipfile import ZipFile

SPEC = importlib.util.spec_from_file_location("packager", Path(__file__).resolve().parents[1] / "scripts/package_server.py")
packager = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(packager)


class PackageTests(unittest.TestCase):
    def test_runtime_data_keys_and_unlisted_files_are_excluded(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in [*packager.PUBLIC_FILES, "docs/README.md", "scripts/private_notes.py",
                             ".data/gateway.db", ".data/master.key",
                             "install.env", "scripts/private.key", "scripts/secret.env",
                             "scripts/private.json", "web/node_modules/package/index.js", "bin/gateway"]:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("test")
            (root / packager.PRIVATE_MARKERS_FILE).write_text("private-fixture.invalid\n")
            with contextlib.redirect_stdout(io.StringIO()):
                target = packager.package(root)
            with tarfile.open(target) as archive:
                names = set(archive.getnames())
            self.assertIn("schleuse-server/install-server.sh", names)
            self.assertIn("schleuse-server/scripts/install_server.py", names)
            self.assertIn("schleuse-server/internal/ui/ui.go", names)
            self.assertFalse(any(".data/" in n or "node_modules" in n or n.endswith((".key", ".env", ".db", "/gateway", "private.json")) for n in names))
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)

            (root / "install-server.sh").chmod(0o755)
            with contextlib.redirect_stdout(io.StringIO()):
                zip_target = packager.package(root, archive_format="zip")
            with ZipFile(zip_target) as archive:
                self.assertEqual(set(archive.namelist()), {"schleuse-server/" + name for name in packager.PUBLIC_FILES})
                self.assertEqual((archive.getinfo("schleuse-server/install-server.sh").external_attr >> 16) & 0o777,
                                 0o755)
                self.assertIsNone(archive.testzip())

    def test_public_archive_rejects_private_content_without_echoing_it(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in packager.PUBLIC_FILES:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("test")
            private_domain = "private-example.invalid"
            (root / packager.PRIVATE_MARKERS_FILE).write_text(private_domain + "\n")
            page = root / "site/index.html"
            for leak in (private_domain, "ghp_" + "a" * 36):
                with self.subTest(kind="domain" if leak == private_domain else "token"):
                    page.write_text(leak)
                    with self.assertRaises(RuntimeError) as error:
                        packager.package(root, archive_format="zip")
                    self.assertIn("site/index.html", str(error.exception))
                    self.assertNotIn(leak, str(error.exception))
                    self.assertFalse((root / "dist/schleuse-server.zip").exists())

    def test_symlink_source_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in [*packager.FILES, "internal/ui/ui.go"]:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("test")
            (root / "go.mod").unlink()
            (root / "go.mod").symlink_to(root / "go.sum")
            with self.assertRaises(RuntimeError):
                packager.package(root)


if __name__ == "__main__":
    unittest.main()
