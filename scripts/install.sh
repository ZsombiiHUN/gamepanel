#!/usr/bin/env bash
# GamePanel installer for Linux with systemd.
#
#   Guided:  curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh | sudo bash
#
#   Panel:   curl -fsSL .../install.sh \
#              | sudo bash -s -- panel --domain panel.example.com --email you@example.com
#   Node:    curl -fsSL .../install.sh | sudo bash -s -- node --panel panel.example.com --token <node token>
#   Upgrade: curl -fsSL .../install.sh | sudo bash -s -- upgrade
#
# With no command it asks what to install. With a command it asks only for
# what the options leave out; add --yes (or --accept-terms) to run without
# any questions, e.g. from cloud-init. Run `install.sh help` for every option.
#
# The script downloads a release from GitHub, checks it against the release's
# checksums.txt, and installs the same files and systemd units the manual
# guides describe (docs/panel/installing.md, docs/nodes/installing.md).
# Running it again is safe: it never overwrites an existing env file or
# database.
set -Eeuo pipefail

REPO=ZsombiiHUN/gamepanel
# A mirror of the release files (<url>/<tag>/<file>), e.g. for offline installs.
RELEASE_URL=${GAMEPANEL_RELEASE_URL:-https://github.com/$REPO/releases/download}
LICENSE_URL=https://github.com/$REPO/blob/main/LICENSE
LE_TERMS_URL=https://letsencrypt.org/repository/
VERSION=latest
DOMAIN=""
EMAIL=""
PANEL=""
TOKEN=""
ADOPT=""
CADDY=auto
DOCKER=auto
YES=0
ACCEPT=0

ETC=/etc/gamepanel
PANEL_HOME=/var/lib/gamepanel
DAEMON_DATA=/var/lib/gamepanel-daemon
BACKUPS=/var/backups/gamepanel
BIN=/usr/local/bin
WINGS_VOLUMES=/var/lib/pterodactyl/volumes
TMP=""
LOG=/dev/null
RUN_PID=""
INTERACTIVE=0
TERMS_DONE=0

# ---------------------------------------------------------------- terminal
#
# Colour and animation only when stdout is a terminal, never with NO_COLOR
# (no-color.org) or TERM=dumb. Questions read /dev/tty, because when the
# script is piped from curl its stdin is the script itself. On a terminal,
# each step shows a spinner and a one-line result while its output goes to a
# log, whose last lines are shown if the step fails; elsewhere (CI, cloud-init,
# redirected output) everything streams as plain text.

PRETTY=0
[ -t 1 ] && [ "${TERM:-dumb}" != dumb ] && PRETTY=1
ESC=$'\033'
if [ "$PRETTY" -eq 1 ] && [ -z "${NO_COLOR:-}" ]; then
	BOLD="${ESC}[1m" DIM="${ESC}[2m" RST="${ESC}[0m"
	RED="${ESC}[31m" GRN="${ESC}[32m" YLW="${ESC}[33m" CYN="${ESC}[36m"
	ACC="${ESC}[38;5;78m" # the panel's "good" green, #5cc98a
else
	BOLD="" DIM="" RST="" RED="" GRN="" YLW="" CYN="" ACC=""
fi
case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
*UTF-8* | *utf-8* | *UTF8* | *utf8*)
	OK="✓" BAD="✗" WRN="!" DOT="•" ARROW="›" PICK="❯" LINE="─"
	SPIN=(⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏)
	UTF=1
	;;
*)
	OK="+" BAD="x" WRN="!" DOT="*" ARROW=">" PICK=">" LINE="-"
	SPIN=(- "\\" "|" /)
	UTF=0
	;;
esac

say() { printf '%s==>%s %s\n' "$BOLD" "$RST" "$*"; }
warn() {
	if [ "$PRETTY" -eq 1 ]; then
		printf '  %s%s %s%s\n' "$YLW" "$WRN" "$*" "$RST" >&2
	else
		printf 'warning: %s\n' "$*" >&2
	fi
	# Inside a step stderr goes to the log; keep the warning to show the user.
	if [ "$PRETTY" -eq 1 ] && [ -n "$TMP" ]; then printf '%s\n' "$*" >>"$TMP/warnings"; fi
}
die() {
	printf '%serror:%s %s\n' "$RED" "$RST" "$*" >&2
	exit 1
}
# out prints to the terminal the questions come from.
# shellcheck disable=SC2059 # out takes a format, like printf
out() { printf "$@" >/dev/tty; }
rule() {
	local i bar=""
	for ((i = 0; i < ${2:-52}; i++)); do bar+="$LINE"; done
	printf '\n  %s%s%s %s%s%s\n' "$BOLD" "$1" "$RST" "$DIM" "$bar" "$RST"
}
check_line() { # check_line ok|warn|bad <text> [detail]
	local sym=$OK col=$GRN
	case $1 in warn) sym=$WRN col=$YLW ;; bad) sym=$BAD col=$RED ;; esac
	printf '  %s%s%s %s%s%s\n' "$col" "$sym" "$RST" "$2" "${3:+  $DIM}" "${3:+$3$RST}"
}
show_cursor() { [ "$PRETTY" -eq 1 ] && printf '%s[?25h' "$ESC" >/dev/tty 2>/dev/null || true; }
hide_cursor() { [ "$PRETTY" -eq 1 ] && printf '%s[?25l' "$ESC" >/dev/tty 2>/dev/null || true; }

on_exit() {
	show_cursor
	[ -n "$TMP" ] && rm -rf "$TMP"
	return 0
}
on_interrupt() {
	if [ -n "$RUN_PID" ]; then
		pkill -TERM -P "$RUN_PID" 2>/dev/null || true
		kill "$RUN_PID" 2>/dev/null || true
	fi
	show_cursor
	printf '\n\n  %sCancelled.%s Nothing after the last %s%s%s step was done.\n\n' "$RED" "$RST" "$GRN" "$OK" "$RST" >&2
	exit 130
}
# A command that fails outside a step stops the script (set -e); say where.
on_error() {
	show_cursor
	printf '\n%serror:%s the installer stopped unexpectedly at line %s (exit %s); nothing after the last finished step was done. Please report this.\n' "$RED" "$RST" "$1" "$2" >&2
}
trap on_exit EXIT
trap on_interrupt INT TERM
trap 'on_error $LINENO $?' ERR

