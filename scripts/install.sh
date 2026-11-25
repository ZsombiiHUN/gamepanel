#!/usr/bin/env bash
# GamePanel installer for Linux with systemd.
#
#   Panel:   curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh \
#              | sudo bash -s -- panel --domain panel.example.com --email you@example.com
#   Node:    curl -fsSL .../install.sh | sudo bash -s -- node --panel panel.example.com --token <node token>
#   Upgrade: curl -fsSL .../install.sh | sudo bash -s -- upgrade
#
# Run `install.sh help` for every option. The script downloads a release from
# GitHub, checks it against the release's checksums.txt, and installs the
# same files and systemd units the manual guides describe (docs/panel/installing.md,
# docs/nodes/installing.md). Running it again is safe: it never overwrites an
# existing env file or database.
set -euo pipefail

REPO=ZsombiiHUN/gamepanel
# A mirror of the release files (<url>/<tag>/<file>), e.g. for offline installs.
RELEASE_URL=${GAMEPANEL_RELEASE_URL:-https://github.com/$REPO/releases/download}
VERSION=latest
DOMAIN=""
EMAIL=""
PANEL=""
TOKEN=""
ADOPT=""
CADDY=auto
DOCKER=auto
YES=0

ETC=/etc/gamepanel
PANEL_HOME=/var/lib/gamepanel
DAEMON_DATA=/var/lib/gamepanel-daemon
BACKUPS=/var/backups/gamepanel
BIN=/usr/local/bin
TMP=""
trap '[ -n "$TMP" ] && rm -rf "$TMP"' EXIT

say() { printf '\033[1m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
die() {
	printf '\033[31merror:\033[0m %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: install.sh <command> [options]

Commands:
  panel      install the panel (and Caddy for HTTPS)
  node       install the daemon on a machine that will run game servers
  upgrade    upgrade whatever is installed here (backs up the panel first)
  download   only download and verify both archives into the current folder
  help       show this

Options:
  --version <tag>     release to install, e.g. v0.1.0 (default: the newest)
  --domain <name>     panel: the panel's domain; its DNS must point here
  --email <address>   panel: create the first admin with this email
  --no-caddy          panel: do not install Caddy; bring your own HTTPS proxy
  --panel <domain>    node: the panel's domain (or a full wss:// URL)
  --token <token>     node: the token from Nodes -> Create node
  --no-docker         node: do not install Docker if it is missing
  --adopt-wings <dir> node: take over the server files Pterodactyl's Wings kept
                      in <dir> (e.g. /var/lib/pterodactyl/volumes) before the
                      daemon first starts; see docs/guides/from-pterodactyl.md
  -y, --yes           do not ask before installing Docker or Caddy
EOF
}

need_root() { [ "$(id -u)" -eq 0 ] || die "run this as root (sudo bash -s -- ...)"; }

need_systemd() {
	[ -d /run/systemd/system ] || die "this installer needs systemd; see docs/panel/docker.md for Docker Compose instead"
}

confirm() {
	[ "$YES" -eq 1 ] && return 0
	# Piped from curl, stdin is the script itself; ask on the terminal.
	[ -r /dev/tty ] || die "$1 Re-run with --yes to agree, or install it yourself first."
	local reply
	printf '%s [y/N] ' "$1" >/dev/tty
	read -r reply </dev/tty
	[[ $reply =~ ^[Yy] ]]
}

arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported CPU type $(uname -m); releases are built for amd64 and arm64" ;;
	esac
}

resolve_version() {
	[ "$VERSION" != latest ] && return 0
	[ -z "${GAMEPANEL_RELEASE_URL:-}" ] || die "--version is required with GAMEPANEL_RELEASE_URL"
	# Pre-releases count: until v1.0 every release is one.
	VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=1" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
	[ -n "$VERSION" ] || die "could not find a release of $REPO"
}

# fetch <panel|daemon> <dir>: downloads and verifies the archive, unpacks it into dir.
fetch() {
	local part=$1 dir=$2 a file base
	a=$(arch)
	file="gamepanel-${part}_${VERSION#v}_linux_${a}.tar.gz"
	base="$RELEASE_URL/$VERSION"
	say "Downloading $part $VERSION ($a)"
	curl -fsSL -o "$dir/$file" "$base/$file" || die "download failed: $base/$file"
	curl -fsSL -o "$dir/checksums.txt" "$base/checksums.txt" || die "download failed: $base/checksums.txt"
	(cd "$dir" && grep " $file\$" checksums.txt | sha256sum -c --quiet -) ||
		die "checksum mismatch for $file; not installing"
	tar -xzf "$dir/$file" -C "$dir"
}

installed_version() { [ -x "$1" ] && "$1" version 2>/dev/null | awk '{print $2}' || true; }

# ---------------------------------------------------------------- panel

install_caddy() {
	command -v caddy >/dev/null && return 0
	command -v apt-get >/dev/null || {
		warn "Caddy is not installed and this is not a Debian/Ubuntu system; install Caddy or another HTTPS proxy yourself (docs/panel/reverse-proxy.md)"
		return 1
	}
	confirm "Install Caddy from its official apt repository for HTTPS?" || return 1
	say "Installing Caddy"
	apt-get update -qq
	apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl gnupg >/dev/null
	curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key |
		gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
	curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt >/etc/apt/sources.list.d/caddy-stable.list
	apt-get update -qq
	apt-get install -y -qq caddy >/dev/null
}

configure_caddy() {
	local file=/etc/caddy/Caddyfile
	if [ -f "$file" ] && grep -q "reverse_proxy 127.0.0.1:8080" "$file"; then
		return 0
	fi
	if [ -f "$file" ] && grep -qv '^\s*\(#.*\)\?$' "$file" && ! grep -q '^:80' "$file"; then
		warn "$file already has sites; add this yourself:"
		printf '\n%s {\n\treverse_proxy 127.0.0.1:8080\n}\n\n' "$DOMAIN" >&2
		return 0
	fi
	say "Configuring Caddy for https://$DOMAIN"
	printf '%s {\n\treverse_proxy 127.0.0.1:8080\n}\n' "$DOMAIN" >"$file"
	systemctl reload-or-restart caddy
}

cmd_panel() {
	need_root
	need_systemd
	[ -n "$DOMAIN" ] || die "--domain is required (the panel's domain, e.g. panel.example.com)"
	resolve_version
	TMP=$(mktemp -d)
	local tmp=$TMP
	fetch panel "$tmp"

	say "Installing $BIN/panel"
	install -m 0755 "$tmp/panel" "$BIN/panel"
	id gamepanel >/dev/null 2>&1 ||
		useradd --system --home "$PANEL_HOME" --shell /usr/sbin/nologin gamepanel
	install -d -o gamepanel -g gamepanel -m 0700 "$PANEL_HOME"
	install -d -o root -g gamepanel -m 0750 "$ETC"

	if [ ! -f "$ETC/panel.env" ]; then
		say "Writing $ETC/panel.env"
		cat >"$ETC/panel.env" <<EOF
# GamePanel panel settings; every option is in docs/panel/configuration.md.
GAMEPANEL_ADDR=127.0.0.1:8080
GAMEPANEL_DB=$PANEL_HOME/panel.db
GAMEPANEL_PUBLIC_URL=https://$DOMAIN
GAMEPANEL_TRUSTED_PROXIES=127.0.0.1,::1
GAMEPANEL_REQUIRE_MFA=admins
EOF
		chown root:gamepanel "$ETC/panel.env"
		chmod 0640 "$ETC/panel.env"
	else
		say "Keeping the existing $ETC/panel.env"
	fi

	install -m 0644 "$tmp/packaging/systemd/gamepanel.service" /etc/systemd/system/gamepanel.service
	systemctl daemon-reload

	if [ -n "$EMAIL" ] && [ ! -f "$PANEL_HOME/panel.db" ]; then
		say "Creating the first admin, $EMAIL"
		runuser -u gamepanel -- sh -c "set -a; . $ETC/panel.env; exec $BIN/panel create-admin '$EMAIL'"
	fi
	systemctl enable --now gamepanel
	systemctl restart gamepanel

	if [ "$CADDY" != no ] && install_caddy; then
		configure_caddy
	else
		warn "no HTTPS proxy set up: put one in front of 127.0.0.1:8080 (docs/panel/reverse-proxy.md)"
	fi

	say "Done: GamePanel $VERSION runs at https://$DOMAIN"
	if [ -z "$EMAIL" ]; then
		echo "    Create an admin: sudo runuser -u gamepanel -- sh -c 'set -a; . $ETC/panel.env; exec panel create-admin you@example.com'"
	fi
	echo "    Back up $PANEL_HOME/panel.db and panel.db.key (docs/panel/backing-up.md)."
	echo "    Add a node: Nodes -> Create node, then run this script with 'node' on that machine."
}

# ---------------------------------------------------------------- node

install_docker() {
	command -v docker >/dev/null && return 0
	[ "$DOCKER" = no ] && die "Docker is not installed (and --no-docker was given)"
	confirm "Docker is not installed. Install it with Docker's script (get.docker.com)?" ||
		die "the daemon needs Docker"
	say "Installing Docker"
	# get.docker.com, pinned to a docker/docker-install commit and checked
	# against its SHA-256 before it runs. The script still installs the newest
	# Docker Engine from Docker's own package repository.
	local commit=8ad27bd3ac871feabe62f1d926c4067800c1732f
	local sum=77f623a9f23857ebc45a50e37696fc406e3a60bda4ef17399444f792ecf8895d
	local script
	script=$(mktemp)
	curl -fsSL "https://raw.githubusercontent.com/docker/docker-install/$commit/install.sh" -o "$script"
	echo "$sum  $script" | sha256sum -c --quiet - || {
		rm -f "$script"
		die "Docker's install script did not match its checksum"
	}
	sh "$script" || {
		rm -f "$script"
		die "installing Docker failed"
	}
	rm -f "$script"
	systemctl enable --now docker
}

cmd_node() {
	need_root
	need_systemd
	[ -n "$PANEL" ] || die "--panel is required (the panel's domain)"
	[ -n "$TOKEN" ] || die "--token is required (Nodes -> Create node in the panel)"
	local url=$PANEL
	case "$url" in
	ws://* | wss://*) ;;
	*) url="wss://$url/daemon/connect" ;;
	esac
	case "$url" in
	ws://127.0.0.1* | ws://localhost* | wss://*) ;;
	*) warn "$url is not encrypted: the node token crosses the network in the clear" ;;
	esac

	install_docker
	resolve_version
	TMP=$(mktemp -d)
	local tmp=$TMP
	fetch daemon "$tmp"

	say "Installing $BIN/gamepanel-daemon"
	install -m 0755 "$tmp/daemon" "$BIN/gamepanel-daemon"
	install -d -m 0700 "$ETC" "$DAEMON_DATA"
	if [ ! -f "$ETC/daemon.env" ]; then
		say "Writing $ETC/daemon.env"
		umask 077
		cat >"$ETC/daemon.env" <<EOF
