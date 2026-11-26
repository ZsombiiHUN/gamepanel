#!/usr/bin/env bash
# Checks that game servers are isolated, against a real Docker engine: panel
# and daemon run as root on a Docker host (a docker:dind container, so this
# works the same on Docker Desktop). Two servers run a small web server on
# their published port and on an unpublished one; from inside one of them:
#
#   - the other server's unpublished port         must be unreachable
#   - the other server's published port (node IP) must be reachable
#   - the panel, on the node                      must be unreachable
#   - a port listed in GAMEPANEL_HOST_PORTS       must be reachable
#   - a "cloud metadata" service at 169.254.169.254 (a network namespace
#     routed from the host, like a real one)      must be unreachable
#
# The same checks run once more with GAMEPANEL_FIREWALL=off, where the
# blocked ones must succeed: that proves the checks can fail. Needs: docker, go.
#
#   bash scripts/isolation-smoke.sh
set -u
IMAGE=${IMAGE:-busybox:1.37}

if [ "${1:-}" != "--inside" ]; then
  cd "$(dirname "$0")/.."
  T=$(mktemp -d); H=gp-iso-$$
  trap '[ -n "${KEEP:-}" ] || docker rm -f $H >/dev/null 2>&1; rm -rf "$T"' EXIT
  [ -z "${KEEP:-}" ] || H=gp-iso-keep; docker rm -f $H >/dev/null 2>&1
  CGO_ENABLED=0 go build -o $T/panel ./cmd/panel && CGO_ENABLED=0 go build -o $T/daemon ./cmd/daemon || exit 1
  docker run -d --privileged --name $H -e DOCKER_TLS_CERTDIR= docker:29-dind >/dev/null || exit 1
  docker cp $T/panel $H:/usr/local/bin/panel >/dev/null && docker cp $T/daemon $H:/usr/local/bin/daemon >/dev/null &&
    docker cp "$0" $H:/smoke.sh >/dev/null || exit 1
  docker exec -e IMAGE=$IMAGE $H sh -c 'apk add -q bash curl python3 iproute2 >/dev/null && bash /smoke.sh --inside'
  exit $?
fi

# ---- inside the Docker host ----
fail=0; ok(){ echo "  ok   $*"; }; bad(){ echo "  FAIL $*"; fail=1; }
check(){ if eval "$2"; then ok "$1"; else bad "$1"; fi; }
for _ in $(seq 60); do docker info >/dev/null 2>&1 && break; sleep 1; done
docker pull -q $IMAGE >/dev/null || { echo "cannot pull $IMAGE"; exit 1; }
NODE_IP=$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1)

# A fake metadata service, off the host like a real one: its own network
# namespace, routed through a veth pair.
ip netns add meta && ip link add gpm0 type veth peer name gpm1 && ip link set gpm1 netns meta &&
  ip addr add 169.254.169.1/24 dev gpm0 && ip link set gpm0 up &&
  ip netns exec meta ip addr add 169.254.169.254/24 dev gpm1 && ip netns exec meta ip link set gpm1 up &&
  ip netns exec meta ip link set lo up || { echo "cannot make the metadata namespace"; exit 1; }
ip netns exec meta python3 -m http.server 80 --bind 169.254.169.254 >/dev/null 2>&1 &
# Something on the node that servers may reach (GAMEPANEL_HOST_PORTS) and
# something they may not.
python3 -m http.server 13306 >/dev/null 2>&1 &
python3 -m http.server 13307 >/dev/null 2>&1 &

