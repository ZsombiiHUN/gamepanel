# Network limits

An admin can give a server a bandwidth limit in Mbit/s (`bandwidth_mbit`, `0` = unlimited). It applies **each way**: to what the server sends and to what it receives.

## How it works

The daemon applies the limit inside the container's own network namespace:

- **Outgoing**: a token bucket (`tbf`) on the container's interface.
- **Incoming**: traffic is redirected through an `ifb` device and shaped there with the same kind of token bucket. If the kernel has no `ifb`, incoming traffic is **policed** instead: packets above the rate are dropped, which is coarser.

It is re-applied whenever the server starts, restarts after a crash, or the daemon restarts.

Measured with iperf3 against a real Docker engine ([`scripts/bandwidth-smoke.sh`](https://github.com/ZsombiiHUN/gamepanel/blob/main/scripts/bandwidth-smoke.sh)): at 10 and 100 Mbit/s both directions hold at about **96% of the limit**, including after a restart, a crash restart and a daemon restart.

## What the node needs

| Need | Package or setting |
|---|---|
| root | The daemon runs as root |
| `tc` and `ip` | `iproute2` (Debian/Ubuntu) or `iproute` (Fedora/RHEL) |
| `nsenter` | `util-linux` |
| `ifb` kernel module, for shaping incoming traffic | `modprobe ifb`; load it at boot with `/etc/modules-load.d/ifb.conf` |

If `tc`, `ip` or `nsenter` are missing, the server **starts without a limit**, and its console says so. Nothing fails silently.

## Daemon in a container

If you run the daemon itself in a container, it needs the host's PID namespace and two capabilities to reach the servers' namespaces:

```sh
docker run --pid=host --cap-add SYS_ADMIN --cap-add NET_ADMIN ...
```

## Changing a limit

`PUT /api/servers/{uuid}/limits` (admin), or the server's limits in the interface. Like other limits, a change to a running server applies at its **next start**.