banner() {
	[ "$PRETTY" -eq 1 ] || {
		say "GamePanel installer"
		return 0
	}
	local -a art
	if [ "$UTF" -eq 1 ]; then
		art=('█▀▀ ▄▀█ █▀▄▀█ █▀▀ █▀█ ▄▀█ █▄ █ █▀▀ █  '
			'█▄█ █▀█ █ ▀ █ ██▄ █▀▀ █▀█ █ ▀█ ██▄ █▄▄')
	else
		art=('GamePanel')
	fi
	printf '\n'
	local row i n c
	for row in "${art[@]}"; do
		printf '  '
		n=${#row}
		for ((i = 0; i < n; i++)); do
			c=${row:i:1}
			if [ -z "$ACC" ] || [ "$c" = " " ]; then
				printf '%s' "$c"
			else
				# A green-to-teal sweep across the word.
				printf '%s[38;5;%dm%s' "$ESC" "$((i < n / 3 ? 78 : i < 2 * n / 3 ? 79 : 80))" "$c"
			fi
		done
		printf '%s\n' "$RST"
	done
	printf '  %sSelf-hosted game servers, one panel.%s\n' "$DIM" "$RST"
}

# run <label> <command...>: runs one install step. On a terminal it shows a
# spinner, then the step's result; the command's output goes to the log.
run() {
	local label=$1 rc=0 start=$SECONDS i=0 w from
	shift
	if [ "$PRETTY" -eq 0 ]; then
		say "$label"
		"$@"
		return
	fi
	printf '\n===== %s: %s\n' "$(date -u +%FT%TZ)" "$label" >>"$LOG"
	from=$(($(wc -l <"$LOG") + 1))
	# In the background so that set -e still applies inside the step (it is
	# ignored in a subshell tested with ||), and so the spinner can turn. A
	# failing command explains itself; the step's own log is shown below.
	(
		trap - ERR
		"$@"
	) >>"$LOG" 2>&1 </dev/null &
	RUN_PID=$!
	hide_cursor
	while kill -0 "$RUN_PID" 2>/dev/null; do
		printf '\r  %s%s%s %s' "$CYN" "${SPIN[i++ % ${#SPIN[@]}]}" "$RST" "$label"
		sleep 0.1
	done
	wait "$RUN_PID" || rc=$?
	RUN_PID=""
	show_cursor
	printf '\r%s[2K' "$ESC"
	if [ "$rc" -ne 0 ]; then
		printf '  %s%s%s %s\n\n' "$RED" "$BAD" "$RST" "$label"
		tail -n +"$from" "$LOG" | tail -n 15 | sed "s/^/    $DIM/; s/\$/$RST/"
		printf '\n'
		die "that step failed. The full log is $LOG"
	fi
	printf '  %s%s%s %s %s%ss%s\n' "$GRN" "$OK" "$RST" "$label" "$DIM" "$((SECONDS - start))" "$RST"
	if [ -s "$TMP/warnings" ]; then
		while IFS= read -r w; do printf '    %s%s %s%s\n' "$YLW" "$WRN" "$w" "$RST"; done <"$TMP/warnings"
		: >"$TMP/warnings"
	fi
}

# ---------------------------------------------------------------- questions

# answered <question> <answer>: replaces the prompt line with its answer.
answered() {
	if [ "$PRETTY" -eq 1 ]; then out '%s[1A%s[2K' "$ESC" "$ESC"; fi
	out '  %s%s%s %s  %s%s%s\n' "$GRN" "$OK" "$RST" "$1" "$ACC" "$2" "$RST"
}

# ask <var> <question> [default] [check] [secret]: asks until check accepts.
# check is a function that prints why an answer is wrong and returns 1.
ask() {
	local __var=$1 q=$2 def=${3:-} check=${4:-} secret=${5:-} reply why
	while :; do
		out '  %s?%s %s%s%s ' "$CYN" "$RST" "$BOLD" "$q" "$RST"
		[ -n "$def" ] && out '%s(%s)%s ' "$DIM" "$def" "$RST"
		if [ -n "$secret" ]; then
			IFS= read -rs reply </dev/tty || die "no answer"
			out '\n'
		else
			IFS= read -r reply </dev/tty || die "no answer"
		fi
		reply=${reply:-$def}
		reply=${reply#"${reply%%[![:space:]]*}"} # trim
		reply=${reply%"${reply##*[![:space:]]}"}
		if [ -z "$reply" ]; then
			out '    %san answer is needed%s\n' "$RED" "$RST"
			continue
		fi
		if [ -n "$check" ] && ! why=$("$check" "$reply"); then
			out '    %s%s%s\n' "$RED" "$why" "$RST"
			continue
		fi
		break
	done
	if [ -n "$secret" ]; then answered "$q" "received"; else answered "$q" "$reply"; fi
	printf -v "$__var" '%s' "$reply"
}

# ask_yn <question> y|n: the default answers Enter.
ask_yn() {
	local q=$1 def=${2:-n} reply hint="y/N"
	[ "$def" = y ] && hint="Y/n"
	while :; do
		out '  %s?%s %s%s%s %s(%s)%s ' "$CYN" "$RST" "$BOLD" "$q" "$RST" "$DIM" "$hint" "$RST"
		IFS= read -r reply </dev/tty || die "no answer"
		reply=${reply:-$def}
		case $reply in
		[Yy] | [Yy][Ee][Ss])
			answered "$q" yes
			return 0
			;;
		[Nn] | [Nn][Oo])
			answered "$q" no
			return 1
			;;
		esac
	done
}

