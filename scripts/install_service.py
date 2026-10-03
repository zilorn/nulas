#!/usr/bin/env python3
"""Install the Nulas user service without starting it or enabling boot startup."""
import os
from pathlib import Path
import shutil
import subprocess
import sys

MARKER = "# Managed by Nulas installer\n"


def unit_quote(value):
    if any(c in value for c in "\n\r\x00"):
        raise ValueError("Service paths cannot contain newlines or NUL")
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"').replace("%", "%%") + '"'


def service_text(root, node, installation=None):
    start = ":/bin/bash " + unit_quote(str(root / "scripts/start.sh"))
    if installation:
        import json
        metadata = json.loads((installation / "installation.json").read_text(encoding="utf-8"))
        start = ":" + unit_quote(metadata["tools"]["python"]) + " " + unit_quote(str(installation / "bin/launcher.py")) + " run"
    return (MARKER + "[Unit]\nDescription=Nulas frontend and backend\n\n[Service]\n"
            + "Type=simple\nWorkingDirectory=" + unit_quote(str(root)) + "\n"
            + "Environment=" + unit_quote("PATH=" + str(Path(node).parent) + ":/usr/local/bin:/usr/bin:/bin") + "\n"
            + "EnvironmentFile=-%h/.config/nulas/service.env\n"
            + "ExecStart=" + start + "\n"
            + "Restart=on-failure\nRestartSec=5\nTimeoutStopSec=30\nKillMode=control-group\n"
            + "\n[Install]\nWantedBy=default.target\n")


def main():
    if sys.platform != "linux" or os.geteuid() == 0:
        raise SystemExit("Install as the ordinary Linux user who runs Nulas, not root.")
    root = Path(__file__).resolve().parent.parent
    node = shutil.which("node")
    if not node or int(subprocess.check_output([node, "-p", "process.versions.node.split('.')[0]"], text=True)) < 24:
        raise SystemExit("Node.js 24+ is required.")
    if not (root / "bin/nulas").is_file() or not (root / "web/.output/server/index.mjs").is_file():
        raise SystemExit("Run scripts/build.sh first.")
    config = Path(os.environ.get("XDG_CONFIG_HOME", str(Path.home() / ".config")))
    path = config / "systemd/user/nulas.service"
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and not path.read_text().startswith(MARKER):
        raise SystemExit("Refusing to overwrite an existing service not created by Nulas.")
    installation = os.environ.get("NULAS_INSTALL_HOME")
    content = service_text(root, node, Path(installation) if installation else None)
    temporary = path.with_suffix(".service.pending")
    with temporary.open("x") as output:
        output.write(content)
        output.flush()
        os.fsync(output.fileno())
    temporary.replace(path)
    subprocess.run(["systemctl", "--user", "daemon-reload"], check=True)
    print(f"Installed {path}. Current services were not started or stopped.")
    print("Enable linger for this user using loginctl enable-linger, then enable startup in the Nulas page.")
    print("Optional deployment overrides: ~/.config/nulas/service.env (chmod 600).")


if __name__ == "__main__":
    main()
