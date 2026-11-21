# Eggs

An **egg** says how to run one kind of server: which Docker images it can use, the startup command, the stop command, the variables a user can set, and the install script that downloads the game. GamePanel reads Pterodactyl's egg format (PTDL v1 and v2), which Pelican also uses, so the large community library works.

<Shot name="eggs" alt="The Eggs page: imported eggs with their Docker images and startup commands" />

## Importing an egg

Open **Eggs** (admin) and import a `.json` egg file, or call `POST /api/eggs/import` with the file as the body.

Where to find eggs:

- [pelican-eggs](https://github.com/pelican-eggs): the community collection for games (Minecraft, Steam games, Rust, ARK, Valheim and many more), applications, databases, bots and language runtimes.
- An egg you exported from an existing Pterodactyl or Pelican panel (**Nests → egg → Export**).

## What is imported

| Egg field | Used for |
|---|---|
| `name`, `description` | Shown in the interface |
| `docker_images` (v2), `images` or `docker_image` (v1) | The images a server can use. **The first one is the default**, as in Pterodactyl (for Java eggs that is usually the newest Java) |
| `startup` | The startup command. `{{VAR}}` becomes `${VAR}` (see below) |
| `config.stop` | The stop command sent to the console, or `^C` to send an interrupt. Default `^C` |
| `scripts.installation` | The install script, its container image and entrypoint (`bash` or `ash`) |
| `variables` | Name, description, environment variable, default, `user_viewable`, `user_editable`, and `rules` |

The import is refused when the egg has no name, no startup command or no image, when an image name is not a valid Docker reference, or when it has an install script but no installer image. Variables with malformed names are skipped.

## What is not imported (yet)

These parts of an egg are ignored. Most eggs work without them, but some need a hand:

| Egg field | In Pterodactyl | In GamePanel |
|---|---|---|
| `config.files` | Rewrites config files before each start, e.g. sets `server-port` in Minecraft's `server.properties` to the server's port | **Not done.** Set the port (and anything else the egg would have written) in the game's config file yourself. See [Ports and config files](#ports-and-config-files) |
| `config.startup.done` | A console line that marks the server as started | Not used. A server counts as **running** as soon as its container runs |
| `config.logs` | Log settings | Not used |
| `file_denylist` | Files users may not edit | Not used |
| `features` (e.g. `eula`, `java_version`, `pid_limit`) | Helper prompts in Pterodactyl's interface | Not used. Accept a Minecraft EULA by setting `eula=true` in `eula.txt` |
| `{{config.*}}`, `{{server.*}}` placeholders in startup | Panel values substituted into the command | Not substituted; only the server's own variables are |

### Ports and config files

Each server port is published on the **same number inside the container** as outside. A game must listen on the port it was given:

- Eggs whose startup passes the port on the command line (`-port {{SERVER_PORT}}`, `+port {{SERVER_PORT}}`, `--port ...`), as most Steam game eggs do, work as they are.
- Eggs that rely on `config.files` to write the port into a config file do not. The common case is **Minecraft** (`server.properties`): set `server-port=` to the server's primary port in **Files**, then restart. For proxies such as BungeeCord or Velocity, set the bind address in `config.yml` or `velocity.toml` the same way.

## Variables

Each variable becomes an environment variable in the server's container. In the interface:

- `user_viewable: false` hides it from the owner and subusers (admins still see it).
- `user_editable: false` lets only admins change it.

### Rules

Values are checked against the egg's `rules` when a server is created and whenever a variable changes. Pterodactyl uses Laravel validation rules, such as `required|string|max:20` or `nullable|integer|between:1,64`. GamePanel checks the ones eggs use:

| Rule | Meaning |
|---|---|
| `required`, `nullable` | Must have a value, or may be empty |
| `integer`, `numeric`, `boolean` | A whole number, any number, or `1`/`0`/`true`/`false` |
| `in:a,b`, `not_in:a,b` | One of, or none of, the listed values |
| `regex:/.../` | Matches the pattern |
| `max:n`, `min:n`, `between:a,b`, `size:n` | The number, for numbers; the length, for text |
| `alpha`, `alpha_num`, `alpha_dash` | Letters; letters and digits; also `-` and `_` |
| `url` | A URL |

A value that breaks a rule is refused with a message naming the variable. Rules GamePanel does not know are ignored, as are patterns Go's regular expressions cannot express (look-arounds, back-references), so an egg written for Pterodactyl never makes a value impossible to save.

### Built-in variables

The panel always sets:

| Variable | Value |
|---|---|
| `SERVER_MEMORY` | The server's memory limit in MB |
| `SERVER_PORT` | The server's primary port |

They cannot be overridden through the API, and neither can any variable the egg does not declare (so nobody can set `LD_PRELOAD` and the like).

Pterodactyl also sets `SERVER_IP`, `P_SERVER_UUID`, `P_SERVER_LOCATION`, `P_SERVER_ALLOCATION_LIMIT` and `TZ`. GamePanel does **not**. An egg whose startup uses `{{SERVER_IP}}` gets an empty value; servers should listen on all addresses (`0.0.0.0`), which is what most eggs do anyway.

## Startup and safety

Pterodactyl builds the startup command by pasting variable values into it as text, which has led to command injection and token leaks. GamePanel does not:

- `{{VAR}}` becomes `${VAR}`, and values reach the process **only as environment variables**. The shell expands them without parsing their contents again, so a value like `; rm -rf /` stays a harmless string.
- No panel or node secret is ever substituted into a command or script.
- The image's own `ENTRYPOINT` is replaced by `/bin/sh -c <startup>`. Pterodactyl's "yolk" images ship an entrypoint that re-parses a `STARTUP` variable; GamePanel does not need it, so it is not run.
- The container runs as the owner of the server's folder (uid 1000), with every capability dropped and `no-new-privileges`.

## Install scripts

The install script runs once when a server is created, and again on **Reinstall**:

- in its own container, from the egg's installer image,
- with the server's folder at `/mnt/server` and the script at `/mnt/install/install.sh`,
- with the server's variables in the environment,
- without capabilities, with at least 1 GB of memory and 4096 processes,
- stopped after **30 minutes**.

Its output is the server's console. Afterwards the files are handed to the folder's owner. A non-zero exit (or the 30-minute limit) marks the server **install failed**; it will not start until a reinstall succeeds.

## Docker images

Users with the `startup.docker-image` permission can switch between the images the egg lists. Admins can also set any other image. See [Startup settings](/servers/startup).

## Writing your own egg

Write it in Pterodactyl's format; Pelican's guide to [creating a custom egg](https://pelican.dev/docs/eggs/creating-a-custom-egg) applies, with the differences above. In short:

1. Start from a similar egg.
2. Put the port on the command line with `{{SERVER_PORT}}` instead of relying on `config.files`.
3. Keep the install script idempotent: it runs again on reinstall, over existing files.
4. Test it: create a server, watch the install output, start it, connect.