# choose <var> <question> <value|label|hint>...: a menu, arrow keys or numbers.
choose() {
	local __var=$1 q=$2 sel=0 n i key rest v l h
	shift 2
	n=$#
	local -a vals=() labels=() hints=()
	for i in "$@"; do
		IFS='|' read -r v l h <<<"$i"
		vals+=("$v") labels+=("$l") hints+=("$h")
	done
	out '  %s?%s %s%s%s %s(%s arrows or a number, Enter to pick)%s\n' "$CYN" "$RST" "$BOLD" "$q" "$RST" "$DIM" "$( ((UTF)) && echo '↑↓' || echo up/down)" "$RST"
	hide_cursor
	while :; do
		for ((i = 0; i < n; i++)); do
			if [ "$i" -eq "$sel" ]; then
				out '%s[2K    %s%s %d. %s%s  %s%s%s\n' "$ESC" "$ACC$BOLD" "$PICK" $((i + 1)) "${labels[i]}" "$RST" "$DIM" "${hints[i]}" "$RST"
			else
				out '%s[2K      %d. %s\n' "$ESC" $((i + 1)) "${labels[i]}"
			fi
		done
		IFS= read -rsn1 key </dev/tty || die "no answer"
		case $key in
		"") break ;;
		"$ESC")
			IFS= read -rsn2 -t 0.05 rest </dev/tty || rest=""
			case $rest in
			"[A") sel=$(((sel + n - 1) % n)) ;;
			"[B") sel=$(((sel + 1) % n)) ;;
			esac
			;;
		k) sel=$(((sel + n - 1) % n)) ;;
		j) sel=$(((sel + 1) % n)) ;;
		[1-9]) [ "$key" -le "$n" ] && sel=$((key - 1)) ;;
		esac
		out '%s[%dA' "$ESC" "$n"
	done
	show_cursor
	# Fold the menu away into one answered line.
	out '%s[%dA' "$ESC" $((n + 1))
	for ((i = 0; i <= n; i++)); do out '%s[2K\n' "$ESC"; done
	out '%s[%dA' "$ESC" $((n + 1))
	out '\n'
	answered "$q" "${labels[sel]}"
	printf -v "$__var" '%s' "${vals[sel]}"
}

check_domain() {
	case $1 in
	http://* | https://*)
		echo "only the name, without http(s)://"
		return 1
		;;
	esac
	[[ $1 =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$ ]] || {
		echo "that is not a domain name, e.g. panel.example.com"
		return 1
	}
}
check_email() {
	[[ $1 =~ ^[^\'[:space:]@]+@[^\'[:space:]@]+\.[^\'[:space:]@]+$ ]] || {
		echo "that does not look like an email address"
		return 1
	}
}
check_panel() {
	case $1 in ws://* | wss://*) return 0 ;; esac
	check_domain "$1"
}
check_token() {
	[[ $1 =~ ^[^[:space:]\']+$ ]] && [ ${#1} -ge 16 ] || {
		echo "paste the whole token from Nodes -> Create node"
		return 1
	}
}

# ---------------------------------------------------------------- terms

terms_text() { # terms_text <panel|node|both>
	local role=${TERMS_ROLE:-$1}
	cat <<EOF
GamePanel is free software under the GNU Affero General Public License v3.
It comes WITHOUT ANY WARRANTY. If you change it and let other people use it
over a network, you must offer them your changed source code.
The licence: $LICENSE_URL

You run this installation. You are responsible for the game servers on it,
for the people you give accounts to and their data, and for following each
game's own licence (Minecraft servers, for example, need Mojang's EULA,
which GamePanel asks you to accept for each server).
EOF
	if [ "$role" != node ] && [ "$CADDY" != no ]; then
		cat <<EOF

The panel's HTTPS certificate comes from a public certificate authority,
normally Let's Encrypt, through Caddy. Continuing means you accept the
Let's Encrypt Subscriber Agreement: $LE_TERMS_URL
EOF
	fi
	if [ "$role" = all ] || { [ "$role" != panel ] && ! command -v docker >/dev/null && [ "$DOCKER" != no ]; }; then
		cat <<EOF

Docker Engine is installed from Docker's own package repository, under its
own licence (Apache 2.0).
EOF
	fi
}

# terms <role>: the terms must be accepted once, on the terminal or with a flag.
terms() {
	[ "$TERMS_DONE" -eq 1 ] && return 0
	if [ "$ACCEPT" -eq 0 ]; then
		[ "$INTERACTIVE" -eq 1 ] ||
			die "read the terms (install.sh terms) and add --accept-terms (or --yes) to accept them"
		rule "Before you install"
		printf '\n'
		terms_text "$1" | sed "s/^/    /"
		printf '\n'
		ask_yn "Do you accept these terms?" n || die "the terms were not accepted; nothing was installed"
	fi
	TERMS_DONE=1
}

record_terms() { # record_terms <role>: a dated note of what was accepted
	{
		printf 'Accepted by %s on %s with install.sh for GamePanel %s.\n\n' \
			"$([ "$INTERACTIVE" -eq 1 ] && echo "answering yes" || echo "--accept-terms/--yes")" "$(date -u +%FT%TZ)" "$VERSION"
		terms_text "$1"
	} >"$ETC/terms-accepted.txt"
	chmod 0644 "$ETC/terms-accepted.txt"
}

# ---------------------------------------------------------------- checks

need_root() { [ "$(id -u)" -eq 0 ] || die "run this as root (curl ... | sudo bash)"; }

need_systemd() {
	[ -d /run/systemd/system ] || die "this installer needs systemd; see docs/panel/docker.md for Docker Compose instead"
}

confirm() {
	[ "$YES" -eq 1 ] && return 0
	[ "$INTERACTIVE" -eq 1 ] || die "$1 Re-run with --yes to agree, or install it yourself first."
	ask_yn "$1" y
}

arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported CPU type $(uname -m); releases are built for amd64 and arm64" ;;
	esac
}

# port_user <port>: the program listening on a TCP port, if any.
port_user() {
	command -v ss >/dev/null || return 0
	ss -Hltnp "sport = :$1" 2>/dev/null | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | head -n1 || true
}

# preflight <panel|node|both>: shows what this machine has, warns early.
preflight() {
	local role=$1 os mem free who
	rule "This machine"
	# shellcheck source=/dev/null
	os=$(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-}") || true
	check_line ok "${os:-Linux} $(arch)" "$(uname -r)"
	check_line ok "systemd"
	mem=$(awk '/^MemTotal:/ {print int($2 / 1024)}' /proc/meminfo)
	if [ "$role" != panel ] && [ "$mem" -lt 2000 ]; then
		check_line warn "${mem} MB memory" "game servers usually want 2 GB or more"
	else
		check_line ok "${mem} MB memory"
	fi
	free=$(df -Pm /var/lib | awk 'NR == 2 {print int($4 / 1024)}')
	if [ "$free" -lt 5 ]; then
		check_line warn "${free} GB free in /var/lib" "game servers need room for their files and backups"
	else
		check_line ok "${free} GB free in /var/lib"
	fi
	if [ "$role" != node ]; then
		for p in 80 443; do
			who=$(port_user "$p")
			if [ -n "$who" ] && [ "$who" != caddy ]; then
				check_line warn "port $p is used by $who" "Caddy needs 80 and 443 for HTTPS"
				[ "$CADDY" = auto ] && CADDY=busy
			fi
		done
		who=$(port_user 8080)
		[ -z "$who" ] || [ "$who" = panel ] || check_line bad "port 8080 is used by $who" "the panel listens on 127.0.0.1:8080"
	fi
	if [ "$role" != panel ]; then
		if command -v docker >/dev/null; then
			local dv
			dv=$(docker version --format '{{.Server.Version}}' 2>/dev/null) || dv=""
			if [ -n "$dv" ]; then
				check_line ok "Docker $dv"
			else
				check_line warn "Docker is installed but not running" "the daemon needs it: systemctl start docker"
			fi
		else
			check_line warn "Docker is not installed" "the installer can add it"
		fi
		who=$(port_user 2022)
		[ -z "$who" ] || [ "$who" = gamepanel-daemo ] || check_line warn "port 2022 is used by $who" "the daemon's SFTP server uses it"
	fi
	[ -x "$BIN/panel" ] && check_line ok "GamePanel panel $(installed_version "$BIN/panel") is installed"
	[ -x "$BIN/gamepanel-daemon" ] && check_line ok "GamePanel daemon $(installed_version "$BIN/gamepanel-daemon") is installed"
	return 0
}

# dns_check: tells early whether the domain points here, without asking any
# outside service for this machine's address.
dns_check() {
	local ips mine ip
	ips=$(getent ahosts "$DOMAIN" 2>/dev/null | awk '{print $1}' | sort -u | tr '\n' ' ') || true
	if [ -z "$ips" ]; then
		check_line warn "$DOMAIN does not resolve yet" "add its DNS record; HTTPS starts working once it points here"
		return 0
	fi
	mine=" $(hostname -I 2>/dev/null || true) "
	for ip in $ips; do
		case $mine in *" $ip "*)
			check_line ok "$DOMAIN points to this machine" "$ip"
			return 0
			;;
		esac
	done
	check_line warn "$DOMAIN points to ${ips% }" "not an address of this machine; fine behind NAT, but a proxy such as Cloudflare's orange cloud must forward to it"
}

# ---------------------------------------------------------------- release

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
	echo "Downloading $part $VERSION ($a)"
	curl -fsSL -o "$dir/$file" "$base/$file" || die "download failed: $base/$file"
	curl -fsSL -o "$dir/checksums.txt" "$base/checksums.txt" || die "download failed: $base/checksums.txt"
	(cd "$dir" && grep " $file\$" checksums.txt | sha256sum -c --quiet -) ||
		die "checksum mismatch for $file; not installing"
	tar -xzf "$dir/$file" -C "$dir"
}

