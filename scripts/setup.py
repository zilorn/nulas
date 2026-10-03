#!/usr/bin/env python3
"""User-local installation, staged updates, and cross-platform foreground runner."""
import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shlex
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.request
import zipfile

REPOSITORY = 'https://github.com/zilorn/nulas.git'
TIMEOUT = 1800


def command(*args, cwd=None, capture=False):
    result = subprocess.run([str(a) for a in args], cwd=cwd, check=True,
                            timeout=TIMEOUT, text=True,
                            stdout=subprocess.PIPE if capture else None)
    return result.stdout.strip() if capture else ''


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(dir=path.parent, prefix='.pending-')
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            json.dump(value, stream, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def load(home):
    return json.loads((home / 'installation.json').read_text(encoding='utf-8'))


@contextlib.contextmanager
def locked(home):
    home.mkdir(parents=True, exist_ok=True)
    path = home / '.update-lock'
    try:
        path.mkdir()
    except FileExistsError:
        raise RuntimeError('Another installation/update is active. After an interrupted update, inspect and remove ' + str(path))
    try:
        yield
    finally:
        path.rmdir()


def download(url):
    if not url.startswith('https://'):
        raise RuntimeError('Downloads require HTTPS')
    with urllib.request.urlopen(url, timeout=60) as response:
        if not response.url.startswith('https://'):
            raise RuntimeError('Refusing HTTPS downgrade')
        return response.read(256 * 1024 * 1024 + 1)


def unpack(data, digest, destination, suffix):
    if len(data) > 256 * 1024 * 1024 or hashlib.sha256(data).hexdigest() != digest:
        raise RuntimeError('Archive size/checksum mismatch')
    with tempfile.TemporaryDirectory() as temporary:
        archive = Path(temporary) / ('asset' + suffix)
        archive.write_bytes(data)
        output = Path(temporary) / 'extracted'
        output.mkdir()
        if suffix == '.zip':
            with zipfile.ZipFile(archive) as package:
                for item in package.infolist():
                    target = (output / item.filename).resolve()
                    if not target.is_relative_to(output.resolve()) or (item.external_attr >> 16) & 0o170000 == 0o120000:
                        raise RuntimeError('Unsafe archive member')
                package.extractall(output)
        else:
            with tarfile.open(archive) as package:
                # Validate paths and link targets before extraction (also on Python 3.10).
                members = package.getmembers()
                links = {item.name.rstrip("/") for item in members if item.issym() or item.islnk()}
                for item in members:
                    if any(str(parent) in links for parent in Path(item.name).parents):
                        raise RuntimeError("Archive member traverses a link")
                    if not (output / item.name).resolve().is_relative_to(output.resolve()) or not (item.isdir() or item.isfile() or item.issym() or item.islnk()):
                        raise RuntimeError('Unsafe archive member')
                    if item.issym() or item.islnk():
                        base = (output / item.name).parent if item.issym() else output
                        if not (base / item.linkname).resolve().is_relative_to(output.resolve()):
                            raise RuntimeError('Unsafe archive link')
                package.extractall(output)
        destination.mkdir(parents=True, exist_ok=True)
        for item in output.iterdir():
            target = destination / item.name
            if target.exists():
                raise RuntimeError('Refusing to overwrite existing tool: ' + str(target))
            shutil.move(str(item), target)


def version_ok(tool, minimum):
    if not shutil.which(tool):
        return False
    try:
        text = command(tool, 'version' if tool == 'go' else '--version', capture=True)
        match = re.search(r'(\d+)\.(\d+)', text)
        return bool(match and tuple(map(int, match.groups())) >= minimum)
    except (OSError, subprocess.SubprocessError):
        return False


def package_install(packages):
    system = platform.system()
    if system == 'Darwin':
        brew = shutil.which('brew')
        if not brew:
            brew = next((p for p in ('/opt/homebrew/bin/brew', '/usr/local/bin/brew') if Path(p).is_file()), None)
        if not brew:
            print('Installing Homebrew to provision missing dependencies; macOS may request administrator approval.', flush=True)
            with tempfile.TemporaryDirectory() as temporary:
                installer = Path(temporary) / 'homebrew.sh'
                installer.write_bytes(download('https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh'))
                command('/bin/bash', installer)
            brew = next((p for p in ('/opt/homebrew/bin/brew', '/usr/local/bin/brew') if Path(p).is_file()), None)
        if not brew:
            raise RuntimeError('Homebrew installation did not produce a usable brew executable')
        os.environ['PATH'] += os.pathsep + str(Path(brew).parent)
        command(brew, 'install', *packages)
        return
    if system == 'Windows':
        ids = {'git': 'Git.Git', 'python3': 'Python.Python.3.12'}
        for package in packages:
            command('winget', 'install', '--exact', '--id', ids[package], '--source', 'winget',
                    '--accept-package-agreements', '--accept-source-agreements')
        # Installers may modify persistent PATH only, so reload before checking tools.
        command_path = command('powershell.exe', '-NoProfile', '-Command',
                               "[Environment]::GetEnvironmentVariable('Path','Machine')+';'+[Environment]::GetEnvironmentVariable('Path','User')", capture=True)
        os.environ['PATH'] += os.pathsep + command_path
        return
    managers = [('apt-get', ['apt-get', 'install', '-y']),
                ('pacman', ['pacman', '-S', '--needed', '--noconfirm']),
                ('dnf', ['dnf', 'install', '-y']), ('zypper', ['zypper', '--non-interactive', 'install']),
                ('apk', ['apk', 'add'])]
    for name, args in managers:
        if shutil.which(name):
            prefix = [] if os.geteuid() == 0 else ['sudo']
            if name == 'apt-get':
                command(*prefix, 'apt-get', 'update')
            command(*prefix, *args, *packages)
            return
    raise RuntimeError('No supported package manager. Install ' + ', '.join(packages) + ' and rerun.')


def dependencies(home):
    if not version_ok('git', (2, 0)):
        package_install(['git'])
    if not shutil.which('git'):
        raise RuntimeError('Git still unavailable; reopen the terminal and rerun')
    system = {'Linux': 'linux', 'Darwin': 'darwin', 'Windows': 'win'}.get(platform.system())
    arch = {'x86_64': 'x64', 'AMD64': 'x64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
    if not system or not arch:
        raise RuntimeError('Supported platforms: Linux/macOS/Windows on x64 or arm64')
    tools = home / 'tools'
    tools.mkdir(exist_ok=True)
    if not version_ok('node', (24, 0)):
        index = json.loads(download('https://nodejs.org/dist/index.json'))
        version = next(v['version'] for v in index if v['version'].startswith('v24.'))
        suffix = '.zip' if system == 'win' else '.tar.gz'
        filename = f'node-{version}-{system}-{arch}{suffix}'
        base = f'https://nodejs.org/dist/{version}/'
        sums = download(base + 'SHASUMS256.txt').decode()
        digest = next(line.split()[0] for line in sums.splitlines() if line.split()[-1] == filename)
        unpack(download(base + filename), digest, tools, suffix)
        node_dir = tools / filename.removesuffix(suffix)
        node_bin = node_dir if system == 'win' else node_dir / 'bin'
        os.environ['PATH'] = str(node_bin) + os.pathsep + os.environ['PATH']
    if not version_ok('go', (1, 23)):
        index = json.loads(download('https://go.dev/dl/?mode=json'))
        go_os = 'windows' if system == 'win' else system
        go_arch = 'amd64' if arch == 'x64' else arch
        asset = next(f for v in index if v['stable'] for f in v['files']
                     if f['os'] == go_os and f['arch'] == go_arch and f['kind'] == 'archive')
        suffix = '.zip' if system == 'win' else '.tar.gz'
        target = tools / asset['version']
        if not target.exists():
            unpack(download('https://go.dev/dl/' + asset['filename']), asset['sha256'], target, suffix)
        os.environ['PATH'] = str(target / 'go' / 'bin') + os.pathsep + os.environ['PATH']
    if not version_ok('node', (24, 0)) or not version_ok('go', (1, 23)):
        raise RuntimeError('Node.js 24+ and Go 1.23+ are required')
    return {'node': shutil.which('node'), 'go': shutil.which('go'), 'git': shutil.which('git'), 'python': sys.executable}


def activate_tools(metadata):
    for key in ('git', 'node', 'go'):
        os.environ['PATH'] = str(Path(metadata['tools'][key]).parent) + os.pathsep + os.environ['PATH']


def build(source, tools):
    activate_tools({'tools': tools})
    web = source / 'web'
    manager = json.loads((web / 'package.json').read_text())['packageManager']
    # Reuse pnpm only when it matches this checkout's locked packageManager.
    expected = manager.split('@')[1]
    if shutil.which('pnpm') and command('pnpm', '--version', capture=True) == expected:
        pnpm = [shutil.which('pnpm')]
    else:
        npm = shutil.which('npm.cmd' if os.name == 'nt' else 'npm')
        if not npm:
            raise RuntimeError('npm is required to provision the pinned pnpm')
        prefix = source / '.build-tools'
        command(npm, 'install', '--prefix', prefix, '--no-audit', '--no-fund', manager)
        pnpm = [tools['node'], prefix / 'node_modules/pnpm/bin/pnpm.cjs']
    for args in [('install', '--frozen-lockfile'), ('typecheck',), ('build',)]:
        command(*pnpm, *args, cwd=web)
    (source / 'bin').mkdir(exist_ok=True)
    command(tools['go'], 'build', '-o', source / 'bin' / binary_name(), '.', cwd=source / 'backend')
    if not (web / '.output/server/index.mjs').is_file():
        raise RuntimeError('SSR build output is missing')


def binary_name():
    return 'nulas.exe' if os.name == 'nt' else 'nulas'


def stage(home, metadata, commit):
    # Every build uses a fresh checkout. Never mutate the active version or user data.
    release = home / 'releases' / (commit[:12] + '-' + str(time.time_ns()))
    release.parent.mkdir(exist_ok=True)
    command('git', 'clone', '--no-hardlinks', '--no-checkout', home / 'source', release)
    command('git', 'checkout', '--detach', commit, cwd=release)
    build(release, metadata['tools'])
    updated = dict(metadata, current=str(release), commit=commit, updated=time.time(), error=None)
    atomic_json(home / 'installation.json', updated)
    return updated


def update(home, check=False):
    with locked(home):
        metadata = load(home)
        activate_tools(metadata)
        source = home / 'source'
        command('git', 'fetch', '--no-tags', 'origin', metadata['branch'], cwd=source)
        latest = command('git', 'rev-parse', 'FETCH_HEAD', cwd=source, capture=True)
        current = metadata['commit']
        print(f'Installed: {current}\nLatest:    {latest}', flush=True)
        if latest == current:
            if metadata.get('error'):
                metadata['error'] = None
                atomic_json(home / 'installation.json', metadata)
            print('Nulas is up to date.', flush=True)
            return
        # Reject remote history rewrites instead of silently downgrading/diverging.
        command('git', 'merge-base', '--is-ancestor', current, latest, cwd=source)
        print('Update available.', flush=True)
        if not check:
            stage(home, metadata, latest)
            print('Update installed. Restart Nulas to use the new version.', flush=True)


def set_auto(home, enabled):
    with locked(home):
        metadata = load(home)
        metadata['autoUpdate'] = enabled
        atomic_json(home / 'installation.json', metadata)
    print('Automatic updates ' + ('enabled.' if enabled else 'disabled.'))


def watch(home, stop):
    while not stop.is_set():
        try:
            if load(home).get('autoUpdate', False):
                update(home)
        except Exception as error:
            print(f'Automatic update failed: {error}', file=sys.stderr, flush=True)
            # Keep the failure visible across sessions; never change the active release.
            try:
                with locked(home):
                    metadata = load(home)
                    metadata['error'] = str(error)
                    atomic_json(home / 'installation.json', metadata)
            except Exception:
                pass
        stop.wait(6 * 3600)


def run_servers(home):
    metadata = load(home)
    activate_tools(metadata)
    source = Path(metadata['current'])
    environment = dict(os.environ)
    if environment.get('NULAS_WEB_DIR') or environment.get('NULAS_SSR_URL', 'http://127.0.0.1:3001') != 'http://127.0.0.1:3001':
        raise RuntimeError('nulas run requires the default SSR origin and no NULAS_WEB_DIR')
    environment.setdefault('NULAS_DATA_DIR', str(home / 'data'))
    environment.setdefault('NULAS_CORE_DIR', str(home / 'runtime/core'))
    environment.setdefault('NULAS_CORE_INSTALLER', str(source / 'scripts/install_core.py'))
    environment.setdefault('NULAS_TRAY_SCRIPT', str(source / 'scripts/tray.py'))
    environment.setdefault('NULAS_PYTHON', metadata['tools']['python'])
    processes = []
    stop = threading.Event()
    def interrupt(_signum, _frame):
        stop.set()
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupt)
    try:
        processes.append(subprocess.Popen([str(source / 'bin' / binary_name())], cwd=source / 'backend', env=environment))
        environment.update(NITRO_HOST='127.0.0.1', NITRO_PORT='3001')
        processes.append(subprocess.Popen([metadata['tools']['node'], str(source / 'web/.output/server/index.mjs')],
                                          cwd=source / 'web', env=environment))
        threading.Thread(target=watch, args=(home, stop), daemon=True).start()
        print('Nulas: http://' + environment.get('NULAS_ADDR', '127.0.0.1:8080'), flush=True)
        while not stop.wait(0.5):
            for process in processes:
                if process.poll() is not None:
                    raise RuntimeError(f'A server exited with status {process.returncode}')
    finally:
        stop.set()
        for process in processes:
            if process.poll() is None:
                process.terminate()
        for process in processes:
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def add_path(home, user_home=None):
    user_home = Path(user_home or Path.home())
    bin_dir = str(home / 'bin')
    if os.name == 'nt':
        # Avoid setx's truncation and expansion of the user's existing PATH.
        import winreg
        with winreg.CreateKey(winreg.HKEY_CURRENT_USER, 'Environment') as key:
            try:
                existing, kind = winreg.QueryValueEx(key, 'Path')
            except FileNotFoundError:
                existing, kind = '', winreg.REG_EXPAND_SZ
            if bin_dir.lower() not in [p.lower() for p in existing.split(';')]:
                winreg.SetValueEx(key, 'Path', 0, kind, existing + (';' if existing else '') + bin_dir)
        import ctypes
        result = ctypes.c_size_t()
        ctypes.windll.user32.SendMessageTimeoutW(0xffff, 0x1a, 0, 'Environment', 2, 5000, ctypes.byref(result))
        return
    quoted = shlex.quote(bin_dir)
    line = '\n# Nulas CLI\nexport PATH=' + quoted + ':"$PATH"\n'
    paths = [user_home / '.bashrc', user_home / '.profile',
             Path(os.environ.get('ZDOTDIR', str(user_home))) / '.zshrc']
    # Bash login shells read only the first existing login file.
    for filename in ('.bash_profile', '.bash_login'):
        if (user_home / filename).exists():
            paths.append(user_home / filename)
            break
    for path in paths:
        path.parent.mkdir(parents=True, exist_ok=True)
        existing = path.read_text() if path.exists() else ''
        if line not in existing:
            with path.open('a') as stream:
                stream.write(line)
    fish = Path(os.environ.get('XDG_CONFIG_HOME', str(user_home / '.config'))) / 'fish/conf.d/nulas.fish'
    fish.parent.mkdir(parents=True, exist_ok=True)
    # Single-quoted fish strings support escaped backslash and quote.
    fish_path = "'" + bin_dir.replace('\\', '\\\\').replace("'", "\\'") + "'"
    fish_line = '\n# Nulas CLI\nfish_add_path --path ' + fish_path + '\n'
    existing = fish.read_text() if fish.exists() else ''
    if fish_line not in existing:
        with fish.open('a') as stream:
            stream.write(fish_line)


def launchers(home):
    directory = home / 'bin'
    directory.mkdir(exist_ok=True)
    launcher = directory / 'launcher.py'
    # This stable dispatcher remains outside release trees; updates are an atomic pointer switch.
    launcher.write_text('''import json, os, pathlib, subprocess, sys
home = pathlib.Path(__file__).resolve().parent.parent
metadata = json.loads((home / 'installation.json').read_text(encoding='utf-8'))
os.environ['NULAS_INSTALL_HOME'] = str(home)
os.environ['NULAS_PYTHON'] = metadata['tools']['python']
for key in ('git', 'node', 'go'):
    os.environ['PATH'] = str(pathlib.Path(metadata['tools'][key]).parent) + os.pathsep + os.environ['PATH']
os.environ.setdefault('NULAS_DATA_DIR', str(home / 'data'))
os.environ.setdefault('NULAS_CORE_DIR', str(home / 'runtime/core'))
root = pathlib.Path(metadata['current'])
args = sys.argv[1:]
if args and args[0] in ('run', 'update'):
    cmd = [metadata['tools']['python'], str(root / 'scripts/setup.py'), *args, '--home', str(home)]
else:
    cmd = [str(root / 'bin' / ('nulas.exe' if os.name == 'nt' else 'nulas')), *args]
try:
    sys.exit(subprocess.call(cmd))
except KeyboardInterrupt:
    sys.exit(130)
''', encoding='utf-8')
    python = sys.executable
    if os.name == 'nt':
        if any(c in python + str(launcher) for c in '%\r\n'):
            raise RuntimeError('Windows installation paths cannot contain %, CR or LF')
        # Decode UTF-8 paths correctly even for non-ASCII Windows user names.
        batch = ('@echo off\r\nsetlocal\r\n'
                 'for /f "tokens=2 delims=:" %%C in (\'chcp\') do set "_nulas_cp=%%C"\r\n'
                 'chcp 65001 >nul\r\n'
                 + f'"{python}" "{launcher}" %*\r\n'
                 + 'set "_nulas_status=%errorlevel%"\r\n'
                 'chcp %_nulas_cp% >nul\r\nexit /b %_nulas_status%\r\n')
        (directory / 'nulas.cmd').write_bytes(batch.encode('utf-8'))
    else:
        entry = directory / 'nulas'
        entry.write_text('#!/bin/sh\nexec ' + shlex.quote(python) + ' ' + shlex.quote(str(launcher)) + ' "$@"\n')
        entry.chmod(0o755)


def install(home, repository, branch):
    if os.name != 'nt' and os.geteuid() == 0:
        raise RuntimeError('Install as your ordinary user, not root')
    with locked(home):
        if (home / 'installation.json').exists() or (home / 'source').exists():
            raise RuntimeError('Installation already exists; use nulas update. An incomplete installation must be inspected first.')
        if (home / 'bin').exists():
            raise RuntimeError('Refusing to overwrite an existing bin directory')
        tools = dependencies(home)
        command('git', 'clone', '--single-branch', '--branch', branch, repository, home / 'source')
        commit = command('git', 'rev-parse', 'HEAD', cwd=home / 'source', capture=True)
        metadata = {'repository': repository, 'branch': branch, 'tools': tools, 'autoUpdate': False}
        stage(home, metadata, commit)
        launchers(home)
        add_path(home)
    print(f'Installed Nulas in {home}. Reopen your terminal, then run nulas run. Automatic updates are disabled.')


def main():
    if sys.version_info < (3, 10):
        raise RuntimeError('Python 3.10+ is required')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['install', 'update', 'run'])
    parser.add_argument('--home', type=Path, default=Path(os.environ.get('NULAS_INSTALL_HOME', str(Path.home() / '.local/share/nulas'))))
    parser.add_argument('--repository', default=REPOSITORY)
    parser.add_argument('--branch', default='main')
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--auto', choices=['on', 'off', 'status'])
    parser.add_argument('--watch', action='store_true', help='Run automatic update scheduler in foreground')
    args = parser.parse_args()
    home = args.home.expanduser().resolve()
    if args.action != 'update' and (args.check or args.auto or args.watch):
        parser.error('update options require the update command')
    if args.action == 'install':
        install(home, args.repository, args.branch)
    elif args.action == 'run':
        run_servers(home)
    elif sum(bool(v) for v in (args.auto, args.watch, args.check)) > 1:
        parser.error('--check, --auto and --watch are mutually exclusive')
    elif args.auto:
        if args.auto == 'status':
            metadata = load(home)
            print(json.dumps({k: metadata.get(k) for k in ('autoUpdate', 'commit', 'updated', 'error')}, indent=2))
        else:
            set_auto(home, args.auto == 'on')
    elif args.watch:
        stop = threading.Event()
        for signum in (signal.SIGINT, signal.SIGTERM):
            signal.signal(signum, lambda *_: stop.set())
        watch(home, stop)
    else:
        update(home, args.check)


if __name__ == '__main__':
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print(f'nulas: {error}', file=sys.stderr)
        sys.exit(1)
