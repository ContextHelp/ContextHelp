#!/usr/bin/env bash
# install.sh — one-line installer for dpkms + ctxt
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ideacrafterslabs/ctxt/main/scripts/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/ideacrafterslabs/ctxt/main/scripts/install.sh | bash -s -- --binary dpkms
#
# Options:
#   --binary <name>     binary to install: dpkms | ctxt | all (default: all)
#   --version <tag>     specific release tag, e.g. v0.5.0 (default: latest)
#   --install-dir <dir> install destination (default: ~/.local/bin)
#   --no-service        skip systemd/launchd service setup
#   --dry-run           print actions without executing

set -euo pipefail

REPO="ideacrafterslabs/ctxt"
INSTALL_DIR="${HOME}/.local/bin"
BINARY="all"
VERSION=""
NO_SERVICE=false
DRY_RUN=false

# ── argument parsing ────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)     BINARY="$2";       shift 2 ;;
    --version)    VERSION="$2";      shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --no-service) NO_SERVICE=true;   shift ;;
    --dry-run)    DRY_RUN=true;      shift ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

# ── helpers ─────────────────────────────────────────────────────────────────
info()  { printf '\033[0;34m  ==> \033[0m%s\n' "$*"; }
ok()    { printf '\033[0;32m  ✓   \033[0m%s\n' "$*"; }
warn()  { printf '\033[0;33m  !   \033[0m%s\n' "$*" >&2; }
die()   { printf '\033[0;31m  ✗   \033[0m%s\n' "$*" >&2; exit 1; }
run()   { if $DRY_RUN; then echo "  [dry-run] $*"; else "$@"; fi; }

# ── detect OS / arch ────────────────────────────────────────────────────────
detect_os() {
  case "$(uname -s)" in
    Darwin)               echo "darwin" ;;
    Linux)                echo "linux"  ;;
    MINGW*|MSYS*|CYGWIN*)
      warn "Windows detected. Use the PowerShell installer instead:"
      warn "  irm https://raw.githubusercontent.com/${REPO}/main/scripts/install.ps1 | iex"
      exit 1 ;;
    *) die "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64)           echo "amd64" ;;
    amd64)            echo "amd64" ;;
    arm64|aarch64)    echo "arm64" ;;
    *)                die "Unsupported architecture: $(uname -m)" ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"

# ── fetch latest version if not pinned ──────────────────────────────────────
if [[ -z "$VERSION" ]]; then
  info "Fetching latest release tag..."
  if command -v curl &>/dev/null; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  elif command -v wget &>/dev/null; then
    VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  else
    die "curl or wget is required"
  fi
  [[ -n "$VERSION" ]] || die "Could not determine latest release version"
fi

# strip leading 'v' for archive name component
VER="${VERSION#v}"

info "Version : $VERSION"
info "OS/Arch : $OS/$ARCH"
info "Dest    : $INSTALL_DIR"

# ── download + install one binary ───────────────────────────────────────────
install_binary() {
  local name="$1"
  local archive="${name}_${VER}_${OS}_${ARCH}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"
  local checksum_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  info "Downloading $name $VERSION..."
  if command -v curl &>/dev/null; then
    run curl -fsSL --output "${tmpdir}/${archive}" "$url"
    run curl -fsSL --output "${tmpdir}/checksums.txt" "$checksum_url"
  else
    run wget -qO "${tmpdir}/${archive}" "$url"
    run wget -qO "${tmpdir}/checksums.txt" "$checksum_url"
  fi

  # checksum validation
  if ! $DRY_RUN; then
    info "Validating checksum..."
    local expected
    expected=$(grep "${archive}" "${tmpdir}/checksums.txt" | awk '{print $1}')
    if [[ -z "$expected" ]]; then
      warn "Checksum entry not found for ${archive}; skipping validation"
    else
      local actual
      if command -v sha256sum &>/dev/null; then
        actual=$(sha256sum "${tmpdir}/${archive}" | awk '{print $1}')
      elif command -v shasum &>/dev/null; then
        actual=$(shasum -a 256 "${tmpdir}/${archive}" | awk '{print $1}')
      else
        warn "sha256sum/shasum not available; skipping checksum validation"
        actual="$expected"
      fi
      [[ "$actual" == "$expected" ]] || die "Checksum mismatch for ${archive}"
      ok "Checksum verified"
    fi
  fi

  # extract
  if ! $DRY_RUN; then
    tar -xzf "${tmpdir}/${archive}" -C "$tmpdir"
  fi

  # install
  run mkdir -p "$INSTALL_DIR"
  run install -m 755 "${tmpdir}/${name}" "${INSTALL_DIR}/${name}"
  ok "Installed ${INSTALL_DIR}/${name}"

  trap - EXIT
  rm -rf "$tmpdir"
}

