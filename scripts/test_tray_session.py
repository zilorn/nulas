import io
import os
import socket
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
import tray_session as session


class SessionTests(unittest.TestCase):
    def test_delayed_desktop_refreshes_environment(self):
        output = io.StringIO()
        with patch.dict(os.environ, {}, clear=True), \
                patch.object(session, "session_environment", side_effect=[{}, {"DISPLAY": ":7"}]), \
                patch.object(session.select, "select", return_value=([], [], [])), \
                patch.object(session.sys, "stdout", output):
            session.wait_for_session(lambda: os.environ.get("DISPLAY") == ":7")
        self.assertEqual(output.getvalue().count("WAIT "), 1)

    def test_missing_tray_host_waits_and_parent_eof_cancels(self):
        with patch.object(session, "session_environment", return_value={}), \
                patch.object(session.select, "select", return_value=([session.sys.stdin], [], [])), \
                patch.object(session.os, "read", return_value=b""), \
                patch.object(session.sys, "stdout", io.StringIO()):
            with self.assertRaisesRegex(RuntimeError, "等待已取消"):
                session.wait_for_session(lambda: False)

    def test_wayland_socket_becomes_available(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ,
                {"XDG_RUNTIME_DIR": directory, "WAYLAND_DISPLAY": "wayland-7", "DISPLAY": ""}, clear=True):
            self.assertFalse(session.display_available())
            with socket.socket(socket.AF_UNIX) as host:
                host.bind(str(Path(directory) / "wayland-7"))
                host.listen()
                self.assertTrue(session.display_available())
            self.assertEqual(os.environ["DBUS_SESSION_BUS_ADDRESS"], "unix:path=" + directory + "/bus")

    def test_manager_environment_allowlist(self):
        import subprocess
        result = subprocess.CompletedProcess([], 0,
            "DISPLAY=:7\nXAUTHORITY=/tmp/auth\nMIHOMO_SECRET=secret\nNULAS_SECRET=secret\n")
        with patch.object(session.subprocess, "run", return_value=result):
            self.assertEqual(session.session_environment(), {"DISPLAY": ":7", "XAUTHORITY": "/tmp/auth"})

    def test_probe_failure_and_timeout_are_retryable(self):
        import subprocess
        for outcome in (subprocess.CompletedProcess([], 1), subprocess.TimeoutExpired([], 5)):
            with self.subTest(outcome=outcome):
                kwargs = {"side_effect": outcome} if isinstance(outcome, Exception) else {"return_value": outcome}
                with patch.object(session.subprocess, "run", **kwargs):
                    self.assertFalse(session.desktop_ready())

    def test_readiness_requires_gtk_and_registered_host(self):
        import types
        from unittest.mock import Mock
        for gtk_ready, host_ready, expected in ((False, True, 1), (True, False, 1), (True, True, 0)):
            bus = Mock()
            bus.call_sync.return_value.unpack.return_value = (host_ready,)
            gtk = types.SimpleNamespace(init_check=lambda: (gtk_ready, []))
            gio = types.SimpleNamespace(bus_get_sync=lambda *args: bus,
                BusType=types.SimpleNamespace(SESSION=0), DBusCallFlags=types.SimpleNamespace(NONE=0))
            glib = types.SimpleNamespace(Variant=Mock(), VariantType=types.SimpleNamespace(new=Mock()))
            modules = {"gi": types.SimpleNamespace(require_version=Mock()),
                "gi.repository": types.SimpleNamespace(Gtk=gtk, Gio=gio, GLib=glib)}
            with self.subTest(gtk=gtk_ready, host=host_ready), patch.dict("sys.modules", modules):
                with self.assertRaises(SystemExit) as outcome:
                    exec(session.READY_CHECK, {})
                self.assertEqual(outcome.exception.code, expected)
                self.assertEqual(bus.call_sync.called, gtk_ready)
