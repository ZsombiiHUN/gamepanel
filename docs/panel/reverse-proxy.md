# Reverse proxy and HTTPS

The panel speaks plain HTTP. Put a reverse proxy in front of it to handle HTTPS. The proxy must:

1. **pass websockets** (consoles in the browser, and every node's connection under `/daemon/`);
2. **keep the `Host` header**, which the panel checks against the `Origin` of every change request;
3. **send `X-Forwarded-For` and `X-Forwarded-Proto`**;
4. **not buffer or cap request bodies**, because uploads and backups stream through the panel;
5. **allow long-lived connections**: a node stays connected for days.

Then tell the panel to trust the proxy:

```ini
GAMEPANEL_TRUSTED_PROXIES=127.0.0.1,::1
```

Only then does the panel believe `X-Forwarded-Proto: https`. That makes it name the session cookie `__Host-gp_session`, mark it `Secure`, and send HSTS. It is also when the panel believes `X-Forwarded-For`, which rate limits and the audit log need for real client addresses. The panel reads `X-Forwarded-For` right to left and stops at the first address that is not a trusted proxy, so addresses a client forges at the front are ignored.

::: warning
Without `GAMEPANEL_TRUSTED_PROXIES`, every request looks like it comes from the proxy itself. All users then share one rate limit, and the audit log shows `127.0.0.1` for everyone.
:::

## Caddy (recommended)

[Caddy](https://caddyserver.com) gets and renews certificates by itself and does everything above by default.

`/etc/caddy/Caddyfile`:

```txt
panel.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

```sh
sudo systemctl reload caddy
```

That is all. Caddy keeps the `Host` header, sets `X-Forwarded-For` and `X-Forwarded-Proto`, passes websockets and does not cap bodies.

## nginx

Get a certificate first, for example with [certbot](https://certbot.eff.org): `sudo certbot certonly --nginx -d panel.example.com`.

`/etc/nginx/sites-available/gamepanel.conf`:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    listen [::]:80;
    server_name panel.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name panel.example.com;

    ssl_certificate     /etc/letsencrypt/live/panel.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/panel.example.com/privkey.pem;

    # Uploads and backups stream through the panel: no size cap, no buffering.
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;

        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        $connection_upgrade;

        # Nodes and consoles keep websockets open; nginx's default closes
        # a connection after 60 s without traffic.
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
    }
}
```

```sh
sudo ln -s /etc/nginx/sites-available/gamepanel.conf /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

## Cloudflare in front

If the domain is proxied through Cloudflare (orange cloud):

- Set SSL/TLS mode to **Full (strict)**, with a real certificate on your server (Caddy's, or a Cloudflare origin certificate).
- **Websockets** work through Cloudflare.
- **Request bodies are capped** by Cloudflare (100 MB on the Free and Pro plans). Large uploads through the browser will fail; use [SFTP](/nodes/sftp) for those, which does not go through Cloudflare.
- The direct peer of your proxy is now a Cloudflare address. For real client addresses, your proxy must take them from Cloudflare (for example nginx's `real_ip` module with Cloudflare's ranges and `CF-Connecting-IP`), or add [Cloudflare's IP ranges](https://www.cloudflare.com/ips/) to `GAMEPANEL_TRUSTED_PROXIES` along with your local proxy.
- Nodes connect through Cloudflare too (`wss://panel.example.com/daemon/connect`). Cloudflare may close long-lived websockets during its own maintenance; the daemon reconnects by itself within seconds.

## Without a proxy

If something else in front terminates TLS in a way the panel cannot see, you can force secure cookies:

```ini
GAMEPANEL_COOKIE_SECURE=1
```

The panel then always behaves as if browsers came over HTTPS. Use this only when they really always do.

When the panel listens on anything but loopback, has no trusted proxies and no `GAMEPANEL_COOKIE_SECURE`, it warns at start: session cookies would not be marked `Secure`.

## HSTS

Over HTTPS, the panel sends `Strict-Transport-Security` for its own host. `GAMEPANEL_HSTS=subdomains` adds `includeSubDomains`; `preload` also adds `preload` with a two-year `max-age`. Both affect **every subdomain** of the panel's domain, so use them only when all of those serve HTTPS.

## Checking it

1. Open `https://panel.example.com` and sign in.
2. In the browser's developer tools, the session cookie should be `__Host-gp_session`, marked `Secure`.
3. Open a server's console: it should stream. If it connects and then drops after a minute, your proxy closes idle websockets (see `proxy_read_timeout`).
4. Upload a file larger than 100 MB. If it fails at once, something caps bodies.
5. **Admin → Audit log** should show your real address for the sign-in, not `127.0.0.1`.