# ── install requested binary/binaries ───────────────────────────────────────
case "$BINARY" in
  all)
    install_binary "ctxt"
    install_binary "dpkms"
    ;;
  ctxt|dpkms)
    install_binary "$BINARY"
    ;;
  *)
    die "Unknown binary: $BINARY (choose: ctxt | dpkms | all)"
    ;;
esac

# ── PATH hint ───────────────────────────────────────────────────────────────
if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  warn "${INSTALL_DIR} is not in your PATH."
  warn "Add the following to your shell profile:"
  warn "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

# ── service setup (dpkms serve) ─────────────────────────────────────────────
if ! $NO_SERVICE && [[ "$BINARY" == "all" || "$BINARY" == "dpkms" ]]; then
  info "Setting up ${OS} service..."
  case "$OS" in
    darwin)
      PLIST_DIR="${HOME}/Library/LaunchAgents"
      PLIST="${PLIST_DIR}/io.contexthelp.dpkms.plist"
      if ! $DRY_RUN; then
        run mkdir -p "$PLIST_DIR"
        cat > "$PLIST" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>            <string>io.contexthelp.dpkms</string>
  <key>ProgramArguments</key>
  <array>
    <string>${INSTALL_DIR}/dpkms</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>        <true/>
  <key>KeepAlive</key>        <true/>
  <key>StandardOutPath</key>  <string>${HOME}/.local/share/contexthelp/dpkms.log</string>
  <key>StandardErrorPath</key><string>${HOME}/.local/share/contexthelp/dpkms.log</string>
</dict>
</plist>
PLIST_EOF
        launchctl load -w "$PLIST" 2>/dev/null || true
        ok "launchd service loaded: io.contexthelp.dpkms"
      else
        echo "  [dry-run] would write $PLIST and launchctl load"
      fi
      ;;
    linux)
      UNIT_DIR="${HOME}/.config/systemd/user"
      UNIT="${UNIT_DIR}/dpkms.service"
      if ! $DRY_RUN; then
        run mkdir -p "$UNIT_DIR"
        cat > "$UNIT" <<UNIT_EOF
[Unit]
Description=dPKMS knowledge substrate
After=network.target

[Service]
ExecStart=${INSTALL_DIR}/dpkms serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
UNIT_EOF
        if command -v systemctl &>/dev/null; then
          systemctl --user daemon-reload
          systemctl --user enable --now dpkms.service 2>/dev/null || true
          ok "systemd user service enabled: dpkms.service"
        else
          warn "systemctl not found; unit written to ${UNIT} but not started"
        fi
      else
        echo "  [dry-run] would write $UNIT and systemctl --user enable --now dpkms"
      fi
      ;;
  esac
fi

# ── first-run init hint ──────────────────────────────────────────────────────
if [[ "$BINARY" == "all" || "$BINARY" == "dpkms" ]]; then
  info "Run 'dpkms serve' to start the server (migrations run automatically on first launch)."
fi

ok "Installation complete."
