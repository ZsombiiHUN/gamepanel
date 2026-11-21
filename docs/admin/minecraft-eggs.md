# Minecraft eggs

GamePanel ships its own eggs for Minecraft in [`eggs/minecraft`](https://github.com/ZsombiiHUN/gamepanel/tree/main/eggs/minecraft). Import them like any other egg (see [Eggs](./eggs)). Compared with the community Minecraft eggs, they:

- **Size the JVM for the container.** The heap is the memory limit minus room for everything outside it (metaspace, JIT code, thread stacks, network buffers), so the kernel does not kill the server when it gets busy. Community eggs usually set `-Xmx` to the whole limit.
- **Use tuned garbage collection.** G1 with Aikar's flags, or the flags Paper recommends for the installed version. Generational ZGC for heaps of 12 GB or more on Java 21+. Compact object headers on Java 25, which need about 10–20% less memory for the same world.
- **Set the port themselves.** Before each start, the server's port is written to `server.properties`, `velocity.toml` or `config.yml`. It always matches the port the panel assigned, so you never edit it by hand.
- **Check every download** against the checksum its project publishes, and refuse a Java version the server cannot run on with a message that names the image to pick.

| Egg | What it is | Java images |
|---|---|---|
| **Paper** | Fast server with plugin support. The best default | 25, 21, 17, 11, 8 |
| **Purpur** | Paper fork with more gameplay settings | 25, 21, 17, 11, 8 |
| **Folia** | Paper fork that runs world regions on separate threads; most plugins do not support it | 25, 21, 17 |
| **Vanilla** | Mojang's official server | 25, 21, 17, 11, 8 |
| **Fabric** | Lightweight mod loader | 25, 21, 17, 11, 8 |
| **Forge** | The classic mod loader | 25, 21, 17, 11, 8 |
| **NeoForge** | Modern Forge fork (Minecraft 1.20.2 and newer) | 25, 21, 17 |
| **Velocity** | Modern proxy for a network of servers | 25, 21, 17 |
| **BungeeCord** | The original proxy; prefer Velocity for new networks | 25, 21, 17 |

## Picking the Java image

The newest Java is the default and is right for current Minecraft. Older versions need an older Java:

| Minecraft | Java image |
|---|---|
| 1.20.5 and newer | 21 or 25 (26.x needs 25) |
| 1.17 – 1.20.4 | 17 |
| 1.16.5 and older | 8 (Paper and Purpur 1.16.5 also run on 11) |

If the image is too old for the installed server, it does not start: the console says which image to pick, and a banner above it offers to switch to that image and start again in one click. If the image is newer than the Java that Mojang shipped with an older version (Java 17 or earlier), the console shows a warning, because old mods often break on new Java. Change the image under **Startup**, then start again.

## In the panel

A server made from one of these eggs gets more than the usual tabs. The eggs declare Pterodactyl's `eula` and `java_version` features and GamePanel's own `minecraft` (or `minecraft_proxy`), so a community egg that declares `eula` gets the console banners too.

- **Accurate state.** The server shows *Starting* until it prints its "Done" line, then *Running*. Without this, it would look ready while the world is still loading.
- **Console banners.** When the server stops because the EULA is not accepted or its Java is too old, a banner above the console offers the fix: *I accept, start the server*, or *Use Java 21 and start*. These refusals end with exit code 0, as the vanilla server does for the EULA, so they are not counted as crashes and the server is not restarted for them.
- **A Minecraft tab** with:
  - **Status**: what players see in their server list (MOTD with colours, icon, players online, version), from a ping every 10 seconds. Operators can op, kick or ban a listed player, save the world, make it day, clear the weather, or message everyone.
  - **Players**: the whitelist, operators, banned players and banned IPs. While the server runs, changes go through its own commands. While it is stopped, the panel edits the JSON files and looks up each player's UUID with Mojang, or derives the offline UUID in offline mode.
  - **Server settings**: the common `server.properties` settings as a form, with a preview of the MOTD, checks on numbers, and every other key under *All settings*. Comments and unknown keys are kept. The port is locked because the panel sets it.
  - **Version**: switch to any release the egg's project offers (Paper, Purpur, Folia, Vanilla, Fabric, Forge, NeoForge, Velocity). This reinstalls the server software and keeps worlds, plugins and configs.
  - **World**: archive the world as a .zip (after saving it, if the server runs), or reset it with an optional new seed.
  - **Server icon**: upload any image; it is cropped and scaled to the 64×64 `server-icon.png`.

What each section can change follows the user's permissions: lists need file access (or console access while running), versions need startup and reinstall rights.

## Variables

| Variable | Default | Meaning |
|---|---|---|
| Accept the Minecraft EULA (`EULA`) | `false` | Set to `true` to accept the [Minecraft EULA](https://aka.ms/MinecraftEULA). The server does not start until you do. Not on proxies |
| Minecraft version (`MINECRAFT_VERSION`) | `latest` | A version such as `1.21.11`. On Velocity, the Velocity version |
| Build (`BUILD_NUMBER`) | `latest` | Paper, Purpur, Folia and Velocity: the newest stable build, or a build number |
| Loader version (`LOADER_VERSION`) | `latest` | Fabric, Forge and NeoForge. Forge defaults to `recommended`; `latest` takes the newest build |
| Garbage collector (`JVM_GC`) | `auto` | `auto`, `g1` or `zgc` |
| Extra JVM flags (`JVM_EXTRA_FLAGS`) | empty | Added last, so they override GamePanel's flags. Do not set `-Xmx`; it follows the memory limit |

The version variables take effect when you **reinstall** the server. Reinstalling replaces the server jar and libraries but keeps worlds, configs, plugins and mods. BungeeCord always installs the latest build.

## Memory

Give a server at least **768 MB** and a proxy at least **448 MB**. From the memory limit, a server keeps 400 MB plus 10% outside the heap; a proxy keeps 25% (at least 192 MB). For example:

| Memory limit | Heap |
|---|---|
| 2 GB | 1.4 GB |
| 4 GB | 3.2 GB |
| 8 GB | 6.8 GB |
| 16 GB | 14 GB (ZGC on Java 21+) |

The heap is reserved in full at start (`-Xms` equals `-Xmx`, and every page is touched), so a server that starts will not run out of memory later, and the server never pauses to grow its heap.

The first two console lines show what was chosen:

```
[gamepanel] Java 25 | limit 4096 MB | heap 3287 MB | GC g1
[gamepanel] java -Xms3287M -Xmx3287M ...
```

## The images

The eggs run on `ghcr.io/zsombiihun/gamepanel-java:<8|11|17|21|25>`. These are Eclipse Temurin JREs on Ubuntu, for amd64 and arm64, rebuilt weekly for security updates. Each image contains two scripts, both in [`images/java`](https://github.com/ZsombiiHUN/gamepanel/tree/main/images/java):

- `gp-install` downloads the server during install and records how to start it in `.gamepanel/`.
- `gp-java` is the startup command. It picks the JVM flags, writes the port, checks the EULA and starts the server.

Each server's install uses the Java 25 image to run installers, whatever image the server itself runs on.

If a node cannot pull an image (an offline node, a registry outage, or an image built on the node itself), the daemon uses the copy the node already has and logs a warning.
