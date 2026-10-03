"""On-demand Linux tray dependencies; never modify the system Python with pip."""
import os
from pathlib import Path
import shutil
import select
import signal
import time
import subprocess
import sys

GI_CHECK = """import gi
gi.require_version('Gtk', '3.0')
from gi.repository import Gtk
try:
    gi.require_version('AppIndicator3', '0.1')
    from gi.repository import AppIndicator3
except ValueError:
    gi.require_version('AyatanaAppIndicator3', '0.1')
    from gi.repository import AyatanaAppIndicator3
"""


def available(python, code):
    return subprocess.run([str(python), "-c", code], stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL, timeout=15).returncode == 0


def package_command():
    for manager, args in (
        ("apt-get", ["install", "-y", "python3-gi", "python3-venv", "gir1.2-gtk-3.0", "gir1.2-ayatanaappindicator3-0.1"]),
        ("dnf", ["install", "-y", "python3-gobject", "gtk3", "libayatana-appindicator-gtk3"]),
        ("pacman", ["-S", "--needed", "--noconfirm", "python-gobject", "gtk3", "libayatana-appindicator"]),
    ):
        executable = shutil.which(manager)
        if executable:
            command = [executable, *args]
            if os.geteuid() == 0:
                return command
            if shutil.which("pkexec"):
                return [shutil.which("pkexec"), *command]
            if shutil.which("sudo"):
                return [shutil.which("sudo"), "-n", *command]
            raise RuntimeError("安装托盘系统依赖需要 pkexec 或已授权的 sudo。")
    raise RuntimeError("此 Linux 发行版无法自动安装托盘依赖，请手动安装 GTK3、PyGObject 与 AyatanaAppIndicator3。")


def run(command):
    try:
        process = subprocess.Popen([str(arg) for arg in command], start_new_session=True,
                                   stdin=subprocess.DEVNULL, stdout=sys.stderr, stderr=sys.stderr)
        deadline = time.monotonic() + 180
        try:
            while process.poll() is None:
                readable, _, _ = select.select([sys.stdin], [], [], 0.2)
                if readable and not os.read(sys.stdin.fileno(), 1):
                    raise RuntimeError("后台已停止或托盘已关闭，依赖安装已中断。")
                if time.monotonic() >= deadline:
                    raise subprocess.TimeoutExpired(command, 180)
            if process.returncode:
                raise subprocess.CalledProcessError(process.returncode, command)
        finally:
            if process.poll() is None:
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                    process.wait(timeout=2)
                except (ProcessLookupError, PermissionError, subprocess.TimeoutExpired):
                    process.kill()
                    process.wait()
    except (subprocess.SubprocessError, OSError) as exc:
        raise RuntimeError("托盘依赖安装失败或超时；请检查网络、软件源与管理员授权后重试。") from exc


def prepare_linux():
    if not (os.environ.get("DISPLAY") or os.environ.get("WAYLAND_DISPLAY")):
        raise RuntimeError("Linux 托盘需要桌面会话与托盘区域，无法在无桌面环境启动。")
    python = Path(sys.executable)
    if available(python, GI_CHECK) and available(python, "import pystray; from PIL import Image; assert pystray.Icon.HAS_MENU"):
        return
    cache = Path(os.environ.get("XDG_CACHE_HOME", str(Path.home() / ".cache")))
    environment = cache / "nulas" / ("tray-python%d.%d" % sys.version_info[:2])
    target = environment / "bin" / "python"
    if target.exists() and available(target, GI_CHECK) and available(target, "import pystray; from PIL import Image; assert pystray.Icon.HAS_MENU"):
        os.execv(str(target), [str(target), *sys.argv])
        return
    print("STATUS 正在检查并安装 Linux 托盘依赖；系统软件包可能请求管理员授权。", flush=True)
    # Distribution GI bindings must match the selected Python interpreter.
    if not available(python, GI_CHECK) or not available(python, "import venv, ensurepip"):
        run(package_command())
    if not available(python, GI_CHECK):
        raise RuntimeError("系统依赖已安装，但当前 Python 无法加载 GTK/AppIndicator；请将 NULAS_PYTHON 设置为发行版 Python。")
    if not target.exists():
        run([python, "-m", "venv", "--system-site-packages", environment])
    if not available(target, "import pystray; from PIL import Image; assert pystray.Icon.HAS_MENU"):
        print("STATUS 正在独立 Python 环境安装托盘依赖。", flush=True)
        run([target, "-m", "pip", "install", "--disable-pip-version-check", "--no-input",
             "-r", Path(__file__).with_name("requirements-tray.txt")])
    if not available(target, GI_CHECK) or not available(target, "import pystray; from PIL import Image; assert pystray.Icon.HAS_MENU"):
        raise RuntimeError("托盘依赖检查失败，请检查 Python 与桌面 GTK/AppIndicator 支持。")
    os.execv(str(target), [str(target), *sys.argv])
