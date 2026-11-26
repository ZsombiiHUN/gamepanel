#!/usr/bin/env bash
# Moves a real Pterodactyl panel and Wings node to GamePanel and checks that
# the game server comes back as it was. Needs docker, go and internet access
# (images, Wings, Paper); takes about ten minutes.
#
#   bash scripts/ptero-import-smoke.sh
#
# 1. Pterodactyl (PTERO_PANEL_IMAGE) with MariaDB, and a node: a docker:dind
#    container running Wings (WINGS_URL). Through Pterodactyl's own services
#    it gets two accounts, a Paper server (installed and started by Wings),
#    a subuser and a schedule.
# 2. GamePanel's panel and daemon in the same node container:
#    panel import-pterodactyl --apply (with a read-only database user), Wings
#    stopped (its server keeps running), daemon adopt-wings, the daemon
#    started: it stops the server under Wings with the egg's stop command and
#    starts it itself.
# 3. Checks: the Pterodactyl password signs in, the server starts by itself
#    on the same port with the same world and jar (nothing reinstalled), the
#    egg's config file rule wrote the port, stop and start work, and the
#    subuser and schedule came over. Then, as a billing module would through
#    Pterodactyl's application API: the server, egg, node and location keep
#    their Pterodactyl ids, the server is found by its external id, and
#    suspending and unsuspending it work.
set -u
PANEL_IMAGE=${PTERO_PANEL_IMAGE:-ghcr.io/pterodactyl/panel:latest}
WINGS_URL=${WINGS_URL:-https://github.com/pterodactyl/wings/releases/latest/download/wings_linux_amd64}
MC_VERSION=${MC_VERSION:-1.21.4}
P=gpt$$
DB=$P-db PANEL=$P-panel NODE=$P-node
APP_KEY=base64:7NZw/Mk/56iKLVPFAFqc9/IYSZGf/5uw2tSAlfZ7/Gg= # throwaway
ADMIN_PW=ptero-admin-password-1 FRIEND_PW=ptero-friend-password-1

fail=0
ok() { echo "  ok   $*"; }
bad() { echo "  FAIL $*"; fail=1; }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }
die() {
	echo "error: $*" >&2
	exit 1
}
step() { echo "==> $*"; }

cd "$(dirname "$0")/.."
T=$(mktemp -d)
cleanup() {
	if [ "${KEEP:-}" = 1 ]; then
		echo "KEEP=1: containers $DB $PANEL $NODE and network $P are left running"
	else
		docker rm -f "$DB" "$PANEL" "$NODE" >/dev/null 2>&1
		docker network rm "$P" >/dev/null 2>&1
	fi
	rm -rf "$T"
}
trap cleanup EXIT

step "Building GamePanel"
CGO_ENABLED=0 go build -o "$T/panel" ./cmd/panel && CGO_ENABLED=0 go build -o "$T/daemon" ./cmd/daemon || die "build failed"

step "Starting MariaDB, Pterodactyl and a Wings node"
docker network create "$P" >/dev/null || die "network"
docker run -d --name "$DB" --network "$P" -e MARIADB_ROOT_PASSWORD=root -e MARIADB_DATABASE=panel \
	-e MARIADB_USER=pterodactyl -e MARIADB_PASSWORD=ptero mariadb:10.11 >/dev/null || die "mariadb"
docker create --name "$PANEL" --network "$P" -e APP_URL="http://$PANEL" -e APP_KEY="$APP_KEY" -e APP_ENV=production \
	-e APP_TIMEZONE=UTC -e APP_SERVICE_AUTHOR=smoke@example.com -e HASHIDS_SALT=smoketestsalt -e HASHIDS_LENGTH=8 \
	-e DB_HOST="$DB" -e DB_PORT=3306 -e DB_DATABASE=panel -e DB_USERNAME=pterodactyl -e DB_PASSWORD=ptero \
	-e CACHE_DRIVER=file -e SESSION_DRIVER=file -e QUEUE_CONNECTION=sync -e MAIL_MAILER=array -e MAIL_DRIVER=array \
	-e RECAPTCHA_ENABLED=false -e TRUSTED_PROXIES='*' "$PANEL_IMAGE" >/dev/null || die "pterodactyl panel"
