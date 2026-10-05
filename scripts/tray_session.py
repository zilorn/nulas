"""Wait for the current Linux user's desktop without installing or changing it."""
import os
from pathlib import Path
import select
import socket
import subprocess
import sys

SESSION_KEYS = {"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS",
                "XDG_RUNTIME_DIR", "XDG_SESSION_TYPE", "XDG_CURRENT_DESKTOP"}
READY_CHECK = """import gi
gi.require_version('Gtk', '3.0')
from gi.repository import Gtk, Gio, GLib
if not Gtk.init_check()[0]:
    raise SystemExit(1)
bus = Gio.bus_get_sync(Gio.BusType.SESSION, None)
result = bus.call_sync('org.kde.StatusNotifierWatcher', '/StatusNotifierWatcher',
    'org.freedesktop.DBus.Properties', 'Get',
    GLib.Variant('(ss)', ('org.kde.StatusNotifierWatcher', 'IsStatusNotifierHostRegistered')),
    GLib.VariantType.new('(v)'), Gio.DBusCallFlags.NONE, 2000, None)
raise SystemExit(0 if result.unpack()[0] else 1)
"""


def session_environment():
    values = {}
    # A boot-started user service retains its original environment. Desktop login
    # usually imports fresh variables into the user manager.
    try:
        result = subprocess.run(["systemctl", "--user", "show-environment"],
                                capture_output=True, text=True, timeout=3)
        if result.returncode == 0:
            for line in result.stdout.splitlines():
                key, sep, value = line.partition("=")
                if sep and key in SESSION_KEYS:
                    values[key] = value
    except (OSError, subprocess.SubprocessError):
        pass
    # Some desktops do not import their environment into systemd. Read only
    # allowlisted session variables from processes owned by this user.
    if not (values.get("DISPLAY") or values.get("WAYLAND_DISPLAY")):
        for entry in Path("/proc").iterdir():
            if not entry.name.isdigit():
                continue
            try:
                if entry.stat().st_uid != os.getuid():
                    continue
                env = {}
                for item in (entry / "environ").read_bytes().split(b"\0"):
                    key, sep, value = item.partition(b"=")
                    name = key.decode(errors="replace")
                    if sep and name in SESSION_KEYS:
                        env[name] = value.decode(errors="replace")
                if env.get("XDG_SESSION_TYPE") in ("x11", "wayland") and (env.get("DISPLAY") or env.get("WAYLAND_DISPLAY")):
                    values.update(env)
                    break
            except (OSError, ValueError):
                continue
    return values


def display_available():
    runtime = os.environ.get("XDG_RUNTIME_DIR", "/run/user/" + str(os.getuid()))
    paths = []
    wayland = os.environ.get("WAYLAND_DISPLAY")
    if wayland:
        paths.append(Path(runtime) / wayland)
    display = os.environ.get("DISPLAY", "")
    if display.startswith(":") and display[1:].split(".")[0].isdigit():
        paths.append(Path("/tmp/.X11-unix") / ("X" + display[1:].split(".")[0]))
    for path in paths:
        try:
            with socket.socket(socket.AF_UNIX) as connection:
                connection.settimeout(1)
                connection.connect(str(path))
            os.environ.setdefault("XDG_RUNTIME_DIR", runtime)
            os.environ.setdefault("DBUS_SESSION_BUS_ADDRESS", "unix:path=" + runtime + "/bus")
            return True
        except OSError:
            pass
    return False


def desktop_ready():
    try:
        return subprocess.run([sys.executable, "-c", READY_CHECK],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                              timeout=5).returncode == 0
    except (OSError, subprocess.SubprocessError):
        return False


def wait_for_session(check=display_available):
    announced = False
    while True:
        os.environ.update(session_environment())
        if check():
            return
        if not announced:
            print("WAIT 正在等待 Linux 桌面会话与托盘区域就绪；请登录桌面并启用 AppIndicator 托盘支持。", flush=True)
            announced = True
        readable, _, _ = select.select([sys.stdin], [], [], 2)
        if readable and not os.read(sys.stdin.fileno(), 1):
            raise RuntimeError("后台已停止或托盘已关闭，桌面等待已取消。")
