# Account security and OWASP ASVS 5.0.0

This lists the authentication and session controls GamePanel implements, the
[OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/releases/tag/v5.0.0_release)
requirement each one was checked against, and the test that covers it.
Requirement IDs use the version-qualified form `v5.0.0-<chapter>.<section>.<requirement>`.

This is not an ASVS assessment of the whole application. It covers the account
controls only, and even there some Level 2 requirements are not met yet (see
[Known gaps](#known-gaps)).

## Controls

| Control | ASVS 5.0.0 | Level | Where | Test |
|---|---|---|---|---|
| Passwords 12 to 256 characters, counted in characters, no composition rules | v5.0.0-6.2.1, v5.0.0-6.2.5, v5.0.0-6.2.9 | L1, L1, L2 | `store.ValidatePassword` | `store.TestValidatePassword` |
| Passwords checked against ~30,000 common and leaked passwords | v5.0.0-6.2.4, v5.0.0-6.2.12 | L1, L2 | `store/commonpw.go` | `store.TestCommonPasswordListLoaded`, `api.TestCommonPasswordRefused` |
| Passwords built from the account's email or the site name are refused | v5.0.0-6.2.11 | L2 | `store.ValidatePassword` | `store.TestValidatePassword` |
| Changing the password needs the current one | v5.0.0-6.2.3 | L1 | `POST /auth/password` | `api.TestChangePasswordRevokesOtherSessions` |
| Changing the password, two-factor settings or creating an API key needs the password again, plus a code when two-factor is on | v5.0.0-7.5.1 | L2 | `api/auth.go`, `api/mfa.go` | `api.TestPasswordChangeNeedsSecondFactor`, `api.TestAPIKeyNeedsSecondFactor` |
| Other sessions are signed out after a password change or turning two-factor off | v5.0.0-7.4.3 | L2 | `store.ChangePassword`, `totpDisable` | `api.TestChangePasswordRevokesOtherSessions`, `api.TestPasswordChangeNeedsSecondFactor` |
| Two factors can be required for administrators or everyone (`GAMEPANEL_REQUIRE_MFA`). A session that signed in with one factor can only set up a second; one whose account already has a second factor can only sign out; API keys of accounts without a second factor stop working; the last second factor cannot be removed | v5.0.0-6.3.3 | L2 | `api/mfapolicy.go` | `api.TestRequireMFAHoldsSessionsToEnrolment`, `api.TestRequireMFAForAdminsOnly`, `api.TestRequireMFABlocksKeysWithoutSecondFactor`, `api.TestRequireMFAEnrolledAccountMustUseItsFactor`, `store.TestSessionMFAMigration` |
| `Strict-Transport-Security` with `includeSubDomains` (and optionally `preload`) when `GAMEPANEL_HSTS` is `subdomains` or `preload` | v5.0.0-3.4.1 | L2 | `api.middleware` | `api.TestHSTSSetting` |
| Login rate-limited per IP and per account; wrong authenticator codes add a doubling delay (up to 24 h) that is kept in the database and checked atomically | v5.0.0-6.3.1 | L1 | `api/limiter.go`, `store.verifyTOTPAttempt` | `api.TestSecondFactorLockoutSurvivesRestart`, `store.TestTOTPLockoutHoldsUnderConcurrency`, `store.TestMFALockSchedule` |
| Authenticator codes and recovery codes work once | v5.0.0-6.5.1 | L2 | `store.verifyTOTPAttempt`, `store.VerifySecondFactor` | `store.TestVerifyTOTPWindowAndReplay`, `store.TestTOTPLockoutHoldsUnderConcurrency`, `store.TestRecoveryCodesAreSingleUse` |
| TOTP secrets and recovery codes come from a CSPRNG | v5.0.0-6.5.3 | L2 | `store.BeginTOTP`, `store.NewRecoveryCodes` | `store.TestRecoveryCodeStrength` |
| Recovery codes have 120 bits, so a SHA-256 hash is enough to store them. Codes from before 120 bits still work once each, and the account page asks their owner to replace them | v5.0.0-6.5.2 | L2 | `store.NewRecoveryCodes`, migration 00115 | `store.TestRecoveryCodeStrength`, `store.TestLegacyRecoveryCodes`, `api.TestRecoveryCodesThroughTheAPI` |
| TOTP uses 30-second steps | v5.0.0-6.5.5 | L2 | `store.verifyTOTP` | `store.TestTOTPRFC6238Vectors`, `store.TestVerifyTOTPWindowAndReplay` |
| Passwords stored with Argon2id (t=3, m=64 MiB, p=4) | v5.0.0-11.4.2 | L2 | `store/models.go` | — |
| Session tokens: 256 random bits, stored only as a hash, checked on the server | v5.0.0-7.2.1, v5.0.0-7.2.3 | L1 | `store.NewToken` | — |
| A new session token at each sign-in | v5.0.0-7.2.4 | L1 | `store.NewSession` | — |
| Signing out ends the session on the server | v5.0.0-7.4.1 | L1 | `logout` | `api.TestCookieAndHeaderHardening` |
| Users can see their live sessions (device, address it signed in from and was last used from, sign-in time and method, last activity) and sign out any one of them, or all but the current one. Only a browser session can do this, not an API key | v5.0.0-7.5.2 | L2 | `api/sessions.go`, `store/sessions.go` | `api.TestSessionListAndRevoke`, `api.TestSessionListNeedsBrowserSession`, `store.TestSessionListIsPerUserAndSkipsDeadSessions`, `store.TestSessionDetailsMigration` |
| Sessions end after 30 minutes without use, and 12 hours after sign-in however busy. Sessions from before the 12-hour limit were cut to at most 12 hours from the upgrade | v5.0.0-7.3.1, v5.0.0-7.3.2, v5.0.0-7.1.1 | L2 | `store.UserBySession`, migration 00010 | `api.TestSessionIdleAndAbsoluteTimeout`, `store.TestSessionUpgradeMigration` |
| Over HTTPS the session cookie is `__Host-gp_session`, `Secure`, `HttpOnly`, `SameSite=Lax`; the old `gp_session` name works over HTTPS only once, for sessions from before the upgrade | v5.0.0-3.3.1, v5.0.0-3.3.2, v5.0.0-3.3.3, v5.0.0-3.3.4 | L1–L2 | `api/cookie.go` | `api.TestCookieAndHeaderHardening`, `api.TestLegacyCookieMigration` |
| API answers are sent with `Cache-Control: no-store` | v5.0.0-14.3.2 | L2 | `api.middleware` | `api.TestCookieAndHeaderHardening` |

The session limits follow the NIST SP 800-63B (revision 3) AAL2 reauthentication
limits: 12 hours absolute, 30 minutes of inactivity. Both can be changed with
`GAMEPANEL_SESSION_MAX` and `GAMEPANEL_SESSION_IDLE`.

## Known gaps

These Level 2 requirements are not met yet, or not by default:

- **v5.0.0-3.4.1 (HSTS), by default**: without `GAMEPANEL_HSTS=subdomains`, `Strict-Transport-Security` covers only the panel's own host, because `includeSubDomains` affects every subdomain of its domain.
- **v5.0.0-6.3.3 (multi-factor required), by default**: two factors are only required with `GAMEPANEL_REQUIRE_MFA`. Single sign-on counts as one factor unless the account's authenticator code is also given, since the panel cannot see how the provider checked the person. An account with no second factor yet can set one up with its password alone, the usual bootstrapping gap of required enrolment.
- **v5.0.0-6.5.2 (lookup secrets), for old codes**: recovery codes made before the upgrade to 120 bits have 80 bits and keep working until they are used or replaced. Their owners see a request to replace them on the account page.