# The image's mysql client insists on TLS, which this MariaDB does not offer.
printf '[client]\nskip-ssl\n' >"$T/skip-ssl.cnf"
docker cp "$T/skip-ssl.cnf" "$PANEL:/etc/my.cnf.d/zz-skip-ssl.cnf" >/dev/null && docker start "$PANEL" >/dev/null || die "pterodactyl panel"
docker run -d --privileged --name "$NODE" --network "$P" -e DOCKER_TLS_CERTDIR= -e TZ=UTC docker:29-dind >/dev/null || die "node"
for f in panel daemon; do docker cp "$T/$f" "$NODE:/usr/local/bin/$f" >/dev/null || die "copy $f"; done

cat >"$T/slp.py" <<'EOF'
# Minecraft Server List Ping: prints the server's MOTD, or fails.
import json, socket, struct, sys
def varint(n):
    out = b""
    while True:
        b = n & 0x7F
        n >>= 7
        out += bytes([b | (0x80 if n else 0)])
        if not n:
            return out
def read_varint(s):
    n = shift = 0
    while True:
        b = s.recv(1)[0]
        n |= (b & 0x7F) << shift
        shift += 7
        if not b & 0x80:
            return n
host, port = sys.argv[1], int(sys.argv[2])
s = socket.create_connection((host, port), 5)
hs = b"\x00" + varint(769) + varint(len(host)) + host.encode() + struct.pack(">H", port) + varint(1)
s.sendall(varint(len(hs)) + hs + b"\x01\x00")
read_varint(s); read_varint(s); n = read_varint(s)
data = b""
while len(data) < n:
    data += s.recv(n - len(data))
st = json.loads(data)
d = st["description"]
print(d if isinstance(d, str) else d.get("text", ""), st["version"]["name"])
EOF
docker cp "$T/slp.py" "$NODE:/slp.py" >/dev/null || die "copy slp.py"
docker exec "$NODE" sh -c 'apk add -q bash curl python3 jq >/dev/null' || die "node packages"
docker exec "$NODE" sh -c "curl -fsSL -o /usr/local/bin/wings '$WINGS_URL' && chmod +x /usr/local/bin/wings" || die "download wings"

step "Waiting for Pterodactyl (migrations and stock eggs)"
for _ in $(seq 180); do docker exec "$PANEL" wget -qO- http://127.0.0.1/auth/login >/dev/null 2>&1 && break; sleep 2; done
docker exec "$PANEL" wget -qO- http://127.0.0.1/auth/login >/dev/null 2>&1 || {
	docker logs "$PANEL" | tail -30
	die "Pterodactyl did not come up"
}
docker exec "$DB" mariadb -uroot -proot -e "CREATE USER 'gpimport'@'%' IDENTIFIED BY 'readonly'; GRANT SELECT ON panel.* TO 'gpimport'@'%';" ||
	die "read-only user"

php() { docker exec -e NODE_FQDN="$NODE" -e ADMIN_PW="$ADMIN_PW" -e FRIEND_PW="$FRIEND_PW" -e MC_VERSION="$MC_VERSION" "$PANEL" php -r "$1"; }
BOOT='require "/app/vendor/autoload.php"; $app = require "/app/bootstrap/app.php"; $app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();'

step "Creating a location, node, ports and accounts in Pterodactyl"
php "$BOOT"'
use Pterodactyl\Models\{Location, Allocation};
$loc = Location::create(["short" => "smoke", "long" => "Smoke test"]);
$node = $app->make(Pterodactyl\Services\Nodes\NodeCreationService::class)->handle([
  "name" => "Smoke node", "location_id" => $loc->id, "fqdn" => getenv("NODE_FQDN"), "scheme" => "http", "behind_proxy" => false,
  "memory" => 8192, "memory_overallocate" => 0, "disk" => 50000, "disk_overallocate" => 0, "upload_size" => 100,
  "daemonListen" => 8080, "daemonSFTP" => 2022, "daemonBase" => "/var/lib/pterodactyl/volumes", "public" => true,
]);
foreach ([25565, 25566] as $p) { Allocation::create(["node_id" => $node->id, "ip" => "0.0.0.0", "port" => $p]); }
$u = $app->make(Pterodactyl\Services\Users\UserCreationService::class);
$u->handle(["email" => "admin@example.com", "username" => "admin", "name_first" => "Ad", "name_last" => "Min", "password" => getenv("ADMIN_PW"), "root_admin" => true]);
$u->handle(["email" => "friend@example.com", "username" => "friend", "name_first" => "Fr", "name_last" => "End", "password" => getenv("FRIEND_PW"), "root_admin" => false]);
' || die "seeding Pterodactyl"

