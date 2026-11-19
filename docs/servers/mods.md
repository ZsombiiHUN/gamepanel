# Mods and plugins

The **Mods** tab searches [Modrinth](https://modrinth.com) and installs mods, plugins and datapacks with one click. It needs the `files.write` permission.

## Searching

Choose what you want (mods, plugins or datapacks), a loader (for mods: Fabric, Forge, NeoForge, Quilt; for plugins: Paper, Purpur, Spigot, Bukkit, Folia, Velocity, BungeeCord) and a game version. The tab guesses the loader from the server's egg (for example a Paper egg presets Paper); change it if the guess is wrong.

Pick a project, then a version. Only versions that match the loader and game version are offered.

## Where files go

| Type | Folder |
|---|---|
| Mod | `/mods/` |
| Plugin | `/plugins/` |
| Datapack | `/world/datapacks/` |

Restart the server to load what you installed.

## Safety

- The panel picks the file itself, from the version you chose, and only accepts files on Modrinth's own CDN.
- The node downloads it and checks its **SHA-512** against Modrinth's before keeping it. A file that does not match is thrown away.
- Installs are recorded in the activity log as `mod.install`.

Dependencies are not installed automatically; install them the same way.

CurseForge is not supported yet.
