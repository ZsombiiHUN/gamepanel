import i18n from "../i18n";
/**
 * A short, human name for a browser's User-Agent string, like "Firefox on
 * Windows", for the session list. Only common browsers and systems are named;
 * anything else falls back to "Unknown browser" or the raw string's product.
 * The order of the checks matters: Edge and Opera also claim to be Chrome,
 * and Chrome claims to be Safari.
 */
export function describeUserAgent(ua: string): string {
  if (!ua.trim()) return i18n.t("device.unknown");
  const browser = /Edg(e|A|iOS)?\//.test(ua)
    ? "Edge"
    : /OPR\/|Opera/.test(ua)
      ? "Opera"
      : ua.includes("SamsungBrowser/")
        ? "Samsung Internet"
        : /Firefox\/|FxiOS\//.test(ua)
          ? "Firefox"
          : /Chrome\/|CriOS\/|Chromium\//.test(ua)
            ? "Chrome"
            : ua.includes("Safari/") && ua.includes("Version/")
              ? "Safari"
              : ua.startsWith("curl/")
                ? "curl"
                : null;
  const os = /iPhone|iPod/.test(ua)
    ? "iPhone"
    : ua.includes("iPad")
      ? "iPad"
      : ua.includes("Android")
        ? "Android"
        : ua.includes("CrOS")
          ? "ChromeOS"
          : ua.includes("Windows")
            ? "Windows"
            : /Mac OS X|Macintosh/.test(ua)
              ? "macOS"
              : ua.includes("Linux")
                ? "Linux"
                : null;
  if (browser && os) return i18n.t("device.browserOn", { browser, os });
  if (browser) return browser;
  if (os) return i18n.t("device.someBrowserOn", { os });
  // Not a browser: show the product token ("python-requests/2.32") trimmed.
  const product = ua.trim().split(/\s/)[0] ?? "";
  return product.length <= 40 ? product : product.slice(0, 39) + "…";
}

/** How a session signed in, from the API's method string. */
export function describeSignIn(method: string): string {
  const [kind, provider] = method.split(":", 2);
  const via = provider || i18n.t("signIn.sso");
  switch (kind) {
    case "password":
      return i18n.t("signIn.password");
    case "password+totp":
      return i18n.t("signIn.passwordTotp");
    case "password+recovery":
      return i18n.t("signIn.passwordRecovery");
    case "passkey":
      return i18n.t("signIn.passkey");
    case "sso":
      return via;
    case "sso+totp":
      return i18n.t("signIn.ssoTotp", { via });
    case "register":
      return i18n.t("signIn.register");
    default:
      return i18n.t("signIn.unknown");
  }
}
