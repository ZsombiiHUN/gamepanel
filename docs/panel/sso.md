# Single sign-on

People can sign in with **GitHub**, **Discord**, or any **OpenID Connect** provider (Google, Microsoft Entra ID, Authentik, Keycloak, Zitadel, Authelia...). You can turn on several at once; each gets a button on the sign-in page.

All of them need `GAMEPANEL_PUBLIC_URL`, because the provider sends people back to an address built from it. Without it, single sign-on stays off and the panel warns at start.

## How accounts are matched

**An identity is never matched to an account by email address.** A provider may let anyone claim any address, so matching on it would let a stranger into someone's account. Instead:

- **Existing accounts link their identity themselves**: sign in as usual, then open **Account → Sign-in methods** and link GitHub, Discord or your provider. After that, the button signs you in. You can unlink it there too.
- **New accounts** are made by single sign-on only when the person has an **invitation**, or when sign-up is **open** (`GAMEPANEL_SIGNUP=open`), and only for a verified email address that has no account yet.

**Two-factor still applies.** A provider proves who someone is, but the panel cannot see how the provider checked them, so single sign-on counts as one factor. If the account has two-factor on, the panel asks for the code after the provider sends them back.

An account made through single sign-on has no password. Its owner can give it one under **Account** (`POST /auth/password/first`).

The flow is the OAuth 2.0 authorization code flow with PKCE. The state is bound to the browser by a short-lived cookie, so nobody can force a sign-in on someone else.

## GitHub

1. On GitHub: **Settings → Developer settings → OAuth Apps → New OAuth App** (or the same under your organisation).
2. **Homepage URL**: `https://panel.example.com`
3. **Authorization callback URL**: `https://panel.example.com/api/auth/sso/github/callback`
4. Create it, then generate a client secret.

```ini
GAMEPANEL_SSO_GITHUB_CLIENT_ID=Ov23li...
GAMEPANEL_SSO_GITHUB_CLIENT_SECRET=...
```

The panel asks for the `read:user` and `user:email` scopes.

## Discord

1. In the [Discord Developer Portal](https://discord.com/developers/applications): **New Application**.
2. Under **OAuth2**, add the redirect `https://panel.example.com/api/auth/sso/discord/callback`.
3. Copy the client ID and reset/copy the client secret.

```ini
GAMEPANEL_SSO_DISCORD_CLIENT_ID=...
GAMEPANEL_SSO_DISCORD_CLIENT_SECRET=...
```

The panel asks for the `identify` and `email` scopes.

## OpenID Connect

Create a confidential client (an "app" or "application" with a client secret) in your provider with:

- **Redirect URI**: `https://panel.example.com/api/auth/sso/oidc/callback`
- **Scopes**: `openid`, `email`, `profile`

```ini
GAMEPANEL_SSO_OIDC_ISSUER=https://auth.example.com/application/o/gamepanel/
GAMEPANEL_SSO_OIDC_CLIENT_ID=...
GAMEPANEL_SSO_OIDC_CLIENT_SECRET=...
GAMEPANEL_SSO_OIDC_NAME=Example SSO
```

The issuer is the address whose `/.well-known/openid-configuration` the panel reads to find the provider's endpoints. Some common ones:

| Provider | Issuer |
|---|---|
| Google | `https://accounts.google.com` |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant id>/v2.0` |
| Authentik | `https://<host>/application/o/<app slug>/` |
| Keycloak | `https://<host>/realms/<realm>` |

`GAMEPANEL_SSO_OIDC_NAME` is the text on the button; without it the button says "single sign-on".

## Troubleshooting

| Problem | Cause |
|---|---|
| No button on the sign-in page | `GAMEPANEL_PUBLIC_URL` is not set, or the panel was not restarted |
| The provider says the redirect URI does not match | The callback in the provider must match `GAMEPANEL_PUBLIC_URL` exactly, including `https` and with no trailing slash before `/api` |
| "That sign-in is not linked to an account here" | Sign in with your password, then link it under **Account → Sign-in methods**. Without an account, ask for an invitation |
| "An account with that email already exists" | The same: sign in with the password and link the identity. Emails never link automatically |
| "The provider has not verified that email address" | The provider says the address is unverified, so the panel will not make an account for it. Verify it at the provider |
| "That sign-in is already linked to another account" | Each identity links to one account. Unlink it from the other account first |
| The round trip fails when started in one browser and finished in another | By design: the state is bound to the browser that started it |
