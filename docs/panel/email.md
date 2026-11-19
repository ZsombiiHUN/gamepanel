# Email

With email set up, the panel sends:

- **password reset links** ("Forgot password" on the sign-in page), valid for 1 hour and usable once;
- **invitations**, to make an account or to join a server, valid for 7 days;
- **setup links** for accounts a shop or an admin made without a password, valid for 7 days.

Without email, invitations and setup links still work: the panel shows the link so you can pass it on yourself. "Forgot password" is turned off; instead an admin runs `panel reset-link` on the panel host, which prints a reset link (see [Command line](/panel/cli)).

## Setting it up

Add to `/etc/gamepanel/panel.env`:

```ini
GAMEPANEL_PUBLIC_URL=https://panel.example.com
GAMEPANEL_SMTP_HOST=smtp.example.com
GAMEPANEL_SMTP_PORT=587
GAMEPANEL_SMTP_USERNAME=panel@example.com
GAMEPANEL_SMTP_PASSWORD=...
GAMEPANEL_SMTP_FROM=GamePanel <panel@example.com>
```

and restart the panel.

`GAMEPANEL_PUBLIC_URL` is required. Links in emails are built only from it, never from the address a request came in on, so nobody can make the panel email out links to their own site. Without it, the panel warns at start and sends no email.

## TLS

`GAMEPANEL_SMTP_PORT` defaults to the usual port for the TLS mode:

| `GAMEPANEL_SMTP_TLS` | Port | When |
|---|---|---|
| `starttls` (default) | 587 | Most providers |
| `tls` | 465 | Providers that want implicit TLS |
| `none` | 25 | Only a relay on the same machine (for example a local Postfix). Credentials would otherwise cross the network in the clear |

## Providers

Any SMTP service works: your mail host, Amazon SES, Postmark, Mailgun, Brevo, Resend and so on. For good delivery, send from a domain you own and set up SPF, DKIM and DMARC for it as your provider describes.

## Limits

To stop the reset form being used to flood someone's inbox, the panel allows at most **10 reset requests per 15 minutes** per client address and per email address, then answers `429 Too Many Requests`. For an email that has no account, the answer is the same as for one that has, so the form cannot be used to find out who has an account.

## Checking it

1. Sign out, choose **Forgot password**, enter your email.
2. The email should arrive within a minute, with a link starting with your `GAMEPANEL_PUBLIC_URL`.
3. If nothing arrives, look in `journalctl -u gamepanel` for an SMTP error.