installed_version() { [ -x "$1" ] && "$1" version 2>/dev/null | awk '{print $2}' || true; }

# begin: root checks, a private scratch folder and the log.
begin() {
	need_root
	need_systemd
	[ -n "$TMP" ] && return 0
	TMP=$(mktemp -d)
	: >"$TMP/warnings"
	if [ "$PRETTY" -eq 1 ]; then
		LOG=/var/log/gamepanel-install.log
		touch "$LOG" && chmod 0600 "$LOG" || LOG="$TMP/install.log"
		printf '\n===== %s: install.sh %s\n' "$(date -u +%FT%TZ)" "${CMD:-}" >>"$LOG"
	fi
}

# ready <line...>: the plan, then one last chance to stop.
ready() {
	[ "$INTERACTIVE" -eq 1 ] || return 0
	rule "Ready to install"
	local l
	for l in "$@"; do printf '    %s%s%s %s\n' "$DIM" "$DOT" "$RST" "$l"; done
	printf '\n'
	ask_yn "Install now?" y || die "stopped; nothing was installed"
	rule "Installing"
}

# ---------------------------------------------------------------- panel

install_caddy() {
	command -v caddy >/dev/null && return 0
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
		printf '%s {\n\treverse_proxy 127.0.0.1:8080\n}\n' "$DOMAIN" >"$TMP/caddy-snippet"
		warn "$file already has sites; add the GamePanel site to it yourself (shown at the end)"
		return 0
	fi
	echo "Configuring Caddy for https://$DOMAIN"
	printf '%s {\n\treverse_proxy 127.0.0.1:8080\n}\n' "$DOMAIN" >"$file"
	systemctl reload-or-restart caddy
}

