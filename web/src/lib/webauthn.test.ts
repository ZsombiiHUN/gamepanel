import { describe, expect, it } from "vitest";
import { createPasskey } from "./webauthn";

describe("passkey options", () => {
  it("turns base64url fields into bytes and answers back in base64url", async () => {
    let seen: CredentialCreationOptions | undefined;
    const raw = new Uint8Array([1, 2, 255]).buffer;
    Object.defineProperty(globalThis.navigator, "credentials", {
      configurable: true,
      value: {
        create: async (o: CredentialCreationOptions) => {
          seen = o;
          return { id: "AQL_", rawId: raw, type: "public-key", response: { clientDataJSON: raw, attestationObject: raw, getTransports: () => ["internal"] }, getClientExtensionResults: () => ({}) };
        },
      },
    });
    const out = await createPasskey({ publicKey: { challenge: "AQL_", user: { id: "AAE", name: "a", displayName: "a" }, rp: { name: "x" }, pubKeyCredParams: [] } });
    expect(new Uint8Array(seen!.publicKey!.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 255]));
    expect(new Uint8Array(seen!.publicKey!.user.id as ArrayBuffer)).toEqual(new Uint8Array([0, 1]));
    expect(out).toMatchObject({ rawId: "AQL_", response: { attestationObject: "AQL_", transports: ["internal"] } });
  });
});
