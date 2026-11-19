/**
 * A short, human name for a browser's User-Agent string, like "Firefox on
 * Windows", for the session list. Only common browsers and systems are named;
 * anything else falls back to "Unknown browser" or the raw string's product.
 * The order of the checks matters: Edge and Opera also claim to be Chrome,
 * and Chrome claims to be Safari.
 */
export function describeUserAgent(ua: string): string {
  if (!ua.trim()) return "Unknown device";
  const browser = /Edg(e|A|iOS)?\//.test(ua)
    ? "Edge"
    : /OPR\/|Opera/.test(ua)
      ? "Opera"
      : /SamsungBrowser\//.test(ua)
        ? "Samsung Internet"
        : /Firefox\/|FxiOS\//.test(ua)
          ? "Firefox"
          : /Chrome\/|CriOS\/|Chromium\//.test(ua)
            ? "Chrome"
            : /Safari\//.test(ua) && /Version\//.test(ua)
              ? "Safari"
              : /^curl\//.test(ua)
                ? "curl"
                : null;
  const os = /iPhone|iPod/.test(ua)
    ? "iPhone"
    : /iPad/.test(ua)
      ? "iPad"
      : /Android/.test(ua)
        ? "Android"
        : /CrOS/.test(ua)
          ? "ChromeOS"
          : /Windows/.test(ua)
            ? "Windows"
            : /Mac OS X|Macintosh/.test(ua)
              ? "macOS"
              : /Linux/.test(ua)
                ? "Linux"
                : null;
  if (browser && os) return `${browser} on ${os}`;
  if (browser) return browser;
  if (os) return `Browser on ${os}`;
  // Not a browser: show the product token ("python-requests/2.32") trimmed.
  const product = ua.trim().split(/\s/)[0] ?? "";
  return product.length <= 40 ? product : product.slice(0, 39) + "…";
}

/** How a session signed in, from the API's method string. */
export function describeSignIn(method: string): string {
  const [kind, provider] = method.split(":", 2);
  const via = provider || "single sign-on";
  switch (kind) {
    case "password":
      return "Password";
    case "password+totp":
      return "Password and authenticator code";
    case "password+recovery":
      return "Password and recovery code";
    case "passkey":
      return "Passkey";
    case "sso":
      return via;
    case "sso+totp":
      return `${via} and authenticator code`;
    case "register":
      return "New account";
    default:
      return "Unknown";
  }
}