# panel_questions: fills in what the options left out, or stops.
panel_questions() {
	if [ "$INTERACTIVE" -eq 0 ]; then
		[ -n "$DOMAIN" ] || die "--domain is required (the panel's domain, e.g. panel.example.com)"
		[ "$CADDY" = busy ] && CADDY=auto
		return 0
	fi
	rule "The panel"
	if [ -z "$DOMAIN" ] && [ -f "$ETC/panel.env" ]; then
		# An existing panel.env is kept, and with it the domain it names.
		DOMAIN=$(sed -n 's|^GAMEPANEL_PUBLIC_URL=https\{0,1\}://\([^/]*\).*|\1|p' "$ETC/panel.env")
		[ -n "$DOMAIN" ] && check_line ok "Panel domain  $DOMAIN" "from the existing $ETC/panel.env"
	fi
	if [ -z "$DOMAIN" ]; then
		out '    %sThe address people open in their browser. Point its DNS A/AAAA record at this server.%s\n' "$DIM" "$RST"
		ask DOMAIN "Panel domain" "" check_domain
	fi
	dns_check
	if [ -f "$PANEL_HOME/panel.db" ]; then
		check_line ok "keeping the existing database and its accounts"
	elif [ -z "$EMAIL" ]; then
		out '    %sThe first admin account; its password is made for you and shown at the end.%s\n' "$DIM" "$RST"
		ask EMAIL "Admin email" "" check_email
	fi
	if [ "$CADDY" = auto ] || [ "$CADDY" = busy ]; then
		if command -v caddy >/dev/null; then
			CADDY=auto
		elif ! command -v apt-get >/dev/null; then
			check_line warn "Caddy can only be installed for you on Debian or Ubuntu" "bring your own HTTPS proxy (docs/panel/reverse-proxy.md)"
			CADDY=no
		else
			local def=y
			[ "$CADDY" = busy ] && def=n
			out '    %sCaddy serves the panel over HTTPS and renews its certificate by itself.%s\n' "$DIM" "$RST"
			if ask_yn "Set up HTTPS with Caddy?" "$def"; then CADDY=yes; else CADDY=no; fi
		fi
	fi
}

panel_files() {
	install -m 0755 "$TMP/panel" "$BIN/panel"
	id gamepanel >/dev/null 2>&1 ||
		useradd --system --home "$PANEL_HOME" --shell /usr/sbin/nologin gamepanel
	install -d -o gamepanel -g gamepanel -m 0700 "$PANEL_HOME"
	install -d -o root -g gamepanel -m 0750 "$ETC"
	if [ ! -f "$ETC/panel.env" ]; then
		echo "Writing $ETC/panel.env"
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
		echo "Keeping the existing $ETC/panel.env"
	fi
	install -m 0644 "$TMP/packaging/systemd/gamepanel.service" /etc/systemd/system/gamepanel.service
	systemctl daemon-reload
}

create_admin() {
	# The password is printed once; keep it in the private scratch folder
	# until the summary, never in the log.
	umask 077
	runuser -u gamepanel -- sh -c "set -a; . $ETC/panel.env; exec $BIN/panel create-admin '$EMAIL'" >"$TMP/admin"
}

start_panel() {
	systemctl enable --now gamepanel
	systemctl restart gamepanel
	local i
	for i in $(seq 1 30); do
		curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:8080/ 2>/dev/null && return 0
		sleep 1
	done
	journalctl -u gamepanel -n 20 --no-pager || true
	die "the panel did not answer on 127.0.0.1:8080 within 30 seconds"
}

cmd_panel() {
	begin
	[ "${WIZARD:-0}" -eq 1 ] || {
		banner
		preflight panel
	}
	panel_questions
	terms panel
	HAD_DB=0
	if [ -f "$PANEL_HOME/panel.db" ]; then
		HAD_DB=1
		EMAIL=""
	fi
	if [ "$INTERACTIVE" -eq 1 ]; then
		run "Checking the release" resolve_version_file
		VERSION=$(cat "$TMP/version")
	else
		resolve_version
	fi
	local -a plan=("GamePanel $VERSION ($(arch)), checked against the release's checksums"
		"$BIN/panel, settings in $ETC/panel.env, data in $PANEL_HOME"
		"systemd service gamepanel, listening on 127.0.0.1:8080")
	[ -n "$EMAIL" ] && plan+=("the first admin, $EMAIL")
	[ "$CADDY" = no ] || plan+=("Caddy for https://$DOMAIN")
	ready "${plan[@]}"

	local started=$SECONDS
	run "Downloading GamePanel $VERSION" fetch panel "$TMP"
	run "Installing the panel" panel_files
	[ -n "$EMAIL" ] && run "Creating the admin account" create_admin
	run "Starting the panel" start_panel
	record_terms panel
	if [ "$CADDY" != no ]; then
		if ! command -v caddy >/dev/null; then
			if ! command -v apt-get >/dev/null; then
				warn "Caddy is not installed and this is not a Debian/Ubuntu system; install Caddy or another HTTPS proxy yourself (docs/panel/reverse-proxy.md)"
				CADDY=no
			elif [ "$CADDY" = yes ] || confirm "Install Caddy from its official apt repository for HTTPS?"; then
				run "Installing Caddy" install_caddy
			else
				CADDY=no
			fi
		fi
		[ "$CADDY" = no ] || run "Setting up HTTPS for $DOMAIN" configure_caddy
	fi
	[ "$CADDY" != no ] || warn "no HTTPS proxy set up: put one in front of 127.0.0.1:8080 (docs/panel/reverse-proxy.md)"
	: >"$TMP/warnings" # warn already printed those outside a step
	panel_done "$started"
}

resolve_version_file() {
	resolve_version
	echo "$VERSION" >"$TMP/version"
}