step "Starting Wings"
docker exec "$PANEL" php artisan p:node:configuration 1 --format=yaml >"$T/config.yml" || die "Wings configuration"
docker exec "$NODE" mkdir -p /etc/pterodactyl
docker cp "$T/config.yml" "$NODE:/etc/pterodactyl/config.yml" >/dev/null
for _ in $(seq 60); do docker exec "$NODE" docker info >/dev/null 2>&1 && break; sleep 1; done
docker exec -d "$NODE" sh -c 'wings --config /etc/pterodactyl/config.yml >/var/log/wings.log 2>&1'
for _ in $(seq 60); do docker exec "$NODE" sh -c 'curl -s -o /dev/null http://127.0.0.1:8080/' && break; sleep 1; done
docker exec "$NODE" sh -c 'curl -s -o /dev/null http://127.0.0.1:8080/' || {
	docker exec "$NODE" tail -30 /var/log/wings.log
	die "Wings did not start"
}

step "Creating a Paper $MC_VERSION server through Pterodactyl (Wings installs and starts it)"
UUID=$(php "$BOOT"'
use Pterodactyl\Models\{Allocation, Egg, User, Schedule, Task};
$egg = Egg::where("name", "Paper")->firstOrFail();
$admin = User::where("email", "admin@example.com")->firstOrFail();
$s = $app->make(Pterodactyl\Services\Servers\ServerCreationService::class)->handle([
  "name" => "Survival", "description" => "Smoke test", "owner_id" => $admin->id, "egg_id" => $egg->id,
  "allocation_id" => Allocation::where("port", 25565)->value("id"), "allocation_additional" => [Allocation::where("port", 25566)->value("id")],
  "environment" => ["MINECRAFT_VERSION" => getenv("MC_VERSION"), "SERVER_JARFILE" => "server.jar", "BUILD_NUMBER" => "latest", "DL_PATH" => ""],
  "memory" => 2048, "swap" => 0, "disk" => 0, "io" => 500, "cpu" => 0, "threads" => null,
  "startup" => $egg->startup, "image" => "ghcr.io/pterodactyl/yolks:java_21",
  "database_limit" => 0, "allocation_limit" => 2, "backup_limit" => 2, "start_on_completion" => true, "oom_disabled" => true,
  "external_id" => "whmcs-1001",
]);
$app->make(Pterodactyl\Services\Subusers\SubuserCreationService::class)->handle($s, "friend@example.com",
  ["websocket.connect", "control.console", "control.start", "control.stop", "control.restart", "file.read", "file.read-content"]);
$sc = Schedule::create(["server_id" => $s->id, "name" => "Nightly restart", "cron_minute" => "0", "cron_hour" => "4",
  "cron_day_of_month" => "*", "cron_month" => "*", "cron_day_of_week" => "*", "is_active" => true, "is_processing" => false,
  "only_when_online" => true, "next_run_at" => now()->addDay()]);
Task::create(["schedule_id" => $sc->id, "sequence_id" => 1, "action" => "command", "payload" => "say Restarting", "time_offset" => 0, "is_queued" => false]);
Task::create(["schedule_id" => $sc->id, "sequence_id" => 2, "action" => "power", "payload" => "restart", "time_offset" => 10, "is_queued" => false]);
echo $s->uuid;
') || die "creating the server"
echo "    server $UUID"
# The ids a billing module stored: they must still work against GamePanel.
read -r PT_SERVER PT_EGG PT_NODE PT_LOCATION < <(php "$BOOT"'
$s = Pterodactyl\Models\Server::where("uuid", "'"$UUID"'")->firstOrFail();
echo $s->id, " ", $s->egg_id, " ", $s->node_id, " ", $s->node->location_id, PHP_EOL;
') || die "reading the server's ids"
echo "    ids: server $PT_SERVER, egg $PT_EGG, node $PT_NODE, location $PT_LOCATION"
VOL=/var/lib/pterodactyl/volumes/$UUID
mc() { docker exec "$NODE" python3 /slp.py 127.0.0.1 25565 2>/dev/null; }
# Paper stops on its first start until its EULA is accepted, which a person
# does in Pterodactyl's interface: do that, then start it through Pterodactyl.
for _ in $(seq 200); do docker exec "$NODE" test -f "$VOL/eula.txt" && break; sleep 3; done
docker exec "$NODE" sh -c "echo eula=true >$VOL/eula.txt" || die "accepting the EULA"
for _ in $(seq 60); do [ "$(docker exec "$NODE" docker inspect -f '{{.State.Running}}' "$UUID" 2>/dev/null)" = false ] && break; sleep 2; done
php "$BOOT"'
$s = Pterodactyl\Models\Server::where("uuid", "'"$UUID"'")->firstOrFail();
$app->make(Pterodactyl\Repositories\Wings\DaemonPowerRepository::class)->setServer($s)->send("start");
' || die "starting the server through Pterodactyl"
for _ in $(seq 300); do mc >/dev/null && break; sleep 3; done
mc >/dev/null || {
	docker exec "$NODE" sh -c "tail -40 /var/log/wings.log; ls -la $VOL"
	die "the server never answered under Wings"
}
BEFORE_MC=$(mc)
echo "    under Wings: $BEFORE_MC"
MARKER=marker-$RANDOM$RANDOM
docker exec "$NODE" sh -c "echo $MARKER >$VOL/smoke-marker.txt && chown \$(stat -c %u:%g $VOL) $VOL/smoke-marker.txt"
sleep 20 # let Paper save the world once, so uid.dat exists
JAR_SUM=$(docker exec "$NODE" sha256sum "$VOL/server.jar" | cut -d' ' -f1)
UID_SUM=$(docker exec "$NODE" sha256sum "$VOL/world/uid.dat" | cut -d' ' -f1)
OWNER=$(docker exec "$NODE" stat -c %u "$VOL")

step "Installing GamePanel's panel and importing"
docker exec -d "$NODE" sh -c 'mkdir -p /gp && cd /gp && GAMEPANEL_DB=/gp/panel.db GAMEPANEL_ADDR=127.0.0.1:18080 panel >/gp/panel.log 2>&1'
for _ in $(seq 30); do docker exec "$NODE" curl -s -o /dev/null http://127.0.0.1:18080/ && break; sleep 1; done
DSN="gpimport:readonly@tcp($DB:3306)/panel"
docker exec "$NODE" sh -c "cd /gp && panel import-pterodactyl --dsn '$DSN' --app-key '$APP_KEY'" >"$T/dry.txt" 2>&1
check "dry run is ready" 'grep -q "Result: ready" $T/dry.txt'
docker exec "$NODE" sh -c "cd /gp && GAMEPANEL_DB=/gp/panel.db panel import-pterodactyl --dsn '$DSN' --app-key '$APP_KEY' --apply" >"$T/apply.txt" 2>&1
check "--apply imports" 'grep -q "Imported into /gp/panel.db" $T/apply.txt'
TOKEN=$(awk '/token:/{print $2; exit}' "$T/apply.txt")
[ -n "$TOKEN" ] || {
	cat "$T/apply.txt"
	die "no node token printed"
}
check "a second --apply is refused" '! docker exec $NODE sh -c "cd /gp && GAMEPANEL_DB=/gp/panel.db panel import-pterodactyl --dsn \"$DSN\" --apply" >/dev/null 2>&1'

step "Switching the node: Wings off, adopt-wings, GamePanel daemon on"
docker exec "$NODE" pkill -f "wings --config"
sleep 2
check "the server still runs under Wings' container until adopt-wings" 'mc >/dev/null'
docker exec "$NODE" sh -c 'GAMEPANEL_DATA_DIR=/gp/dd daemon adopt-wings' >"$T/adopt.txt" 2>&1
check "adopt-wings moves the server" "grep -q 'moved $UUID' $T/adopt.txt"
check "which keeps running from its new folder meanwhile" 'mc >/dev/null'
docker exec "$NODE" sh -c "echo after-move >/gp/dd/servers/$UUID/written-after-move.txt"
docker exec -d "$NODE" sh -c "GAMEPANEL_PANEL_URL=ws://127.0.0.1:18080/daemon/connect GAMEPANEL_NODE_TOKEN=$TOKEN GAMEPANEL_DATA_DIR=/gp/dd daemon >/gp/daemon.log 2>&1"

B=http://127.0.0.1:18080/api
api() { docker exec "$NODE" curl -s -b "/gp/$1.jar" -c "/gp/$1.jar" -H 'content-type: application/json' "${@:2}"; }
login() { api "$1" -d "{\"email\":\"$1@example.com\",\"password\":\"$2\"}" "$B/auth/login"; }
state() { api admin "$B/servers/$UUID" | jq -r .state; }
wait_state() { for _ in $(seq "${2:-120}"); do [ "$(state)" = "$1" ] && return 0; sleep 1; done; return 1; }

echo "checks"
check "the admin signs in with the Pterodactyl password" 'login admin "$ADMIN_PW" | jq -e ".id" >/dev/null'
check "the server Wings had running starts by itself" 'wait_state running 180'
check "it was stopped under Wings with the egg's stop command (the world was saved)" 'docker exec $NODE docker logs $UUID 2>&1 | grep -q "Saving chunks for level"'
for _ in $(seq 120); do mc >/dev/null && break; sleep 2; done
AFTER_MC=$(mc)
echo "       under GamePanel: $AFTER_MC"
check "it answers on the same port, as the same server" '[ -n "$AFTER_MC" ] && [ "$AFTER_MC" = "$BEFORE_MC" ]'
D=/gp/dd/servers/$UUID
check "nothing was reinstalled (same server.jar)" '[ "$(docker exec $NODE sha256sum $D/server.jar | cut -d" " -f1)" = "$JAR_SUM" ]'
check "same world (world/uid.dat)" '[ "$(docker exec $NODE sha256sum $D/world/uid.dat | cut -d" " -f1)" = "$UID_SUM" ]'
check "files keep Wings' owner" '[ "$(docker exec $NODE stat -c %u $D)" = "$OWNER" ]'
check "the panel reads the server's files" '[ "$(api admin "$B/servers/$UUID/files/content?path=smoke-marker.txt" | jq -r .content)" = "$MARKER" ]'
check "the egg's config file rule set the port" 'docker exec $NODE grep -qx "server-port=25565" $D/server.properties'
check "the panel pings it (Minecraft status)" 'api admin "$B/servers/$UUID/minecraft/status" | jq -e ".version | test(\"$MC_VERSION\")" >/dev/null'
api admin -d '{"action":"stop"}' "$B/servers/$UUID/power" >/dev/null
check "stop works (the egg's stop command)" 'wait_state offline 90'
api admin -d '{"action":"start"}' "$B/servers/$UUID/power" >/dev/null
check "start works" 'wait_state running 120'
for _ in $(seq 90); do mc >/dev/null && break; sleep 2; done
check "and it answers again" 'mc >/dev/null'
check "the extra port came over" 'api admin "$B/servers/$UUID/network" | jq -e "[.ports[].port] | index(25566)" >/dev/null'
check "the schedule came over" '[ "$(api admin "$B/servers/$UUID/schedules" | jq "length")" = 1 ]'
check "the subuser signs in and sees the server" 'login friend "$FRIEND_PW" >/dev/null && api friend "$B/servers" | jq -e "map(.uuid) | index(\"$UUID\")" >/dev/null'
check "Wings' container is stopped" '[ "$(docker exec $NODE docker inspect -f "{{.State.Running}}" $UUID)" = false ]'

echo "billing (Pterodactyl's application API, as WHMCS calls it)"
KEY=$(api admin -d "{\"name\":\"billing\",\"password\":\"$ADMIN_PW\"}" "$B/auth/keys" | jq -r .token)
app() { docker exec "$NODE" curl -s -H "Authorization: Bearer $KEY" -H 'Accept: Application/vnd.pterodactyl.v1+json' -H 'content-type: application/json' -w '\n%{http_code}' "${@:2}" "$B/application/$1"; }
code() { tail -1; }
body() { sed '$d'; }
check "the server keeps its Pterodactyl id" '[ "$(app servers/$PT_SERVER | body | jq -r .attributes.uuid)" = "$UUID" ]'
check "and is found by its external id" '[ "$(app servers/external/whmcs-1001 | body | jq -r .attributes.id)" = "$PT_SERVER" ]'
check "the egg keeps its id, with its variables" 'app "eggs/$PT_EGG?include=variables" | body | jq -e ".attributes.name == \"Paper\" and (.attributes.relationships.variables.data | length > 0)" >/dev/null'
check "the node keeps its id and location" '[ "$(app nodes/$PT_NODE | body | jq -r .attributes.location_id)" = "$PT_LOCATION" ]'
check "suspending it (invoice overdue) stops it" '[ "$(app servers/$PT_SERVER/suspend -X POST | code)" = 204 ] && wait_state offline 90'
check "unsuspending it lets it start again" '[ "$(app servers/$PT_SERVER/unsuspend -X POST | code)" = 204 ] && [ "$(api admin -o /dev/null -w "%{http_code}" -d "{\"action\":\"start\"}" "$B/servers/$UUID/power")" = 202 ] && wait_state running 120'

if [ $fail -ne 0 ]; then
	echo "--- apply"
	cat "$T/apply.txt"
	echo "--- adopt"
	cat "$T/adopt.txt"
	echo "--- daemon log"
	docker exec "$NODE" tail -30 /gp/daemon.log
	echo "--- panel log"
	docker exec "$NODE" tail -30 /gp/panel.log
	echo "FAILED"
	exit 1
fi
echo "PASS"
