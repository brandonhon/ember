// Browser-side WebAuthn helpers. The Go go-webauthn library exchanges the
// spec's JSON shape (base64url-encoded ArrayBuffers); the navigator.credentials
// API needs real ArrayBuffer / Uint8Array. These two helpers do the round-trip.

function b64urlToBytes(s: string): Uint8Array {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const b64 = (s + pad).replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function bytesToB64url(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// Decode the JSON-shaped options from the server into the binary form the
// platform API wants.
function decodeCreateOptions(o: any): PublicKeyCredentialCreationOptions {
  const r: any = { ...o };
  r.challenge = b64urlToBytes(o.challenge);
  r.user = { ...o.user, id: b64urlToBytes(o.user.id) };
  if (Array.isArray(o.excludeCredentials)) {
    r.excludeCredentials = o.excludeCredentials.map((c: any) => ({
      ...c,
      id: b64urlToBytes(c.id),
    }));
  }
  return r as PublicKeyCredentialCreationOptions;
}

function decodeRequestOptions(o: any): PublicKeyCredentialRequestOptions {
  const r: any = { ...o };
  r.challenge = b64urlToBytes(o.challenge);
  if (Array.isArray(o.allowCredentials)) {
    r.allowCredentials = o.allowCredentials.map((c: any) => ({
      ...c,
      id: b64urlToBytes(c.id),
    }));
  }
  return r as PublicKeyCredentialRequestOptions;
}

// Serialize a PublicKeyCredential (from navigator.credentials.create/get) into
// the JSON shape go-webauthn expects on the server.
function encodeCredential(c: PublicKeyCredential): unknown {
  const resp = c.response as
    | AuthenticatorAttestationResponse
    | AuthenticatorAssertionResponse;
  const out: any = {
    id: c.id,
    rawId: bytesToB64url(c.rawId),
    type: c.type,
    clientExtensionResults: c.getClientExtensionResults
      ? c.getClientExtensionResults()
      : {},
    response: {
      clientDataJSON: bytesToB64url(resp.clientDataJSON),
    },
  };
  if ("attestationObject" in resp) {
    out.response.attestationObject = bytesToB64url(resp.attestationObject);
    const trs = (resp as any).getTransports?.();
    if (Array.isArray(trs)) out.response.transports = trs;
  } else {
    const a = resp as AuthenticatorAssertionResponse;
    out.response.authenticatorData = bytesToB64url(a.authenticatorData);
    out.response.signature = bytesToB64url(a.signature);
    if (a.userHandle) out.response.userHandle = bytesToB64url(a.userHandle);
  }
  return out;
}

export interface ChallengeEnvelope {
  publicKey: any; // server-sent options JSON (per spec)
}

export async function createPasskey(env: ChallengeEnvelope): Promise<unknown> {
  const opts = decodeCreateOptions(env.publicKey);
  const cred = (await navigator.credentials.create({
    publicKey: opts,
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error("registration cancelled");
  return encodeCredential(cred);
}

export async function getPasskey(env: ChallengeEnvelope): Promise<unknown> {
  const opts = decodeRequestOptions(env.publicKey);
  const cred = (await navigator.credentials.get({
    publicKey: opts,
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error("authentication cancelled");
  return encodeCredential(cred);
}

export function passkeySupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.PublicKeyCredential !== "undefined"
  );
}