panel_done() {
	local secs=$((SECONDS - $1)) pw=""
	[ -s "$TMP/admin" ] && pw=$(sed -n 's/^ *password: *//p' "$TMP/admin")
	if [ "$PRETTY" -eq 0 ]; then
		say "Done: GamePanel $VERSION runs at https://$DOMAIN"
		[ -s "$TMP/admin" ] && cat "$TMP/admin"
		if [ "$HAD_DB" -eq 0 ] && [ ! -s "$TMP/admin" ]; then
			echo "    Create an admin: sudo runuser -u gamepanel -- sh -c 'set -a; . $ETC/panel.env; exec panel create-admin you@example.com'"
		fi
		[ -s "$TMP/caddy-snippet" ] && cat "$TMP/caddy-snippet"
		echo "    Back up $PANEL_HOME/panel.db and panel.db.key (docs/panel/backing-up.md)."
		echo "    Add a node: Nodes -> Create node, then run this script with 'node' on that machine."
		return 0
	fi
	printf '\n  %s%s GamePanel %s is ready%s  %sin %dm %ds%s\n\n' "$ACC$BOLD" "$OK" "$VERSION" "$RST" "$DIM" $((secs / 60)) $((secs % 60)) "$RST"
	if [ "$CADDY" = no ]; then
		printf '    %-10s %s%s%s  %sonce your HTTPS proxy forwards it to 127.0.0.1:8080%s\n' "Open" "$BOLD" "https://$DOMAIN" "$RST" "$DIM" "$RST"
	else
		printf '    %-10s %s%s%s\n' "Open" "$BOLD" "https://$DOMAIN" "$RST"
	fi
	if [ -n "$pw" ]; then
		printf '    %-10s %s\n' "Email" "$EMAIL"
		printf '    %-10s %s%s%s  %sshown only now; change it after signing in%s\n' "Password" "$YLW$BOLD" "$pw" "$RST" "$DIM" "$RST"
	fi
	if [ -s "$TMP/caddy-snippet" ]; then
		printf '\n    Add this to /etc/caddy/Caddyfile, then run %ssystemctl reload caddy%s:\n\n' "$BOLD" "$RST"
		sed 's/^/      /' "$TMP/caddy-snippet"
	fi
	printf '\n  %sNext%s\n' "$BOLD" "$RST"
	printf '    1. Sign in. Admins set up a second factor (passkey or authenticator app) first.\n'
	if [ "${TERMS_ROLE:-}" = both ]; then
		printf '    2. Back up %s/panel.db and panel.db.key %s(docs/panel/backing-up.md)%s.\n' "$PANEL_HOME" "$DIM" "$RST"
	else
		printf '    2. Add a node: %sNodes %s Create node%s, then run this installer on that machine.\n' "$BOLD" "$ARROW" "$RST"
		printf '    3. Back up %s/panel.db and panel.db.key %s(docs/panel/backing-up.md)%s.\n' "$PANEL_HOME" "$DIM" "$RST"
	fi
	if [ -z "$pw" ] && [ "$HAD_DB" -eq 0 ]; then
		printf '\n    No admin yet. Create one:\n      %ssudo runuser -u gamepanel -- sh -c '"'"'set -a; . %s/panel.env; exec panel create-admin you@example.com'"'"'%s\n' "$BOLD" "$ETC" "$RST"
	fi
	printf '\n  %sLog: %s%s\n' "$DIM" "$LOG" "$RST"
}

# ---------------------------------------------------------------- node

install_docker() {
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

node_questions() {
	if [ "$INTERACTIVE" -eq 0 ]; then
		[ -n "$PANEL" ] || die "--panel is required (the panel's domain)"
		[ -n "$TOKEN" ] || die "--token is required (Nodes -> Create node in the panel)"
		return 0
	fi
	[ "${TERMS_ROLE:-}" = both ] || rule "The node"
	if [ -z "$PANEL" ]; then
		out '    %sThe domain your panel runs at; the node connects to it, nothing connects in.%s\n' "$DIM" "$RST"
		ask PANEL "Panel domain" "" check_panel
	fi
	if [ -z "$TOKEN" ]; then
		out '    %sIn the panel: Nodes %s Create node. Paste the token it shows (typing is hidden).%s\n' "$DIM" "$ARROW" "$RST"
		ask TOKEN "Node token" "" check_token secret
	fi
	if ! command -v docker >/dev/null; then
		[ "$DOCKER" = no ] && die "Docker is not installed (and --no-docker was given)"
		out '    %sGame servers run in Docker containers.%s\n' "$DIM" "$RST"
		ask_yn "Install Docker with Docker's script (get.docker.com)?" y || die "the daemon needs Docker"
		DOCKER=yes
	fi
	if [ -z "$ADOPT" ] && [ -d "$WINGS_VOLUMES" ] && [ ! -x "$BIN/gamepanel-daemon" ]; then
		out '    %sFound Pterodactyl server files in %s. Take them over only if you imported\n    that Pterodactyl panel into GamePanel (docs/guides/from-pterodactyl.md).%s\n' "$DIM" "$WINGS_VOLUMES" "$RST"
		ask_yn "Take over Wings' servers?" n && ADOPT=$WINGS_VOLUMES
	fi
	return 0
}

daemon_files() {
	install -m 0755 "$TMP/daemon" "$BIN/gamepanel-daemon"
	install -d -m 0700 "$ETC" "$DAEMON_DATA"
	if [ ! -f "$ETC/daemon.env" ]; then
		echo "Writing $ETC/daemon.env"
		umask 077
		cat >"$ETC/daemon.env" <<EOF
# GamePanel daemon settings; every option is in docs/nodes/configuration.md.
GAMEPANEL_NODE_TOKEN=$TOKEN
GAMEPANEL_PANEL_URL=$NODE_URL
GAMEPANEL_DATA_DIR=$DAEMON_DATA
GAMEPANEL_SFTP_ADDR=0.0.0.0:2022
EOF
	else
		echo "Keeping the existing $ETC/daemon.env"
	fi
	if modprobe ifb 2>/dev/null; then
		echo ifb >/etc/modules-load.d/ifb.conf
	else
		warn "no ifb kernel module: incoming bandwidth limits fall back to a policer"
	fi
	for t in tc ip nsenter; do
		command -v "$t" >/dev/null || warn "$t is missing: network limits are off (install iproute2 and util-linux)"
	done
}

adopt_wings() {
	# Before the daemon's first start: once it connects, the panel creates
	# the imported servers here, and any whose files are not here yet would
	# be installed from scratch.
	if systemctl is-active --quiet wings 2>/dev/null; then
		die "Wings is still running; stop it first: systemctl disable --now wings"
	fi
	# shellcheck source=/dev/null
	(set -a && . "$ETC/daemon.env" && "$BIN/gamepanel-daemon" adopt-wings --from "$ADOPT") ||
		die "adopt-wings failed; the daemon was not started. Fix the problem above and run this again"
}

start_daemon() {
	date -u '+%F %T' >"$TMP/daemon-started"
	install -m 0644 "$TMP/packaging/systemd/gamepanel-daemon.service" /etc/systemd/system/gamepanel-daemon.service
	systemctl daemon-reload
	systemctl enable --now gamepanel-daemon
	systemctl restart gamepanel-daemon
	sleep 2
	systemctl is-active --quiet gamepanel-daemon || {
		journalctl -u gamepanel-daemon -n 20 --no-pager || true
		die "the daemon stopped right after starting"
	}
}

# wait_connected: the daemon logs "connected to panel" once the panel took
# its token. A refused token is an error; an unreachable panel only a
# warning, since the daemon keeps retrying.
wait_connected() {
	local since i lines
	since=$(cat "$TMP/daemon-started")
	for i in $(seq 1 20); do
		lines=$(journalctl -u gamepanel-daemon --since "$since UTC" -o cat --no-pager 2>/dev/null) || lines=""
		case $lines in *"connected to panel"*)
			touch "$TMP/connected"
			return 0
			;;
		esac
		case $lines in *"got 401"* | *"got 403"*)
			die "the panel refused the node token. Copy the whole token from Nodes -> Create node (or create the node again), put it in $ETC/daemon.env and run: systemctl restart gamepanel-daemon"
			;;
		esac
		sleep 1
	done
	warn "the node has not reached the panel at $NODE_URL yet; it keeps trying. See journalctl -u gamepanel-daemon -f"
}

