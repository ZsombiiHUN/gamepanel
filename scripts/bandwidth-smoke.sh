#!/usr/bin/env bash
# Checks network limits against a real Docker engine: panel and daemon run as
# root on a Docker host (a docker:dind container, so this works the same on
# Docker Desktop, where the engine runs in a VM the daemon could not nsenter
# into), with tc and nsenter installed, and iperf3 measures what a limited
# game server can actually send and receive. Needs: docker, go.
#
#   bash scripts/bandwidth-smoke.sh            # 10 Mbit/s limit
#   LIMIT=100 bash scripts/bandwidth-smoke.sh
set -u
LIMIT=${LIMIT:-10}
IPERF=${IPERF_IMAGE:-networkstatic/iperf3:latest}

if [ "${1:-}" != "--inside" ]; then
  cd "$(dirname "$0")/.."
  T=$(mktemp -d); H=gp-bw-$$
  trap 'docker rm -f $H >/dev/null 2>&1; rm -rf "$T"' EXIT
  CGO_ENABLED=0 go build -o $T/panel ./cmd/panel && CGO_ENABLED=0 go build -o $T/daemon ./cmd/daemon || exit 1
  docker run -d --privileged --name $H -e DOCKER_TLS_CERTDIR= docker:29-dind >/dev/null || exit 1
  docker cp $T/panel $H:/usr/local/bin/panel >/dev/null && docker cp $T/daemon $H:/usr/local/bin/daemon >/dev/null &&
    docker cp "$0" $H:/smoke.sh >/dev/null || exit 1
  docker exec -e LIMIT=$LIMIT -e IPERF_IMAGE=$IPERF $H sh -c 'apk add -q bash curl python3 iproute2-tc util-linux-misc >/dev/null && bash /smoke.sh --inside'
  exit $?
fi

# ---- inside the Docker host ----
fail=0; ok(){ echo "  ok   $*"; }; bad(){ echo "  FAIL $*"; fail=1; }
check(){ if eval "$2"; then ok "$1"; else bad "$1"; fi; }
for _ in $(seq 60); do docker info >/dev/null 2>&1 && break; sleep 1; done
docker pull -q $IPERF >/dev/null || { echo "cannot pull $IPERF"; exit 1; }