# GamePanel daemon settings; every option is in docs/nodes/configuration.md.
GAMEPANEL_NODE_TOKEN=$TOKEN
GAMEPANEL_PANEL_URL=$url
GAMEPANEL_DATA_DIR=$DAEMON_DATA
GAMEPANEL_SFTP_ADDR=0.0.0.0:2022
EOF
	else
		say "Keeping the existing $ETC/daemon.env"
	fi
	if modprobe ifb 2>/dev/null; then
		echo ifb >/etc/modules-load.d/ifb.conf
	else
		warn "no ifb kernel module: incoming bandwidth limits fall back to a policer"
	fi
	for t in tc ip nsenter; do
		command -v "$t" >/dev/null || warn "$t is missing: network limits are off (install iproute2 and util-linux)"
	done

	if [ -n "$ADOPT" ]; then
		# Before the daemon's first start: once it connects, the panel creates
		# the imported servers here, and any whose files are not here yet would
		# be installed from scratch.
		if systemctl is-active --quiet wings 2>/dev/null; then
			die "Wings is still running; stop it first: systemctl disable --now wings"
		fi
		say "Taking over Wings' servers from $ADOPT"
		# shellcheck source=/dev/null
		(set -a && . "$ETC/daemon.env" && "$BIN/gamepanel-daemon" adopt-wings --from "$ADOPT") ||
			die "adopt-wings failed; the daemon was not started. Fix the problem above and run this again"
	fi

	install -m 0644 "$tmp/packaging/systemd/gamepanel-daemon.service" /etc/systemd/system/gamepanel-daemon.service
	systemctl daemon-reload
	systemctl enable --now gamepanel-daemon
	systemctl restart gamepanel-daemon

	say "Done: the node should show as online in the panel within seconds"
	echo "    Logs: journalctl -u gamepanel-daemon -f"
	echo "    Open the node's game port range and 2022/tcp (SFTP) in your firewall."
}