cmd_node() {
	begin
	[ "${WIZARD:-0}" -eq 1 ] || {
		banner
		preflight node
	}
	node_questions
	NODE_URL=$PANEL
	case "$NODE_URL" in
	ws://* | wss://*) ;;
	*) NODE_URL="wss://$NODE_URL/daemon/connect" ;;
	esac
	case "$NODE_URL" in
	ws://127.0.0.1* | ws://localhost* | wss://*) ;;
	*) warn "$NODE_URL is not encrypted: the node token crosses the network in the clear" ;;
	esac
	terms node
	if command -v docker >/dev/null && ! docker info >/dev/null 2>&1; then
		DOCKER=start
	elif ! command -v docker >/dev/null; then
		[ "$DOCKER" = no ] && die "Docker is not installed (and --no-docker was given)"
		[ "$DOCKER" = yes ] || confirm "Docker is not installed. Install it with Docker's script (get.docker.com)?" ||
			die "the daemon needs Docker"
		DOCKER=yes
	fi
	if [ "$INTERACTIVE" -eq 1 ]; then
		run "Checking the release" resolve_version_file
		VERSION=$(cat "$TMP/version")
	else
		resolve_version
	fi
	local -a plan=()
	[ "$DOCKER" = yes ] && plan+=("Docker Engine, from Docker's repository")
	[ "$DOCKER" = start ] && plan+=("start Docker, which is installed but not running")
	plan+=("GamePanel daemon $VERSION ($(arch)), checked against the release's checksums"
		"$BIN/gamepanel-daemon, settings in $ETC/daemon.env"
		"game server files in $DAEMON_DATA"
		"systemd service gamepanel-daemon, connecting to $NODE_URL")
	[ -n "$ADOPT" ] && plan+=("Wings' servers moved over from $ADOPT")
	ready "${plan[@]}"

	local started=$SECONDS
	[ "$DOCKER" = yes ] && run "Installing Docker" install_docker
	[ "$DOCKER" = start ] && run "Starting Docker" systemctl enable --now docker
	run "Downloading the daemon $VERSION" fetch daemon "$TMP"
	run "Installing the daemon" daemon_files
	[ -n "$ADOPT" ] && run "Taking over Wings' servers from $ADOPT" adopt_wings
	run "Starting the daemon" start_daemon
	run "Connecting to the panel" wait_connected
	record_terms node
	node_done "$started"
}

node_done() {
	local secs=$((SECONDS - $1))
	if [ "$PRETTY" -eq 0 ]; then
		if [ -f "$TMP/connected" ]; then say "Done: the node is connected to the panel"; else say "Done: the node shows as online in the panel once it reaches it"; fi
		echo "    Logs: journalctl -u gamepanel-daemon -f"
		echo "    Open the node's game port range and 2022/tcp (SFTP) in your firewall."
		return 0
	fi
	printf '\n  %s%s The node is running%s  %sin %dm %ds%s\n\n' "$ACC$BOLD" "$OK" "$RST" "$DIM" $((secs / 60)) $((secs % 60)) "$RST"
	if [ -f "$TMP/connected" ]; then
		printf '    It is connected to the panel and shows as online there.\n'
	else
		printf '    It shows as online in the panel once it reaches it.\n'
	fi
	printf '\n  %sNext%s\n' "$BOLD" "$RST"
	printf '    1. Open the node'"'"'s game port range and 2022/tcp (SFTP) in your firewall.\n'
	printf '    2. Create a server: %sServers %s Create server%s.\n' "$BOLD" "$ARROW" "$RST"
	printf '\n  %sLogs: journalctl -u gamepanel-daemon -f%s\n' "$DIM" "$RST"
	printf '  %sLog: %s%s\n' "$DIM" "$LOG" "$RST"
}

# ---------------------------------------------------------------- upgrade

backup_panel() { # backup_panel <dest> <from>
	# Stopped, the database is consistent on disk and safe to copy.
	systemctl stop gamepanel
	install -d -m 0700 "$1"
	cp -a "$PANEL_HOME"/panel.db* "$1"/
	cp -a "$BIN/panel" "$1/panel-$2"
}

replace_panel() {
	install -m 0755 "$TMP/panel" "$BIN/panel"
	install -m 0644 "$TMP/packaging/systemd/gamepanel.service" /etc/systemd/system/gamepanel.service
	systemctl daemon-reload
	start_panel
}