T=/tmp/gp; mkdir -p $T; export GAMEPANEL_DB=$T/p.db
PW=$(panel create-admin admin@example.com | awk '/password:/{print $2}')
GAMEPANEL_ADDR=127.0.0.1:18093 panel >$T/panel.log 2>&1 &
sleep 1; B=http://127.0.0.1:18093/api; J=$T/jar
c(){ curl -s -b $J -H 'content-type: application/json' "$@"; }
curl -s -c $J -H 'content-type: application/json' -d "{\"email\":\"admin@example.com\",\"password\":\"$PW\"}" $B/auth/login >/dev/null
TOK=$(c -d '{"name":"local","port_start":25600,"port_end":25610}' $B/nodes | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
start_daemon(){
  GAMEPANEL_PANEL_URL=ws://127.0.0.1:18093/daemon/connect GAMEPANEL_NODE_TOKEN=$TOK GAMEPANEL_SFTP_ADDR=off GAMEPANEL_DATA_DIR=$T/dd daemon >>$T/daemon.log 2>&1 &
  DP=$!
}
start_daemon; sleep 1
c -d "{\"name\":\"iperf\",\"docker_images\":{\"a\":\"$IPERF\"},\"startup\":\"exec iperf3 -s -p \$SERVER_PORT\",\"config\":{\"stop\":\"^C\"}}" $B/eggs/import >/dev/null

new_server(){ c -d "{\"name\":\"$1\",\"node_id\":1,\"egg_id\":1,\"memory_mb\":256,\"bandwidth_mbit\":$2}" $B/servers | python3 -c 'import sys,json;print(json.load(sys.stdin)["uuid"])'; }
state(){ c $B/servers/$1 | python3 -c 'import sys,json;print(json.load(sys.stdin)["state"])'; }
port(){ c $B/servers/$1 | python3 -c 'import sys,json;print(json.load(sys.stdin)["port"])'; }
power(){ c -d "{\"action\":\"$2\"}" $B/servers/$1/power >/dev/null; }
wait_state(){ for _ in $(seq 120); do [ "$(state $1)" = "$2" ] && return 0; sleep 0.5; done; return 1; }
ip_of(){ docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' gp-$1; }
pid_of(){ docker inspect -f '{{.State.Pid}}' gp-$1; }
qdisc(){ nsenter --target "$(pid_of $1)" --net -- tc qdisc show dev eth0; }
# Mbit/s the receiving side measured over 6 s (the first second, a TCP
# slow start and token bucket burst, is left out). $3 = -R: server sends.
mbit(){
  docker run --rm $IPERF -c "$(ip_of $1)" -p "$(port $1)" -t 6 -O 1 ${3:-} -J 2>/dev/null |
    python3 -c 'import sys,json;print(round(json.load(sys.stdin)["end"]["sum_received"]["bits_per_second"]/1e6,1))' 2>/dev/null || echo 0
}
within(){ python3 -c "import sys;v=float('$1');sys.exit(0 if $2<=v<=$3 else 1)"; }
lo=$(python3 -c "print(round($LIMIT*0.8,1))"); hi=$(python3 -c "print(round($LIMIT*1.1,1))")

echo "baseline: unlimited server"
F=$(new_server free 0); power $F start
check "unlimited server runs" 'wait_state $F running'
sleep 1
FREE_UP=$(mbit $F); FREE_DOWN=$(mbit $F x -R)
echo "       unlimited: receives $FREE_UP Mbit/s, sends $FREE_DOWN Mbit/s"
check "baseline is well above the limit (so the limit is what we measure)" 'within $FREE_UP $((LIMIT*5)) 1e9 && within $FREE_DOWN $((LIMIT*5)) 1e9'
check "unlimited server has no shaping" '! qdisc $F | grep -q tbf'
power $F kill

echo "limited server: $LIMIT Mbit/s each way"
U=$(new_server limited $LIMIT); power $U start
check "limited server runs" 'wait_state $U running'
sleep 1
check "token bucket on eth0" 'qdisc $U | grep -q "tbf .*rate ${LIMIT}Mbit"'
check "incoming traffic redirected to an ifb device and shaped there" 'nsenter --target "$(pid_of $U)" --net -- tc filter show dev eth0 ingress | grep -qi "redirect to device gp-ifb" && nsenter --target "$(pid_of $U)" --net -- tc qdisc show dev gp-ifb | grep -q "tbf .*rate ${LIMIT}Mbit"'
check "no warning on the console" '! docker logs gp-$U 2>&1 | grep -q "could not be applied"'
UP=$(mbit $U); DOWN=$(mbit $U x -R)
echo "       limited: receives $UP Mbit/s, sends $DOWN Mbit/s (expect $lo..$hi)"
check "download into the server is held to the limit" 'within $UP $lo $hi'
check "upload from the server is held to the limit" 'within $DOWN $lo $hi'

echo "the limit survives what recreates the network namespace"
power $U restart; sleep 2
check "restart: running again" 'wait_state $U running'
check "restart: shaped again" 'qdisc $U | grep -q "tbf .*rate ${LIMIT}Mbit"'
docker kill gp-$U >/dev/null; sleep 3
check "crash: restarted by the daemon" 'wait_state $U running'
check "crash: shaped again" 'qdisc $U | grep -q "tbf .*rate ${LIMIT}Mbit"'
AFTER_UP=$(mbit $U); AFTER_DOWN=$(mbit $U x -R)
check "crash: still held to the limit (receives $AFTER_UP, sends $AFTER_DOWN Mbit/s)" 'within $AFTER_UP $lo $hi && within $AFTER_DOWN $lo $hi'
kill $DP; sleep 1; start_daemon; sleep 2
check "daemon restart: server still running" 'wait_state $U running'
check "daemon restart: still shaped" 'qdisc $U | grep -q "tbf .*rate ${LIMIT}Mbit"'

echo "without tc the server starts unlimited and says so"
power $U kill; wait_state $U offline
mv /sbin/tc /sbin/tc.off 2>/dev/null || mv /usr/sbin/tc /usr/sbin/tc.off
power $U start
check "starts anyway" 'wait_state $U running'
check "console explains" 'sleep 1; docker logs gp-$U 2>&1 | grep -q "could not be applied" || grep -q "could not apply the bandwidth limit" $T/daemon.log'
mv /sbin/tc.off /sbin/tc 2>/dev/null || mv /usr/sbin/tc.off /usr/sbin/tc

[ $fail = 0 ] && echo "all network limit checks passed" || { echo "--- daemon log"; tail -30 $T/daemon.log; }
exit $fail
