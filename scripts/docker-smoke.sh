#!/usr/bin/env bash
# Runs the real panel + daemon against the local Docker engine with a tiny
# alpine "game" and checks the whole lifecycle. Needs: docker, curl, python3.
set -u
cd "$(dirname "$0")/.."
T=$(mktemp -d); trap 'kill $PP $DP 2>/dev/null; [ -n "${U:-}" ] && docker rm -f gp-$U >/dev/null 2>&1; rm -rf "$T"' EXIT
fail=0; ok(){ echo "  ok   $*"; }; bad(){ echo "  FAIL $*"; fail=1; }
check(){ if eval "$2"; then ok "$1"; else bad "$1"; fi; }

go build -o $T/panel ./cmd/panel && go build -o $T/daemon ./cmd/daemon || exit 1
export GAMEPANEL_DB=$T/p.db
PW=$($T/panel create-admin admin@example.com | awk '/password:/{print $2}')
GAMEPANEL_ADDR=127.0.0.1:18092 $T/panel >$T/panel.log 2>&1 & PP=$!
sleep 1; B=http://127.0.0.1:18092/api; J=$T/jar
c(){ curl -s -b $J -H 'content-type: application/json' "$@"; }
curl -s -c $J -H 'content-type: application/json' -d "{\"email\":\"admin@example.com\",\"password\":\"$PW\"}" $B/auth/login >/dev/null
TOK=$(c -d '{"name":"local","port_start":25600,"port_end":25610}' $B/nodes | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
GAMEPANEL_PANEL_URL=ws://127.0.0.1:18092/daemon/connect GAMEPANEL_NODE_TOKEN=$TOK GAMEPANEL_SFTP_ADDR=off GAMEPANEL_DATA_DIR=$T/dd $T/daemon >$T/daemon.log 2>&1 & DP=$!
sleep 1
c -d '{"name":"Echo","docker_images":{"a":"alpine:3.20"},"startup":"echo ready $GREETING; echo hi > /home/container/proof.txt; while read l; do [ \"$l\" = stop ] && exit 0; echo got $l; done","config":{"stop":"stop"},"variables":[{"name":"g","env_variable":"GREETING","default_value":"world"}]}' $B/eggs/import >/dev/null
U=$(c -d '{"name":"d1","node_id":1,"egg_id":1,"memory_mb":128,"cpu_pct":50}' $B/servers | python3 -c 'import sys,json;print(json.load(sys.stdin)["uuid"])')
check "server created" '[ -n "$U" ]'
state(){ c $B/servers/$U | python3 -c 'import sys,json;print(json.load(sys.stdin)["state"])'; }
power(){ c -d "{\"action\":\"$1\"}" $B/servers/$U/power >/dev/null; }
wait_state(){ for _ in $(seq 40); do [ "$(state)" = "$1" ] && return 0; sleep 0.5; done; return 1; }

power start
check "reaches running" 'wait_state running'
sleep 1
check "env var reached the process" 'docker logs gp-$U 2>&1 | grep -q "ready world"'
check "limits applied (128MiB, 0.5 CPU, no caps, pids)" '[ "$(docker inspect gp-$U --format "{{.HostConfig.Memory}} {{.HostConfig.NanoCpus}} {{.HostConfig.CapDrop}} {{.HostConfig.PidsLimit}}")" = "134217728 500000000 [ALL] 1024" ]'
check "port published" 'docker port gp-$U | grep -q 25600'
proof(){ c "$B/servers/$U/files/content?path=/proof.txt" | grep -q 'content":"hi'; }
check "container can write its data dir" proof
c -d '{"command":"ping"}' $B/servers/$U/command >/dev/null; sleep 1
check "command reaches stdin" 'docker logs gp-$U 2>&1 | grep -q "got ping"'
check "stats reported" 'sleep 5; c $B/servers/$U | grep -q "\"mem_bytes\""'
power stop
check "graceful stop -> offline" 'wait_state offline'
power start
check "start again" 'wait_state running'
power restart; sleep 2
check "restart returns to running" 'wait_state running'
power kill
check "kill -> offline" 'wait_state offline'
check "delete" '[ "$(curl -s -o /dev/null -w %{http_code} -b $J -X DELETE $B/servers/$U)" = 204 ]'
check "container removed" '! docker inspect gp-$U >/dev/null 2>&1'
check "data removed" '[ ! -e $T/dd/servers/$U ]'

# --- egg install script: runs in a throwaway root container, files end up owned by the game user
c -d '{"name":"Inst","docker_images":{"a":"alpine:3.20"},"startup":"cat /home/container/made-by-install.txt; sleep 1","config":{"stop":"^C"},"scripts":{"installation":{"container":"alpine:3.20","entrypoint":"ash","script":"echo \"from $GREETING\" > /mnt/server/made-by-install.txt\nid -u > /mnt/server/installer-uid.txt\n"}},"variables":[{"name":"g","env_variable":"GREETING","default_value":"installer"}]}' $B/eggs/import >/dev/null
U=$(c -d '{"name":"d2","node_id":1,"egg_id":2,"memory_mb":128}' $B/servers | python3 -c 'import sys,json;print(json.load(sys.stdin)["uuid"])')
check "install finishes -> offline" 'wait_state offline'
check "install script wrote into the data dir" 'grep -q "from installer" $T/dd/servers/$U/made-by-install.txt'
check "installer ran as root, files owned by the game user" '[ "$(cat $T/dd/servers/$U/installer-uid.txt)" = 0 ] && [ "$(stat -c %u $T/dd/servers/$U/made-by-install.txt)" = "$(id -u)" ]'
check "installer container cleaned up" '! docker ps -a --format "{{.Names}}" | grep -q "gp-$U-install"'
power start
check "installed server starts and sees installer output" 'sleep 2; docker logs gp-$U 2>&1 | grep -q "from installer"'
c -X DELETE $B/servers/$U >/dev/null

# --- resource limits reach the container engine, and can be edited while stopped
U=$(c -d '{"name":"lim","node_id":1,"egg_id":1,"memory_mb":128,"swap_mb":64,"cpu_pct":50,"cpuset":"0","pids_limit":200,"disk_mb":50}' $B/servers | python3 -c 'import sys,json;print(json.load(sys.stdin)["uuid"])')
power start
check "limited server runs" 'wait_state running'
insp(){ docker inspect gp-$U --format "$1"; }
check "memory 128M + swap 64M => memory+swap 192M" '[ "$(insp "{{.HostConfig.Memory}} {{.HostConfig.MemorySwap}}")" = "134217728 201326592" ]'
check "cpuset and pids limit applied" '[ "$(insp "{{.HostConfig.CpusetCpus}} {{.HostConfig.PidsLimit}}")" = "0 200" ]'
power stop
check "stops" 'wait_state offline'
check "edit limits while stopped (unlimited swap, 256M)" '[ "$(curl -s -o /dev/null -w %{http_code} -b $J -H "content-type: application/json" -X PUT -d "{\"memory_mb\":256,\"swap_mb\":-1,\"cpu_pct\":0,\"cpuset\":\"\",\"pids_limit\":300,\"io_weight\":0,\"disk_mb\":50,\"backup_limit\":20}" $B/servers/$U/limits)" = 204 ]'
check "new limits are on the recreated container" '[ "$(insp "{{.HostConfig.Memory}} {{.HostConfig.MemorySwap}} {{.HostConfig.PidsLimit}}")" = "268435456 -1 300" ]'
power start
check "restarts with the new limits" 'wait_state running'
# A running server accepts the change; it keeps its container until it next starts.
check "edit limits while running is accepted" '[ "$(curl -s -o /dev/null -w %{http_code} -b $J -H "content-type: application/json" -X PUT -d "{\"memory_mb\":512,\"swap_mb\":0,\"cpu_pct\":0,\"cpuset\":\"\",\"pids_limit\":300,\"io_weight\":0,\"disk_mb\":50,\"backup_limit\":20}" $B/servers/$U/limits)" = 204 ]'
check "running container keeps its limits until restart" '[ "$(insp "{{.HostConfig.Memory}}")" = "268435456" ]'
power restart; sleep 2
check "restart applies the pending limits" 'wait_state running && [ "$(insp "{{.HostConfig.Memory}} {{.HostConfig.MemorySwap}}")" = "536870912 536870912" ]'
c -X DELETE $B/servers/$U >/dev/null
exit $fail
