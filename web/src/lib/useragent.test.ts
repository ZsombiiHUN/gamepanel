import { describe, expect, it } from "vitest";
import { describeSignIn, describeUserAgent } from "./useragent";

describe("describeUserAgent", () => {
  it.each([
    [
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
      "Chrome on Windows",
    ],
    [
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0",
      "Edge on Windows",
    ],
    ["Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0", "Firefox on Linux"],
    [
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
      "Safari on macOS",
    ],
    [
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
      "Safari on iPhone",
    ],
    [
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0 Mobile/15E148 Safari/604.1",
      "Chrome on iPhone",
    ],
    [
      "Mozilla/5.0 (Linux; Android 15; SM-S921B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/27.0 Chrome/125.0 Mobile Safari/537.36",
      "Samsung Internet on Android",
    ],
    [
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 OPR/120.0.0.0",
      "Opera on Windows",
    ],
    ["curl/8.9.1", "curl"],
    ["python-requests/2.32.3", "python-requests/2.32.3"],
    ["", "Unknown device"],
  ])("%s", (ua, want) => expect(describeUserAgent(ua)).toBe(want));
  it("trims a long unknown product", () => {
    expect(describeUserAgent("x".repeat(100))).toHaveLength(40);
  });
});

describe("describeSignIn", () => {
  it.each([
    ["password", "Password"],
    ["password+totp", "Password and authenticator code"],
    ["password+recovery", "Password and recovery code"],
    ["passkey", "Passkey"],
    ["sso:GitHub", "GitHub"],
    ["sso", "single sign-on"],
    ["sso+totp:Discord", "Discord and authenticator code"],
    ["register", "New account"],
    ["", "Unknown"],
  ])("%s", (m, want) => expect(describeSignIn(m)).toBe(want));
});
