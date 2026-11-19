# Command line

`panel` with no arguments runs the panel. It also has a few commands for the panel host, for jobs you cannot do from the browser.

Every command opens the same database as the running panel, so it needs the same settings: at least `GAMEPANEL_DB`, and `GAMEPANEL_SECRET_KEY` if you use it. Run commands as the panel's user. With the setup from [Installing the panel](/panel/installing):

```sh
sudo -u gamepanel sh -c 'set -a; . /etc/gamepanel/panel.env; exec /usr/local/bin/panel <command> <args>'
```

The commands are safe to run while the panel is running.

::: warning
Without `GAMEPANEL_DB`, a command opens (and creates) `./panel.db` in the current folder, and a `panel.db.key` next to it.
:::

## `panel create-admin <email>`

Makes an administrator account with a random password and prints the password **once**, to your terminal only. It is never logged or stored in the clear.

```txt
Admin created.
  email:    you@example.com
  password: 3vQx...
Change it after signing in.
```

Use it for the first account, or when every admin is locked out.

## `panel reset-link <email>`

Prints a one-time password reset link for an account, valid for 1 hour. This is how to reset a password on a panel without [email](/panel/email). The link starts with `GAMEPANEL_PUBLIC_URL`; without it, it starts with `<panel address>` for you to fill in.

```txt
Password reset link for you@example.com (valid for 1h0m0s, works once):
https://panel.example.com/reset#token=...
```

Using the link signs the account out everywhere.

## `panel reset-2fa <email>`

Turns off authenticator two-factor for an account and deletes its recovery codes. This is the last way back for someone who lost their authenticator **and** their recovery codes. It is recorded in the audit log as `totp.reset`, by `cli`.

Check who is asking before you run it: it removes a protection. Passkeys are not touched.

## `panel --addr <address>`

Runs the panel on another address than `GAMEPANEL_ADDR`.
