import unittest
from tray import dashboard_url


class TrayURLTests(unittest.TestCase):
    def test_loopback_and_ipv6(self):
        for value in ("http://127.0.0.1:8080", "http://[::1]:8080/"):
            self.assertEqual(dashboard_url(value), value.rstrip("/"))

    def test_reject_external_or_credential_urls(self):
        for value in ("https://127.0.0.1:8080", "http://example.com:8080",
                      "http://192.168.1.1:8080", "http://user:secret@127.0.0.1:8080",
                      "http://127.0.0.1:8080/path", "http://127.0.0.1:8080?secret=x",
                      "http://127.0.0.1:8080#fragment", "http://127.0.0.1"):
            with self.assertRaises(ValueError):
                dashboard_url(value)


class NativeHelperContractTests(unittest.TestCase):
    def test_main_thread_ready_menu_and_parent_eof(self):
        import io
        import threading
        import types
        from unittest.mock import patch, Mock
        import tray

        instances = []

        class Icon:
            HAS_MENU = True

            def __init__(self, name, image, title, menu):
                self.menu = menu
                self.visible = False
                self.stopped = threading.Event()
                instances.append(self)

            def stop(self):
                self.stopped.set()

            def run(self, setup):
                self_thread = threading.current_thread()
                if self_thread is not threading.main_thread():
                    raise AssertionError("native event loop moved off main thread")
                setup(self)
                if not self.stopped.wait(1):
                    raise AssertionError("parent EOF did not stop tray")

        native = types.SimpleNamespace(Icon=Icon, Menu=lambda *items: items,
            MenuItem=lambda title, action, **kwargs: types.SimpleNamespace(action=action))
        pillow = types.SimpleNamespace(Image=types.SimpleNamespace(new=Mock()),
            ImageDraw=types.SimpleNamespace(Draw=Mock()))
        output = io.StringIO()
        stdin = types.SimpleNamespace(buffer=io.BytesIO())
        with patch.dict("sys.modules", {"pystray": native, "PIL": pillow}), \
                patch.object(tray.sys, "argv", ["tray.py", "--url", "http://127.0.0.1:8080"]), \
                patch.object(tray.sys, "stdin", stdin), patch.object(tray.sys, "stdout", output), \
                patch.object(tray.webbrowser, "open") as open_browser:
            tray.main()
            for item in instances[0].menu:
                item.action(instances[0], item)
        self.assertTrue(instances[0].visible)
        self.assertEqual(output.getvalue(), "READY\n")
        self.assertEqual([call.args[0] for call in open_browser.call_args_list],
            ["http://127.0.0.1:8080/", "http://127.0.0.1:8080/nodes", "http://127.0.0.1:8080/tasks"])
