/*
 * WebAuthn needs ArrayBuffers where the panel's JSON has base64url strings.
 * Browsers that have the JSON helpers (parse...FromJSON, toJSON) use them;
 * the rest get the same conversion done here.
 */

const fromB64 = (s: string) => {
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "="));
  return Uint8Array.from(b, (c) => c.charCodeAt(0)).buffer;
};
const toB64 = (buf: ArrayBuffer | null) => {
  if (!buf) return null;
  let s = "";
  for (const byte of new Uint8Array(buf)) s += String.fromCharCode(byte);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
};

type Json = Record<string, unknown>;

export const passkeysSupported = () => typeof window !== "undefined" && "PublicKeyCredential" in window && !!navigator.credentials;

function creationOptions(o: Json): CredentialCreationOptions {
  const pk = o.publicKey as Json;
  const user = pk.user as Json;
  return {
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialCreationOptions),
      challenge: fromB64(pk.challenge as string),
      user: { ...(user as unknown as PublicKeyCredentialUserEntity), id: fromB64(user.id as string) },
      excludeCredentials: ((pk.excludeCredentials as Json[] | undefined) ?? []).map((c) => ({ ...(c as unknown as PublicKeyCredentialDescriptor), id: fromB64(c.id as string) })),
    },
  };
}

function requestOptions(o: Json): CredentialRequestOptions {
  const pk = o.publicKey as Json;
  return {
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialRequestOptions),
      challenge: fromB64(pk.challenge as string),
      allowCredentials: ((pk.allowCredentials as Json[] | undefined) ?? []).map((c) => ({ ...(c as unknown as PublicKeyCredentialDescriptor), id: fromB64(c.id as string) })),
    },
  };
}

function credentialJSON(c: PublicKeyCredential): Json {
  const response: Json = { clientDataJSON: toB64(c.response.clientDataJSON) };
  if ("attestationObject" in c.response) {
    const r = c.response as AuthenticatorAttestationResponse;
    response.attestationObject = toB64(r.attestationObject);
    response.transports = typeof r.getTransports === "function" ? r.getTransports() : [];
  } else {
    const r = c.response as AuthenticatorAssertionResponse;
    response.authenticatorData = toB64(r.authenticatorData);
    response.signature = toB64(r.signature);
    response.userHandle = toB64(r.userHandle);
  }
  return { id: c.id, rawId: toB64(c.rawId), type: c.type, response, clientExtensionResults: c.getClientExtensionResults() };
}

/** Make a passkey with the panel's options; returns what the panel wants back. */
export async function createPasskey(options: unknown) {
  const c = (await navigator.credentials.create(creationOptions(options as Json))) as PublicKeyCredential | null;
  if (!c) throw new Error("No passkey was made.");
  return credentialJSON(c);
}

/** Sign with a passkey; returns what the panel wants back. */
export async function getPasskey(options: unknown) {
  const c = (await navigator.credentials.get(requestOptions(options as Json))) as PublicKeyCredential | null;
  if (!c) throw new Error("No passkey was chosen.");
  return credentialJSON(c);
}

/** The browser's own words for a cancelled or refused prompt, in ours. */
export function passkeyError(err: unknown) {
  if (err instanceof DOMException && (err.name === "NotAllowedError" || err.name === "AbortError")) return new Error("The passkey prompt was closed or timed out.");
  if (err instanceof DOMException && err.name === "InvalidStateError") return new Error("This device already has a passkey for your account.");
  return err;
}