# ---------------------------------------------------------------- upgrade

cmd_upgrade() {
	need_root
	need_systemd
	local has_panel=0 has_daemon=0
	[ -x "$BIN/panel" ] && has_panel=1
	[ -x "$BIN/gamepanel-daemon" ] && has_daemon=1
	[ $has_panel -eq 1 ] || [ $has_daemon -eq 1 ] || die "nothing to upgrade: neither $BIN/panel nor $BIN/gamepanel-daemon is installed"
	resolve_version
	TMP=$(mktemp -d)
	local tmp=$TMP

	if [ $has_panel -eq 1 ]; then
		local from
		from=$(installed_version "$BIN/panel")
		if [ "$from" = "$VERSION" ]; then
			say "The panel already runs $VERSION"
		else
			fetch panel "$tmp"
			# Stopped, the database is consistent on disk and safe to copy.
			local dest
			dest="$BACKUPS/pre-$VERSION-$(date +%Y%m%d-%H%M%S)"
			say "Backing up the panel database to $dest"
			systemctl stop gamepanel
			install -d -m 0700 "$dest"
			cp -a "$PANEL_HOME"/panel.db* "$dest"/
			cp -a "$BIN/panel" "$dest/panel-$from"
			install -m 0755 "$tmp/panel" "$BIN/panel"
			install -m 0644 "$tmp/packaging/systemd/gamepanel.service" /etc/systemd/system/gamepanel.service
			systemctl daemon-reload
			systemctl start gamepanel
			say "Panel upgraded from ${from:-unknown} to $VERSION (migrations run at start)"
			echo "    To go back: stop it, put $dest/panel-$from back, restore the database from $dest."
		fi
	fi
	if [ $has_daemon -eq 1 ]; then
		local from
		from=$(installed_version "$BIN/gamepanel-daemon")
		if [ "$from" = "$VERSION" ]; then
			say "The daemon already runs $VERSION"
		else
			fetch daemon "$tmp"
			install -m 0755 "$tmp/daemon" "$BIN/gamepanel-daemon"
			install -m 0644 "$tmp/packaging/systemd/gamepanel-daemon.service" /etc/systemd/system/gamepanel-daemon.service
			systemctl daemon-reload
			systemctl restart gamepanel-daemon
			say "Daemon upgraded from ${from:-unknown} to $VERSION (game servers kept running)"
		fi
	fi
}

