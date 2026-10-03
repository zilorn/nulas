import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import tray_dependencies as deps


class LinuxDependencyTests(unittest.TestCase):
    def test_headless_never_installs(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(deps, "run") as install:
            with self.assertRaisesRegex(RuntimeError, "桌面会话"):
                deps.prepare_linux()
            install.assert_not_called()

    def test_existing_dependencies_never_install(self):
        with patch.dict(os.environ, {"DISPLAY": ":0"}), patch.object(deps, "available", return_value=True), patch.object(deps, "run") as install:
            deps.prepare_linux()
            install.assert_not_called()

    def test_package_managers_and_privilege(self):
        for manager in ("apt-get", "dnf", "pacman"):
            with self.subTest(manager=manager), patch.object(deps.os, "geteuid", return_value=1000), patch.object(deps.shutil, "which", side_effect=lambda name: "/usr/bin/" + name if name in (manager, "pkexec") else None):
                command = deps.package_command()
                self.assertEqual(command[:2], ["/usr/bin/pkexec", "/usr/bin/" + manager])
        with patch.object(deps.os, "geteuid", return_value=1000), patch.object(deps.shutil, "which", side_effect=lambda name: "/usr/bin/" + name if name in ("apt-get", "sudo") else None):
            self.assertEqual(deps.package_command()[:2], ["/usr/bin/sudo", "-n"])
        with patch.object(deps.shutil, "which", return_value=None):
            with self.assertRaisesRegex(RuntimeError, "发行版"):
                deps.package_command()

    def test_install_isolated_environment_and_reexec(self):
        with tempfile.TemporaryDirectory() as cache, patch.dict(os.environ, {"DISPLAY": ":0", "XDG_CACHE_HOME": cache}), patch.object(deps, "available", side_effect=[True, False, True, True, True, False, True, True]), patch.object(deps, "run") as install, patch.object(deps.os, "execv") as reexec:
            deps.prepare_linux()
            commands = [call.args[0] for call in install.call_args_list]
            self.assertEqual(commands[0][1:4], ["-m", "venv", "--system-site-packages"])
            self.assertIn(Path(cache), commands[0][-1].parents)
            self.assertEqual(commands[1][1:4], ["-m", "pip", "install"])
            reexec.assert_called_once()

    def test_incompatible_python_reports_failure(self):
        with patch.dict(os.environ, {"DISPLAY": ":0"}), patch.object(deps, "available", return_value=False), patch.object(deps, "package_command", return_value=["fake-install"]), patch.object(deps, "run") as install:
            with self.assertRaisesRegex(RuntimeError, "NULAS_PYTHON"):
                deps.prepare_linux()
            install.assert_called_once_with(["fake-install"])

    def test_cancel_installer_when_parent_closes_pipe(self):
        import subprocess
        import sys
        with tempfile.TemporaryFile() as output:
            parent = subprocess.Popen([sys.executable, "-c", "from tray_dependencies import run; import sys; run([sys.executable, '-c', 'import time; time.sleep(30)'])"], cwd=Path(deps.__file__).parent, stdin=subprocess.PIPE, stdout=output, stderr=output)
            parent.stdin.close()
            self.assertNotEqual(parent.wait(timeout=5), 0)
            output.seek(0)
            self.assertIn("依赖安装已中断", output.read().decode())

    def test_cached_environment_never_installs(self):
        with tempfile.TemporaryDirectory() as cache:
            target = Path(cache) / "nulas" / ("tray-python%d.%d" % deps.sys.version_info[:2]) / "bin" / "python"
            target.parent.mkdir(parents=True)
            target.touch()
            with patch.dict(os.environ, {"DISPLAY": ":0", "XDG_CACHE_HOME": cache}), patch.object(deps, "available", side_effect=[True, False, True, True]), patch.object(deps, "run") as install, patch.object(deps.os, "execv") as reexec:
                deps.prepare_linux()
                install.assert_not_called()
                self.assertEqual(reexec.call_args.args[0], str(target))
