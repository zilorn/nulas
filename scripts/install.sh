#!/bin/sh
# POSIX bootstrap: works from bash, zsh, fish (via sh), and macOS's bundled shell.
set -eu
# A piped installer must not let child installers consume the remaining script.
run_interactive() {
  if ( : </dev/tty ) 2>/dev/null; then
    "$@" </dev/tty
  else
    "$@" </dev/null
  fi
}
if [ "$(id -u)" = 0 ]; then
  echo 'Run this installer as your normal user; package installation may ask for sudo.' >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  case "$(uname -s)" in
    Darwin)
      PATH="$PATH:/opt/homebrew/bin:/usr/local/bin"
      export PATH
      if ! command -v brew >/dev/null 2>&1; then
        BREW_INSTALLER=$(mktemp)
        trap 'rm -f "$BREW_INSTALLER"' EXIT HUP INT TERM
        curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o "$BREW_INSTALLER"
        run_interactive /bin/bash "$BREW_INSTALLER"
        rm -f "$BREW_INSTALLER"
        trap - EXIT HUP INT TERM
      fi
      run_interactive brew install python ;;
    Linux)
      if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get update
        sudo apt-get install -y python3 ca-certificates
      elif command -v pacman >/dev/null 2>&1; then
        sudo pacman -S --needed --noconfirm python ca-certificates
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y python3 ca-certificates
      elif command -v zypper >/dev/null 2>&1; then
        sudo zypper --non-interactive install python3 ca-certificates
      elif command -v apk >/dev/null 2>&1; then
        sudo apk add python3 ca-certificates
      else
        echo 'Install Python 3.10+ and rerun.' >&2; exit 1
      fi ;;
    *) echo 'Use scripts/install.ps1 on Windows.' >&2; exit 1 ;;
  esac
fi
python3 -c 'import sys; assert sys.version_info >= (3,10), "Python 3.10+ required"'
SCRIPT_DIR=""
if [ -f "$0" ]; then
  SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
fi
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/setup.py" ]; then
  run_interactive python3 "$SCRIPT_DIR/setup.py" install "$@"
  exit $?
fi
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT HUP INT TERM
python3 - "$TEMP_DIR/setup.py" <<'PY'
import sys, urllib.request
with urllib.request.urlopen('https://raw.githubusercontent.com/zilorn/nulas/main/scripts/setup.py', timeout=60) as response:
    data = response.read(1024 * 1024 + 1)
    if len(data) > 1024 * 1024 or not response.url.startswith('https://'):
        raise RuntimeError('Invalid installer download')
with open(sys.argv[1], 'wb') as stream:
    stream.write(data)
PY
run_interactive python3 "$TEMP_DIR/setup.py" install "$@"