# ---------------------------------------------------------------- main

[ $# -gt 0 ] || {
	usage
	exit 1
}
cmd=$1
shift
while [ $# -gt 0 ]; do
	case "$1" in
	--version) VERSION=${2:?--version needs a tag} && shift ;;
	--domain) DOMAIN=${2:?--domain needs a name} && shift ;;
	--email) EMAIL=${2:?--email needs an address} && shift ;;
	--panel) PANEL=${2:?--panel needs a domain} && shift ;;
	--token) TOKEN=${2:?--token needs a value} && shift ;;
	--no-caddy) CADDY=no ;;
	--no-docker) DOCKER=no ;;
	--adopt-wings) ADOPT=${2:?--adopt-wings needs a directory} && shift ;;
	-y | --yes) YES=1 ;;
	-h | --help) cmd=help ;;
	*) die "unknown option $1 (see install.sh help)" ;;
	esac
	shift
done
case "$VERSION" in latest | v*) ;; *) VERSION="v$VERSION" ;; esac
[[ $EMAIL =~ ^[^\'[:space:]]+@[^\'[:space:]]+$ ]] || [ -z "$EMAIL" ] || die "--email does not look like an address"

case "$cmd" in
panel) cmd_panel ;;
node) cmd_node ;;
upgrade) cmd_upgrade ;;
download)
	resolve_version
	fetch panel "$PWD"
	fetch daemon "$PWD"
	say "Verified and unpacked into $PWD"
	;;
help | -h | --help) usage ;;
*) die "unknown command $cmd (see install.sh help)" ;;
esac