replace_daemon() {
	install -m 0755 "$TMP/daemon" "$BIN/gamepanel-daemon"
	install -m 0644 "$TMP/packaging/systemd/gamepanel-daemon.service" /etc/systemd/system/gamepanel-daemon.service
	systemctl daemon-reload
	systemctl restart gamepanel-daemon
}

cmd_upgrade() {
	begin
	[ "${WIZARD:-0}" -eq 1 ] || banner
	local has_panel=0 has_daemon=0 from dest
	[ -x "$BIN/panel" ] && has_panel=1
	[ -x "$BIN/gamepanel-daemon" ] && has_daemon=1
	[ $has_panel -eq 1 ] || [ $has_daemon -eq 1 ] || die "nothing to upgrade: neither $BIN/panel nor $BIN/gamepanel-daemon is installed"
	resolve_version
	rule "Upgrading to $VERSION"

	if [ $has_panel -eq 1 ]; then
		from=$(installed_version "$BIN/panel")
		if [ "$from" = "$VERSION" ]; then
			check_line ok "The panel already runs $VERSION"
		else
			dest="$BACKUPS/pre-$VERSION-$(date +%Y%m%d-%H%M%S)"
			run "Downloading the panel $VERSION" fetch panel "$TMP"
			run "Backing up the panel to $dest" backup_panel "$dest" "$from"
			run "Upgrading the panel from ${from:-unknown} (migrations run at start)" replace_panel
			printf '    %sTo go back: stop it, put %s/panel-%s back, restore the database from %s.%s\n' "$DIM" "$dest" "$from" "$dest" "$RST"
		fi
	fi
	if [ $has_daemon -eq 1 ]; then
		from=$(installed_version "$BIN/gamepanel-daemon")
		if [ "$from" = "$VERSION" ]; then
			check_line ok "The daemon already runs $VERSION"
		else
			run "Downloading the daemon $VERSION" fetch daemon "$TMP"
			run "Upgrading the daemon from ${from:-unknown} (game servers keep running)" replace_daemon
		fi
	fi
	printf '\n  %s%s Up to date: GamePanel %s%s\n\n' "$ACC$BOLD" "$OK" "$VERSION" "$RST"
}

# ---------------------------------------------------------------- guided

wizard() {
	[ "$INTERACTIVE" -eq 1 ] || {
		usage
		exit 1
	}
	begin
	WIZARD=1
	banner
	local what
	local -a opts=("panel|The panel|the website you manage everything from"
		"node|A node|runs game servers in Docker, connects to a panel"
		"both|Panel and node|everything on this one machine")
	{ [ -x "$BIN/panel" ] || [ -x "$BIN/gamepanel-daemon" ]; } &&
		opts=("upgrade|Upgrade|to the newest release, backing up the panel first" "${opts[@]}")
	opts+=("quit|Quit|change nothing")
	printf '\n'
	choose what "What do you want to install?" "${opts[@]}"
	case $what in
	quit) exit 0 ;;
	upgrade)
		cmd_upgrade
		return
		;;
	esac
	preflight "$what"
	TERMS_ROLE=$what
	case $what in
	panel) cmd_panel ;;
	node) cmd_node ;;
	both)
		cmd_panel
		rule "Now the node"
		printf '\n    Sign in, open %sNodes %s Create node%s and copy its token. The node talks to\n' "$BOLD" "$ARROW" "$RST"
		printf '    the panel on this machine directly, so it works before HTTPS does.\n\n'
		if ! ask_yn "Set up the node now?" y; then
			printf '\n    Later, run: %scurl -fsSL https://github.com/%s/releases/latest/download/install.sh | sudo bash -s -- node --panel ws://127.0.0.1:8080/daemon/connect --token <token>%s\n\n' "$BOLD" "$REPO" "$RST"
			exit 0
		fi
		PANEL="ws://127.0.0.1:8080/daemon/connect"
		VERSION=$(cat "$TMP/version")
		cmd_node
		;;
	esac
}

usage() {
	cat <<'EOF'
Usage: install.sh [command] [options]

With no command, on a terminal, it asks what to install.

Commands:
  panel      install the panel (and Caddy for HTTPS)
  node       install the daemon on a machine that will run game servers
  upgrade    upgrade whatever is installed here (backs up the panel first)
  download   only download and verify both archives into the current folder
  terms      show the terms that installing means accepting
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
  --accept-terms      accept the terms (install.sh terms) without being asked
  -y, --yes           ask nothing: accept the terms and install Docker or
                      Caddy when needed; missing options are errors

Set NO_COLOR=1 for plain output.
EOF
}

# ---------------------------------------------------------------- main

CMD=""
if [ $# -gt 0 ] && [[ $1 != -* ]]; then
	CMD=$1
	shift
fi
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
	--accept-terms) ACCEPT=1 ;;
	-y | --yes) YES=1 ACCEPT=1 ;;
	-h | --help) CMD=help ;;
	-*) die "unknown option $1 (see install.sh help)" ;;
	*)
		[ -z "$CMD" ] || die "one command at a time ($CMD, $1)"
		CMD=$1
		;;
	esac
	shift
done
case "$VERSION" in latest | v*) ;; *) VERSION="v$VERSION" ;; esac
[ -z "$DOMAIN" ] || why=$(check_domain "$DOMAIN") || die "--domain: $why"
[ -z "$EMAIL" ] || why=$(check_email "$EMAIL") || die "--email: $why"

# Ask questions only with a terminal to ask on, and never with --yes.
if [ "$YES" -eq 0 ] && (exec </dev/tty) 2>/dev/null; then INTERACTIVE=1; fi

case "$CMD" in
"") wizard ;;
panel) cmd_panel ;;
node) cmd_node ;;
upgrade) cmd_upgrade ;;
download)
	resolve_version
	fetch panel "$PWD"
	fetch daemon "$PWD"
	say "Verified and unpacked into $PWD"
	;;
terms) terms_text all ;;
help) usage ;;
*) die "unknown command $CMD (see install.sh help)" ;;
esac
