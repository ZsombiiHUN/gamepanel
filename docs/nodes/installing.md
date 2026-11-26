# Installing a node

A node is a machine that runs game servers. It needs Docker and the GamePanel daemon. The panel's own machine can be a node too.

The daemon **connects out** to the panel. You do not open any port on the node for the panel; only the game ports and SFTP.

## 1. Check the machine

```sh
uname -r                 # a recent kernel
systemd-detect-virt      # "kvm", "vmware", "microsoft", "none" are fine;
                         # "lxc" or "openvz" need Docker nesting and are not tested
```

See [Requirements](/getting-started/requirements#a-node).

## 2. Install Docker

Use Docker Engine from Docker's own packages. The quickest way:

```sh
curl -fsSL https://get.docker.com | sudo sh
sudo systemctl enable --now docker
sudo docker run --rm hello-world
```

or follow [Docker's install guide](https://docs.docker.com/engine/install/) for your distribution.

For [network limits](/nodes/network-limits), also make sure these are present (most distributions have them already):

```sh
sudo apt install iproute2 util-linux    # Debian, Ubuntu
sudo dnf install iproute util-linux     # Fedora, Rocky, Alma
sudo modprobe ifb && echo ifb | sudo tee /etc/modules-load.d/ifb.conf
```

## 3. Create the node in the panel

1. In the panel, open **Nodes → Create node**.
2. Fill in:

   | Field | Meaning |
   |---|---|
   | Name | Anything that helps you recognise it |
   | Port range start, end | The ports game servers on this node may get, for example `25565`–`25665`. Each server gets one primary port from this range, plus any extra ones it is allowed |
   | Memory capacity (MB) | What servers on it may use in total; `0` = not tracked |
   | Disk capacity (MB) | The same for disk; `0` = not tracked |
   | Over-allocate (%) | Let limits add up to the capacity plus this much |

3. Copy the **token**. It is shown **only once**. If you lose it, delete the node and create it again (only possible while it has no servers).

More in [Nodes and capacity](/admin/nodes).

## 4. Install the daemon

::: tip The quick way
The installer does steps 2 and 4 to 6 on this page: it installs (or starts) Docker if needed, the daemon, its settings and its service, then waits until the panel accepts the node's token. Run it and pick **A node**; it asks for the panel's domain and the token:

```sh
curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh | sudo bash
```

Or without questions:

```sh
curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh \
  | sudo bash -s -- node --panel panel.example.com --token <the token> --yes
```

Replacing Wings on a node you imported from Pterodactyl? Add `--adopt-wings /var/lib/pterodactyl/volumes` to keep its servers' files. See [Coming from Pterodactyl](/guides/from-pterodactyl#_3-switch-each-node).
:::

Download the daemon for your CPU from the [releases page](https://github.com/ZsombiiHUN/gamepanel/releases) and check it, the same way as [the panel](/panel/installing#_1-get-the-binaries) but with `gamepanel-daemon_${V}_linux_$A.tar.gz`. Use the **same version as the panel**. Then:

```sh
tar xzf gamepanel-daemon_${V}_linux_$A.tar.gz
sudo install -m 0755 daemon /usr/local/bin/gamepanel-daemon
sudo install -d -m 0700 /etc/gamepanel /var/lib/gamepanel-daemon
# Game servers run as this user, which cannot log in anywhere.
sudo useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin gamepanel-game
id gamepanel-game   # note the uid and gid for daemon.env
```

Create `/etc/gamepanel/daemon.env`:

```ini
GAMEPANEL_NODE_TOKEN=<the token>
GAMEPANEL_PANEL_URL=wss://panel.example.com/daemon/connect
GAMEPANEL_DATA_DIR=/var/lib/gamepanel-daemon
GAMEPANEL_SFTP_ADDR=0.0.0.0:2022
GAMEPANEL_CONTAINER_USER=<uid>:<gid>   # of gamepanel-game, from id above
```

```sh
sudo chmod 0600 /etc/gamepanel/daemon.env
```

::: danger Use wss://
The default panel address is `ws://127.0.0.1:8080/daemon/connect`, which only makes sense when the daemon runs on the panel's machine. For any other node use **`wss://`** and the panel's public name. Over plain `ws://` the node's token crosses the network unencrypted, and anyone who has it can pose as the node. The daemon does not refuse `ws://` for you yet.
:::

If the node **is** the panel's machine, it can talk to the panel directly: `GAMEPANEL_PANEL_URL=ws://127.0.0.1:8080/daemon/connect`.

## 5. Try it

Run it in the foreground once:

```sh
sudo sh -c 'set -a; . /etc/gamepanel/daemon.env; exec /usr/local/bin/gamepanel-daemon'
```

The node should show as **online** on the panel's **Nodes** page within seconds. Stop it with `Ctrl+C`.

If it does not connect, see [Troubleshooting](/troubleshooting#a-node-stays-offline).

## Run it as a service

The daemon runs as **root**: it hands server folders to the game servers' user, talks to Docker, keeps the servers' [firewall](/nodes/isolation) and enters container network namespaces for bandwidth limits. It needs `iptables`.

`/etc/systemd/system/gamepanel-daemon.service` (also in the archive, as `packaging/systemd/gamepanel-daemon.service`):

```ini
[Unit]
Description=GamePanel daemon
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
EnvironmentFile=/etc/gamepanel/daemon.env
ExecStart=/usr/local/bin/gamepanel-daemon
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now gamepanel-daemon
journalctl -u gamepanel-daemon -f
```

Restarting the daemon does **not** stop game servers: their containers keep running, and the daemon picks them up again.

::: tip After a reboot
When the daemon starts, it starts again every server that was running when the node went down, even if the panel cannot be reached yet. Servers someone stopped or killed, servers that stopped themselves (exit code 0, such as `stop` typed in the console) and servers that crashed and were left stopped stay offline. Set `GAMEPANEL_RESUME=off` to keep every server offline after a restart.

Game server containers have no Docker restart policy of their own: the daemon decides, so crash limits and suspensions still hold.
:::

## Firewall

| Port | Direction | For |
|---|---|---|
| Your game port range, TCP **and** UDP | inbound | Players. Each server port is published for both |
| 2022/tcp (or your `GAMEPANEL_SFTP_ADDR`) | inbound | SFTP, if you use it |
| 443/tcp to the panel | outbound | The daemon's connection |
| 443/tcp to registries, S3, the internet | outbound | Image pulls, install scripts, S3 backups, "pull from URL" |

Docker publishes ports by writing its own firewall rules, which can bypass `ufw`. Check your distribution's notes on Docker and `ufw`/`firewalld` if you rely on them.

## Next

- [Node configuration reference](/nodes/configuration)
- [SFTP](/nodes/sftp)
- [Network limits](/nodes/network-limits)
- [Data directory](/nodes/data-directory)
