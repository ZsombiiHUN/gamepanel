# Sign-in and security

Everything about your own account is on the **Account** page.

## Ways to sign in

| Method | Notes |
|---|---|
| **Email and password** | Passwords are 12 to 256 characters. Common and leaked passwords, and ones built from your email or the site's name, are refused |
| **Passkey** | Sign in with a fingerprint, face or device PIN. No password and no second factor needed, and it cannot be phished |
| **Single sign-on** | GitHub, Discord or your organisation's provider, if the panel offers them. Link them under **Sign-in methods** first |

## Passkeys

Add one under **Account → Passkeys**. You can add several (phone, laptop, security key) and name them. A passkey on its own is enough to sign in: it already proves you have the device and can unlock it.

Passkeys need the panel to be reached by a domain name over HTTPS. A passkey whose signature counter goes backwards, a sign that it was copied, is refused.

## Two-factor authentication

Turn on an authenticator app (Google Authenticator, Aegis, 1Password, Bitwarden ...) under **Account → Two-factor**:

1. Scan the QR code, or type the secret into the app.
2. Enter the code it shows to confirm.
3. **Save the recovery codes.** Each works once, in place of a code, if you lose the app.

You can see how many recovery codes are left and make new ones (which needs your password and a code). Turning two-factor off needs your password and a code.

After 5 wrong codes in a row, each further try has to wait: 1 minute, then 2, 4 and so on, up to 24 hours. A recovery code still works while you wait.

Your panel may **require** a second factor for your account. Until you set one up, you can only use the Account page.

### Lost your authenticator?

Use a recovery code, or a passkey. If you have neither, an admin can turn two-factor off for you from the panel host.

## Sessions

**Account → Sessions** lists where you are signed in. End any of them, or all except this one. You are signed out after 30 minutes without activity, and 12 hours after signing in, whatever you do (your panel may use other times).

Changing your password signs out every other session. Resetting it signs out all of them.

## Sensitive changes

Changing your password, two-factor settings or creating an API key asks for your password again, and a code if two-factor is on, even though you are signed in.

## API keys

See [API overview → Authentication](/api/overview#authentication). API keys are also your SFTP password; see [SFTP](/nodes/sftp).
