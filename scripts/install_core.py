#!/usr/bin/env python3
"""Download an official prebuilt core on demand; never vendor upstream source."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import tempfile
import urllib.request
import zipfile

REPO = "MetaCubeX/mihomo"
MAX_DOWNLOAD = 100 * 1024 * 1024


def asset_name(system, machine, tag):
    systems = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}
    arches = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64"}
    if system not in systems or machine not in arches:
        raise ValueError(f"Unsupported platform: {system}/{machine}")
    os_name, arch = systems[system], arches[machine]
    # Linux's compatible build avoids requiring newer x86 instruction sets.
    if os_name == "linux" and arch == "amd64":
        arch = "amd64-compatible"
    suffix = ".zip" if system == "Windows" else ".gz"
    return f"mihomo-{os_name}-{arch}-{tag}{suffix}"


def fetch(url, limit=MAX_DOWNLOAD):
    request = urllib.request.Request(url, headers={"User-Agent": "Nulas-core-installer", "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(request, timeout=60) as response:
        data = response.read(limit + 1)
    if len(data) > limit:
        raise ValueError("Download exceeds size limit")
    return data


def unpack(data, system):
    if system == "Windows":
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            candidates = [entry for entry in archive.infolist() if entry.filename.endswith(".exe") and not entry.is_dir()]
            if len(candidates) != 1 or candidates[0].file_size > MAX_DOWNLOAD:
                raise ValueError("Unexpected release archive")
            return archive.read(candidates[0])
    with gzip.GzipFile(fileobj=io.BytesIO(data)) as archive:
        binary = archive.read(MAX_DOWNLOAD + 1)
    if len(binary) > MAX_DOWNLOAD:
        raise ValueError("Decompressed binary exceeds size limit")
    return binary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="latest", help="Official release tag, e.g. v1.19.0")
    parser.add_argument("--sha256", help="Expected archive SHA256 if GitHub does not publish an asset digest")
    parser.add_argument("--output", type=Path, default=Path(__file__).resolve().parents[1] / ".runtime" / "core")
    args = parser.parse_args()
    if args.version != "latest" and not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.version):
        parser.error("Use latest or a stable version tag like v1.19.0")
    endpoint = "latest" if args.version == "latest" else f"tags/{args.version}"
    release = json.loads(fetch(f"https://api.github.com/repos/{REPO}/releases/{endpoint}", 5 * 1024 * 1024))
    tag = release["tag_name"]
    name = asset_name(platform.system(), platform.machine(), tag)
    asset = next((a for a in release["assets"] if a["name"] == name), None)
    if not asset:
        raise ValueError(f"Official asset not found: {name}; choose a supported stable release")
    digest = asset.get("digest") or ""
    expected = args.sha256 or (digest.removeprefix("sha256:") if digest.startswith("sha256:") else "")
    if not re.fullmatch(r"[a-fA-F0-9]{64}", expected):
        raise ValueError("Release has no SHA256 digest; supply a verified --sha256 before downloading")
    url = asset["browser_download_url"]
    if not url.startswith(f"https://github.com/{REPO}/releases/download/"):
        raise ValueError("Unexpected release download URL")
    data = fetch(url)
    if hashlib.sha256(data).hexdigest() != expected.lower():
        raise ValueError("Archive checksum mismatch")
    binary = unpack(data, platform.system())
    args.output.mkdir(parents=True, exist_ok=True)
    target = args.output / ("mihomo.exe" if platform.system() == "Windows" else "mihomo")
    if target.exists():
        raise ValueError(f"{target} already exists; choose a new --output directory to preserve it")
    with tempfile.NamedTemporaryFile(dir=args.output, delete=False) as file:
        temporary = Path(file.name)
        file.write(binary)
    try:
        temporary.chmod(0o700)
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)
    (args.output / "release.json").write_text(json.dumps({"repository": REPO, "tag": tag, "asset": name, "sha256": expected.lower()}, indent=2))
    print(f"Installed {tag}: {target}")
    print("Start the core separately with your own configuration; no system settings were changed.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError) as error:
        raise SystemExit(str(error))
