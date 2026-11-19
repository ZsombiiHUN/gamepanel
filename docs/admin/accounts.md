# Accounts and sign-up

## Kinds of accounts

| Kind | Can |
|---|---|
| **Administrator** | Everything: all servers, nodes, eggs, database hosts, accounts, webhooks, the audit log |
| **User** | Their own servers, and servers others gave them access to, with the permissions they were given |

Accounts are identified by email address (stored in lower case).

## How accounts are made

| Way | How | Needs |
|---|---|---|
| First admin | [`panel create-admin`](/panel/cli#panel-create-admin-email) on the panel host | Shell access |
| Invitation to a server | The server's owner adds an email under **Access**. Someone without an account gets an invitation link (emailed if the panel can, and shown to pass on) and the access comes with it | Nothing |
| Account invitation | An admin calls `POST /api/invites` | API only for now |
| Created by an admin or shop | `POST /api/users`, with or without a password. Without one, the reply includes a setup link valid for 7 days, also emailed when the panel can | API only for now |
| Open sign-up | Anyone registers on the sign-in page | `GAMEPANEL_SIGNUP=open` |
| Single sign-on | First sign-in with an invitation, or with sign-up open | [Single sign-on](/panel/sso) set up |

Invitations last **7 days**. Pending ones can be listed and withdrawn (`GET`/`DELETE /api/invites`, and per server under **Access**).

::: info
There is no page for managing accounts in the interface yet. Admins use the API (`/api/users`), described in [Creating servers → Accounts](/server-creation#accounts).
:::

## Passwords

- 12 to 256 characters, counted in characters. No composition rules.
- Checked against about 30,000 common and leaked passwords, and refused if built from the account's email or the site's name.
- Stored with Argon2id.
- Changing a password signs out every other session; a reset signs out all of them.

## Requiring two factors

`GAMEPANEL_REQUIRE_MFA=admins` or `all` makes those accounts use a second factor: an authenticator code or a passkey. Until they set one up, they can only reach their account page, and their API keys stop working. We recommend at least `admins`.

## Sessions

- Sessions end after 30 minutes without use and 12 hours after sign-in (`GAMEPANEL_SESSION_IDLE`, `GAMEPANEL_SESSION_MAX`).
- Users see their sessions under **Account** and can end any of them.
- Only a hash of the session token is stored.

## Sign-in limits

| What | Limit |
|---|---|
| Sign-in attempts | 10 per minute per client address |
| Wrong passwords or codes | 20 per 15 minutes per account, then sign-in to that account is refused (`login.blocked`) until older failures leave the window. A successful sign-in clears the count |
| Wrong authenticator codes in a row | 5 free, then a wait that doubles each time (1 min, 2 min, 4 min ...) up to 24 hours. The count survives restarts |

## Deleting an account

`DELETE /api/users/{id}`. An account that still owns servers cannot be deleted, and neither can your own. Its access to other people's servers goes with it, and schedules it saved on those servers are turned off, because they ran with its permissions.

## Locked out?

| Problem | Fix |
|---|---|
| Forgot password, email set up | **Forgot password** on the sign-in page |
| Forgot password, no email | An admin runs [`panel reset-link`](/panel/cli#panel-reset-link-email) |
| Lost authenticator | Use a recovery code, or a passkey if the account has one |
| Lost authenticator and recovery codes | An admin runs [`panel reset-2fa`](/panel/cli#panel-reset-2fa-email) |
| Every admin locked out | Run [`panel create-admin`](/panel/cli#panel-create-admin-email) with a new email |
