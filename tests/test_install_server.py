import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("installer", Path(__file__).resolve().parents[1] / "scripts/install_server.py")
installer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(installer)


def container(name, ports, running=True, project=None, workdir=None):
    return {"name": name, "running": running, "project": project, "workdir": workdir,
            "ports": {"8080/tcp": [{"HostPort": str(p)} for p in ports]}}


class InstallerTests(unittest.TestCase):
    def test_published_ports_including_stopped_and_dynamic_bindings(self):
        rows = [container("/ninox", [3500, 3511]), container("/paused", [3521], False),
                {"name": "/dynamic", "running": True,
                 "active_ports": {"9090/tcp": [{"HostPort": "3555"}]}}]
        reserved, own = installer.docker_ports(rows, "schleuse-pilot", Path("/work"))
        self.assertEqual(set(reserved), {3500, 3511, 3521, 3555})
        self.assertIn("gestoppt", next(iter(reserved[3521])))
        self.assertFalse(own)

    def test_automatic_selection_skips_host_docker_and_reserved_ports(self):
        with patch.object(installer, "bind_available", side_effect=lambda p: p != 3502):
            self.assertEqual(installer.choose_ports(range(3500, 3507), (None, None),
                                                   {3500: {"Docker"}}, {3501}, set(), {3503}),
                             [3504, 3505])

    def test_explicit_admin_port_is_not_consumed_by_auto_api(self):
        with patch.object(installer, "bind_available", return_value=True):
            self.assertEqual(installer.choose_ports(range(3500, 3503), (None, "3500"),
                                                   {}, set(), set(), set()), [3501, 3500])

    def test_saved_ports_fail_instead_of_silently_moving(self):
        with patch.object(installer, "bind_available", return_value=True):
            with self.assertRaises(installer.InstallError):
                installer.choose_ports(range(3500, 3510), ("3500", "3501"),
                                       {3500: {"anderer Container"}}, set(), set(), set())
            with self.assertRaises(installer.InstallError):
                installer.choose_ports(range(3500, 3510), ("3500", "3500"),
                                       {}, set(), set(), set())

    def test_own_running_ports_reused_but_foreign_project_not_adopted(self):
        row = container("/own", [3500, 3501], project="pilot", workdir="/work")
        reserved, own = installer.docker_ports([row], "pilot", Path("/work"))
        with patch.object(installer, "bind_available", return_value=False):
            self.assertEqual(installer.choose_ports(range(3500, 3502), ("3500", "3501"),
                                                   reserved, own, own, set()), [3500, 3501])
        with self.assertRaises(installer.InstallError):
            installer.docker_ports([row], "pilot", Path("/another"))

    def test_exhausted_or_invalid_range(self):
        with patch.object(installer, "bind_available", return_value=False):
            with self.assertRaises(installer.InstallError):
                installer.choose_ports(range(3500, 3502), (None, None), {}, set(), set(), set())
        for value in ("443-450", "3500-3400", "invalid", "65535-65536"):
            with self.assertRaises(installer.InstallError):
                installer.port_range(value)

    def test_real_tcp_and_udp_listeners_block_candidate(self):
        for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
            with socket.socket(socket.AF_INET, kind) as sock:
                sock.bind(("127.0.0.1", 0))
                if kind == socket.SOCK_STREAM:
                    sock.listen()
                self.assertFalse(installer.bind_available(sock.getsockname()[1]))

    def test_ipv6_listener_blocks_candidate(self):
        try:
            sock = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
            sock.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
            sock.bind(("::1", 0))
        except OSError:
            self.skipTest("IPv6 lokal nicht verfügbar")
        with sock:
            sock.listen()
            self.assertFalse(installer.bind_available(sock.getsockname()[1]))

    def test_ss_parser_covers_ipv4_ipv6_tcp_udp(self):
        output = "LISTEN 0 128 127.0.0.1:3500 0.0.0.0:*\nLISTEN 0 128 [::]:3511 [::]:*\n"
        with patch.object(installer.shutil, "which", return_value="/fake/ss"), \
             patch.object(installer, "run", side_effect=[output, "UNCONN 0 0 *:3521 *:*\n"]):
            self.assertEqual(installer.host_ports(), {3500, 3511, 3521})

    def test_remote_docker_rejected_before_server_connection(self):
        with patch.dict(os.environ, {"DOCKER_HOST": "ssh://server", "DOCKER_CONTEXT": ""}), \
             patch.object(installer.shutil, "which", return_value="/fake/docker"), \
             patch.object(installer, "run") as run:
            with self.assertRaises(installer.InstallError):
                installer.local_docker()
            run.assert_not_called()

    def test_env_files_are_not_executed_and_unknown_keys_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "install.env"
            config.write_text("GATEWAY_API_PORT=$(touch should-not-exist)\n")
            with self.assertRaises(installer.InstallError):
                installer.read_config(config)
            config.unlink()
            config.symlink_to(root / "elsewhere")
            with self.assertRaises(installer.InstallError):
                installer.read_config(config)
            self.assertFalse((root / "should-not-exist").exists())

    def exercise(self, root, argv, rows=None, bind=None, process=None):
        commands = []

        def fake(command, **kwargs):
            commands.append((command, kwargs.get("env")))
            if command[:3] == ["docker", "context", "inspect"]:
                return json.dumps("unix:///var/run/docker.sock")
            if command[:2] == ["docker", "info"]:
                return "linux"
            if command[:3] == ["docker", "container", "ls"]:
                return "\n".join(str(i) for i in range(len(rows or [])))
            if command[:3] == ["docker", "container", "inspect"]:
                return "\n".join(json.dumps(row) for row in rows)
            if command[-2:] == ["up", "--help"]:
                return "--wait --wait-timeout"
            return ""

        with patch.dict(os.environ, {"DOCKER_HOST": "", "DOCKER_CONTEXT": "",
                                     "GATEWAY_API_PORT": "9999", "COMPOSE_FILE": "other.yaml"}), \
             patch.object(installer.shutil, "which", return_value="/fake/command"), \
             patch.object(installer, "run", side_effect=fake), \
             patch.object(installer, "host_ports", return_value={3500}), \
             patch.object(installer, "bind_available", side_effect=bind or (lambda p: True)), \
             patch.object(installer.subprocess, "run", side_effect=process) as processes, \
             contextlib.redirect_stdout(io.StringIO()) as output:
            result = installer.main(argv, root=root)
        return result, output.getvalue(), commands, processes

    def test_plan_is_read_only_and_environment_cannot_change_ports(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result, output, commands, processes = self.exercise(root, ["--range", "3500-3510"])
            self.assertEqual(result, 0)
            self.assertEqual(list(root.iterdir()), [])
            self.assertIn("127.0.0.1:3501", output)
            config_env = commands[-1][1]
            self.assertEqual(config_env["GATEWAY_API_PORT"], "3501")
            self.assertEqual(config_env["GATEWAY_ADMIN_PORT"], "3502")
            self.assertEqual(config_env["GATEWAY_PUBLIC_URL"], "http://127.0.0.1:3501")
            self.assertIn(str(root.resolve() / "compose.yaml"), commands[-1][0])
            processes.assert_not_called()

    def test_prepare_is_stable_and_private_and_rerun_has_no_start(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.exercise(root, ["--prepare", "--range", "3500-3510"])
            config = root / "install.env"
            initial = config.read_text()
            self.assertEqual(config.stat().st_mode & 0o777, 0o600)
            self.exercise(root, ["--prepare", "--range", "4500-4510"])
            self.assertEqual(initial, config.read_text())

    def test_admin_domain_persists_until_explicit_tunnel_reset(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "install.env"
            self.exercise(root, ["--prepare", "--range", "3500-3510",
                                 "--domain", "api.example.com", "--admin-domain", "admin.example.com"])
            initial = installer.read_config(config)
            self.assertEqual(initial["GATEWAY_ADMIN_ORIGIN"], "https://admin.example.com")
            self.assertEqual(initial["GATEWAY_PUBLIC_URL"], "https://api.example.com")
            _, _, commands, _ = self.exercise(root, ["--plan"])
            self.assertEqual(commands[-1][1]["GATEWAY_ADMIN_ORIGIN"], initial["GATEWAY_ADMIN_ORIGIN"])
            self.assertEqual(commands[-1][1]["GATEWAY_PUBLIC_URL"], initial["GATEWAY_PUBLIC_URL"])
            self.exercise(root, ["--prepare", "--admin-local"])
            self.assertEqual(installer.read_config(config)["GATEWAY_ADMIN_ORIGIN"], "http://127.0.0.1:3502")

    def test_admin_domain_cannot_match_public_api(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(installer.InstallError, "getrennte Domains"):
                self.exercise(Path(temporary), ["--prepare", "--domain", "api.example.com", "--admin-domain", "API.EXAMPLE.COM"])

    def test_install_starts_only_project_gateway_with_wait(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            def process(command, **kwargs):
                self.assertIn(str(root.resolve() / "compose.yaml"), command)
                self.assertEqual(command[-1], "gateway")
                return subprocess_result(0)
            _, output, _, processes = self.exercise(root, ["--install", "--range", "3500-3510"], process=process)
            self.assertEqual(processes.call_count, 2)
            self.assertIn("--wait", processes.call_args_list[-1].args[0])
            self.assertIn("Gateway bereit", output)

    def test_new_collision_after_build_aborts_before_start(self):
        with tempfile.TemporaryDirectory() as temporary:
            calls = 0
            started = []
            def bind(port):
                nonlocal calls
                calls += 1
                return calls <= 2
            def process(command, **kwargs):
                started.append(command)
                return subprocess_result(0)
            with self.assertRaises(installer.InstallError):
                self.exercise(Path(temporary), ["--install", "--range", "3500-3510"],
                              bind=bind, process=process)
            self.assertEqual(len(started), 1)
            self.assertIn("build", started[0])

    def test_show_used_lists_ports_outside_selection_range(self):
        with tempfile.TemporaryDirectory() as temporary:
            _, output, _, _ = self.exercise(Path(temporary), ["--show-used"],
                                             rows=[container("/ninox", [3511, 3521])])
            self.assertIn("3511: Docker ninox", output)
            self.assertIn("3521: Docker ninox", output)

    def test_failed_build_keeps_config_without_starting(self):
        with tempfile.TemporaryDirectory() as temporary:
            calls = []
            def failed(command, **kwargs):
                calls.append(command)
                return subprocess_result(1)
            with self.assertRaises(installer.InstallError):
                self.exercise(Path(temporary), ["--install"], process=failed)
            self.assertTrue((Path(temporary) / "install.env").exists())
            self.assertEqual(len(calls), 1)

    def test_domain_rejects_shell_input_and_url_paths(self):
        for domain in ("https://example.com", "example.com/path", "x.$(touch bad)", "x.com\nBAD=1"):
            with self.assertRaises(installer.InstallError):
                installer.domain_name(domain)


def subprocess_result(code):
    class Result:
        returncode = code
    return Result()


if __name__ == "__main__":
    unittest.main()
