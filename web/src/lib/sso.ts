/** What went wrong in a single sign-on round trip, in words. */
const FAILED = "The sign-in did not work. Try again or use your password.";
export const SSO_ERRORS: Record<string, string> = {
  not_linked: "That sign-in is not linked to an account here. Sign in with your password, then link it on your Account page.",
  email_taken: "An account with that email already exists. Sign in with your password, then link this sign-in on your Account page.",
  unverified: "The provider has not verified that email address, so it cannot make an account.",
  taken: "That sign-in is already linked to another account.",
  invite: "The invitation does not match that sign-in, or it has expired.",
  expired: "The sign-in took too long or was started in another browser. Try again.",
  cancelled: "The sign-in was cancelled.",
  unavailable: "The sign-in provider cannot be reached right now. Try again later or use your password.",
  failed: FAILED,
};

export const ssoError = (code?: string) => (code ? (SSO_ERRORS[code] ?? FAILED) : null);

export const ssoStart = (id: string, q: Record<string, string> = {}) => {
  const qs = new URLSearchParams(q).toString();
  return `/api/auth/sso/${encodeURIComponent(id)}/start${qs ? "?" + qs : ""}`;
};