T=/tmp/gp; mkdir -p $T; export GAMEPANEL_DB=$T/p.db
PW=$(panel create-admin admin@example.com | awk '/password:/{print $2}')
GAMEPANEL_ADDR=0.0.0.0:18093 panel >$T/panel.log 2>&1 &
sleep 1; B=http://127.0.0.1:18093/api; J=$T/jar
c(){ curl -s -b $J -H 'content-type: application/json' "$@"; }
curl -s -c $J -H 'content-type: application/json' -d "{\"email\":\"admin@example.com\",\"password\":\"$PW\"}" $B/auth/login >/dev/null
TOK=$(c -d '{"name":"local","port_start":25600,"port_end":25610}' $B/nodes | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
DP=
start_daemon(){
  GAMEPANEL_PANEL_URL=ws://127.0.0.1:18093/daemon/connect GAMEPANEL_NODE_TOKEN=$TOK GAMEPANEL_SFTP_ADDR=off \
    GAMEPANEL_DATA_DIR=$T/dd GAMEPANEL_CONTAINER_USER=2500:2600 GAMEPANEL_HOST_PORTS=13306/tcp "$@" daemon >>$T/daemon.log 2>&1 &
  DP=$!
  sleep 2
}
stop_daemon(){ kill $DP; wait $DP 2>/dev/null; }
env_daemon(){ start_daemon env "$@"; }
env_daemon
c -d "{\"name\":\"web\",\"docker_images\":{\"a\":\"$IMAGE\"},\"startup\":\"echo hi > index.html; httpd -p 9999 -h .; exec httpd -f -p \$SERVER_PORT -h .\",\"config\":{\"stop\":\"^C\"}}" $B/eggs/import >/dev/null

new_server(){ c -d "{\"name\":\"$1\",\"node_id\":1,\"egg_id\":1,\"memory_mb\":64}" $B/servers | python3 -c 'import sys,json;print(json.load(sys.stdin)["uuid"])'; }
state(){ c $B/servers/$1 | python3 -c 'import sys,json;print(json.load(sys.stdin)["state"])'; }
port(){ c $B/servers/$1 | python3 -c 'import sys,json;print(json.load(sys.stdin)["port"])'; }
power(){ c -d "{\"action\":\"$2\"}" $B/servers/$1/power >/dev/null; }
# run: start a server, again until it runs (a start is refused while its
# container is still being built).
run(){ for _ in $(seq 30); do power $1 start; for _ in $(seq 6); do [ "$(state $1)" = running ] && return 0; sleep 0.5; done; done; return 1; }
wait_state(){ for _ in $(seq 120); do [ "$(state $1)" = "$2" ] && return 0; sleep 0.5; done; return 1; }
ip_of(){ docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' gp-$1; }
gw(){ docker network inspect -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}' gamepanel0; }
# reach FROM URL: whether server FROM can fetch URL.
reach(){ docker exec gp-$1 wget -q -T 3 -O /dev/null "$2" 2>/dev/null; }

A=$(new_server a); Bs=$(new_server b)
check "both servers run" 'run $A && run $Bs'

echo "isolation on"
check "containers are on the gamepanel0 network" '[ "$(docker inspect -f "{{.HostConfig.NetworkMode}}" gp-$A)" = gamepanel0 ]'
check "inter-container traffic is off on gamepanel0" '[ "$(docker network inspect -f "{{index .Options \"com.docker.network.bridge.enable_icc\"}}" gamepanel0)" = false ]'
check "logs use the rotating local driver" '[ "$(docker inspect -f "{{.HostConfig.LogConfig.Type}} {{index .HostConfig.LogConfig.Config \"max-size\"}}" gp-$A)" = "local 10m" ]'
check "games run as the configured user" '[ "$(docker inspect -f "{{.Config.User}}" gp-$A)" = 2500:2600 ]'
check "the server's files belong to it" '[ "$(stat -c %u:%g $T/dd/servers/$A/index.html)" = 2500:2600 ]'
check "HOME is the server's folder" 'docker exec gp-$A sh -c "[ \"\$HOME\" = /home/container ]"'
check "a server reaches its own unpublished port" 'reach $A http://127.0.0.1:9999/'
check "the other server's unpublished port is unreachable" '! reach $A http://$(ip_of $Bs):9999/'
check "the other server's published port is unreachable on its container address" '! reach $A http://$(ip_of $Bs):$(port $Bs)/'
check "the other server's published port is reachable on the node's address" 'reach $A http://$NODE_IP:$(port $Bs)/'
check "the panel on the node is unreachable (node address)" '! reach $A http://$NODE_IP:18093/'
check "the panel on the node is unreachable (gateway)" '! reach $A http://$(gw):18093/'
check "a port in GAMEPANEL_HOST_PORTS is reachable" 'reach $A http://$(gw):13306/'
check "a node port not listed is unreachable" '! reach $A http://$(gw):13307/'
check "cloud metadata is unreachable" '! reach $A http://169.254.169.254/'
check "the internet is reachable" 'reach $A http://example.com/ || reach $A http://1.1.1.1/'
check "outside, the published port is reachable" 'curl -sf -m 3 http://$NODE_IP:$(port $A)/ >/dev/null'

echo "restarting the daemon"
stop_daemon; env_daemon
check "the firewall jumps are not duplicated" '[ "$(iptables -S INPUT | grep -c GAMEPANEL-INPUT)" = 1 ] && [ "$(iptables -S DOCKER-USER | grep -c GAMEPANEL-FORWARD)" = 1 ]'
check "a deleted server's ports close to the others" 'C=$(new_server c); wait_state $C offline; P=$(port $C); iptables -S GAMEPANEL-INPUT | grep -q "dport $P " && c -X DELETE $B/servers/$C >/dev/null && sleep 2 && ! iptables -S GAMEPANEL-INPUT | grep -q "dport $P "'

echo "control: GAMEPANEL_FIREWALL=off (the blocked checks must now pass)"
stop_daemon
iptables -D INPUT -i gp0 -j GAMEPANEL-INPUT; iptables -D DOCKER-USER -i gp0 -j GAMEPANEL-FORWARD
env_daemon GAMEPANEL_FIREWALL=off
check "the panel is reachable without the firewall" 'reach $A http://$(gw):18093/'
check "metadata is reachable without the firewall" 'reach $A http://169.254.169.254/'
check "the other server's unpublished port stays unreachable (Docker's ICC setting alone)" '! reach $A http://$(ip_of $Bs):9999/'

stop_daemon
if [ $fail -ne 0 ]; then
  echo "--- daemon log"; tail -40 $T/daemon.log
fi
[ $fail -eq 0 ] && echo "PASS" || echo "FAILED"
exit $fail
