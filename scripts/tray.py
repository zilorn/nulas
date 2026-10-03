"""Optional native tray helper. No controller access or credentials."""
import argparse
import sys
import threading
import webbrowser
from urllib.parse import urlsplit
import ipaddress


def dashboard_url(value):
    parsed = urlsplit(value)
    if (parsed.scheme != "http" or not parsed.hostname
            or not ipaddress.ip_address(parsed.hostname).is_loopback
            or parsed.username or parsed.password or parsed.path not in ("", "/")
            or parsed.query or parsed.fragment or not parsed.port):
        raise ValueError("A loopback HTTP dashboard URL is required")
    return value.rstrip("/")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True, type=dashboard_url)
    args = parser.parse_args()
    import pystray
    from PIL import Image, ImageDraw

    # Xorg's fallback has no context menu. Require a functional tray backend.
    if not pystray.Icon.HAS_MENU:
        raise RuntimeError("Choose a GTK/AppIndicator backend with menu support")
    image = Image.new("RGBA", (64, 64))
    draw = ImageDraw.Draw(image)
    draw.rounded_rectangle((4, 4, 60, 60), radius=14, fill="#268366")
    draw.line([(20, 46), (20, 18), (44, 46), (44, 18)], fill="white", width=6)

    def open_page(path):
        def activate(icon, item):
            webbrowser.open(args.url + path)
        return activate

    icon = pystray.Icon("nulas", image, "Nulas", pystray.Menu(
        pystray.MenuItem("打开 Nulas", open_page("/"), default=True),
        pystray.MenuItem("节点管理", open_page("/nodes")),
        pystray.MenuItem("后台任务", open_page("/tasks")),
    ))

    def setup(active):
        active.visible = True
        print("READY", flush=True)
        # EOF stops the helper even if its parent is interrupted or crashes.
        def watch_parent():
            sys.stdin.buffer.read()
            active.stop()
        threading.Thread(target=watch_parent, daemon=True).start()

    # macOS requires the native event loop on the main thread.
    icon.run(setup=setup)


if __name__ == "__main__":
    main()
