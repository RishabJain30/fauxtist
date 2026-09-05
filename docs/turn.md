# TURN / Coturn setup

Fauxtist's voice chat is peer-to-peer WebRTC. STUN alone (the default,
`stun:stun.l.google.com:19302`) lets most players connect directly, but
fails for players behind symmetric NATs, some corporate networks, or
certain mobile carriers — those players need a TURN relay.

Fauxtist never runs a TURN server itself; it only knows how to hand
clients time-limited credentials for a TURN server you run separately,
using [Coturn](https://github.com/coturn/coturn) (the standard open-source
implementation) and its REST API static-auth-secret scheme.

## Deploying Coturn

Run Coturn on its own host/VM (not inside this process, and not as a
sidecar in the same container). A minimal `turnserver.conf`:

```ini
listening-port=3478
tls-listening-port=5349
fingerprint
lt-cred-mech
use-auth-secret
static-auth-secret=<a long random value — this is FAUXTIST_TURN_SHARED_SECRET>
realm=your-domain.example.com
no-cli
```

Open UDP/TCP 3478 (and TLS 5349 if you terminate TLS at Coturn) on that
host's firewall/security group. See Coturn's own documentation for
production hardening (rate limits, denied IP ranges, systemd unit, etc.)
— that configuration is entirely Coturn's concern, not this app's.

## Connecting Fauxtist to it

Set on the Fauxtist process (not the frontend — the frontend never sees
the shared secret):

```text
FAUXTIST_TURN_URLS=turn:turn.your-domain.example.com:3478
FAUXTIST_TURN_SHARED_SECRET=<the same value as static-auth-secret above>
FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS=3600
```

With both `FAUXTIST_TURN_URLS` and `FAUXTIST_TURN_SHARED_SECRET` set, a
joined client's `ice_config_request` gets back a TURN entry with a
freshly minted username/credential pair, valid for
`FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS`. Leave either unset and only the
STUN entries are returned — voice keeps working, just without a relay
option.

## Why this isn't a plain HTTP endpoint

An earlier design considered a public `GET /ice-config` HTTP endpoint.
Authenticating it would have meant either accepting it unauthenticated
(anyone could mint TURN credentials) or passing a reconnect token as a
query parameter — which risks that token ending up in access logs or a
proxy's logs, exactly what this project's logging rules forbid. Instead,
ICE configuration is requested over the WebSocket session a player
already authenticated on join (`ice_config_request` → `ice_config`),
reusing that exact trust boundary with no new auth scheme and no
credential ever touching a URL.

## Credential mechanics

- Username: the credential's Unix expiry timestamp (`now + TTL`).
- Credential: `base64(HMAC-SHA1(sharedSecret, username))` — Coturn's
  documented REST API convention.
- The shared secret itself is never sent to a client, logged, or
  otherwise exposed; only the derived, time-limited credential is.
