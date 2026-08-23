# Operations

## Local development

Use Go 1.27.2 and Node 22.20+. Go 1.27.2 was verified as stable against
Go's official release feed and is locked in go.mod and Dockerfile.

```
npm ci --prefix web
npm run build --prefix web
go test -race ./...
go vet ./...
go run ./cmd/seaweed-zip
```

By default, no source is allowed; source authorization rejects creation.
Management routes return 403 without Pocket ID administrator authentication. Health endpoints are
public and contain no task data. Frontend assets in web/dist are generated and ignored; build them before
Go tests or compilation. Docker and CI perform that step automatically.

Development environment (do not copy to a public deployment):

```
export SEAWEED_ZIP_SOURCES='{"resources":{"scheme":"http","host":"storage.example.invalid:8333","bucket":"resources","prefix":"/resources/","dial":"seaweedfs:8333"}}'
go run ./cmd/seaweed-zip
```

Source JSON is administrator-only. The example contains no valid source
credentials and is not installed by default. Keep capability tokens in protected
backend memory/storage, never query strings or browser localStorage. Never log
request bodies, Authorization headers, upstream response bodies or presigned URLs
at reverse proxies. TLS is required on any non-loopback consumer connection.

### Explicit internal storage route

Each source rule requires `scheme`, signed `host`, `bucket` and exact
`prefix: /<bucket>/`; optional `dial` is a single administrator-owned host:port.
For a deployment whose storage service alias is `platform-pre-seaweedfs:8333`
on the shared `dokploy-network`, an explicitly approved internal HTTP rule is:

```json
{"resources":{"scheme":"http","host":"platform-pre-seaweedfs:8333","bucket":"resources","prefix":"/resources/","dial":"platform-pre-seaweedfs:8333"}}
```

The bucket above is illustrative; use the actual shared resource bucket. The
resource administrator must create presigns with that exact HTTP endpoint, using
a separate signer from its public-endpoint manifest reader. The Zip container
must resolve the alias on the shared network. Before enabling the integration,
verify a real presigned manifest AND selected blob GET from the container with
original Host/raw query intact. A publicly signed HTTPS URL instead needs an
HTTPS route with its original hostname/SNI and a valid certificate; pointing its
dial at plaintext port 8333 is not a valid TLS route and fails closed. Never change
scheme/host of an already-signed URL or add an arbitrary request-provided dial.

This repository records the route option but does not modify external Compose,
networks, storage settings or production configuration. Source rules without a
scheme are rejected at startup; migrate old administrator-owned rules explicitly.

## Limits

Defaults: 10,000 unique objects, 2 GiB/object, 10 GiB payload, 11 GiB ZIP,
8 MiB manifest, one active ZIP writer, one synchronous submission at a time (global burst 4, refill 1/second),
four bounded authorization probes and four bounded staged downloads,
128 cached plans / 64 MiB authorization memory (15-minute TTL), 1-hour task and 2-hour maximum download lease; volume quota 50 GiB.
Environment byte overrides: SEAWEED_ZIP_MAX_BYTES, MAX_OBJECT_BYTES, MAX_ZIP_BYTES,
MAX_MANIFEST_BYTES, DISK_QUOTA (each prefix SEAWEED_ZIP_). These are development
starting points, not measured production capacity. Additional overrides:
SEAWEED_ZIP_CONCURRENCY (1..16, default 4), SEAWEED_ZIP_MAX_OBJECTS,
SEAWEED_ZIP_TASK_TIMEOUT_SECONDS,
SEAWEED_ZIP_DOWNLOAD_TIMEOUT_SECONDS. Provision enough physical space
and monitor disk usage. Reserve ZIP output plus up to one concurrency window
of temporary object files (also enforced by admission); no full-package memory buffer. Capacity pressure rejects work, never deletes unexpired
READY packages. Requests are limited to 8 MiB JSON. Untrusted upstream failures
return safe problem details; see events for safe lifecycle codes.

## Container

`docker compose up --build` binds only localhost, mounts its own named volume,
uses UID 10001, read-only root FS, no capabilities, memory/CPU limits and healthcheck.
Do not attach SeaweedFS data volumes. Submission does not require peer CIDRs;
source GET authorization, strict allowlisted routes and admission limits remain.
No production deployment is created by this repository.

Only one server process may own a volume. SQLite is not a multi-instance queue.
Use your approved production deployment and recovery procedures.

## Backup and recovery

Stop the service and snapshot the entire dedicated volume, including state.sqlite,
WAL/SHM and artifacts. Alternatively use SQLite's online backup API from an
operator-controlled tool and coordinate a consistent artifact snapshot; copying
only the database file while writes are active is unsafe. Test restore into a
separate offline instance. Restored unfinished work requires fresh source URLs;
READY packages remain downloadable with the valid consumer capability.

Never restore two live owners onto the same database/volume. Do not discard
DELETING rows after a failed remove; correct storage permissions and retry cleanup.
Lifecycle event histories are bounded at 1000 events per pack. Authorization
cache is bounded at 128 plans / 64 MiB with independent five-second expiry
sweeps, so a long-running worker cannot defer credential eviction.
The first scheduled cleanup plus maximum download duration is a hard upper
bound for renewing download leases (410 afterwards). Delayed/failed eligible
cleanup retries once per minute.
Matching ACK / consumer cancellation / download exit trigger eligible cleanup.
Only all-consumer completion or expiry can remove a READY shared artifact. Changing sources or
limits requires restart and review of WAITING_AUTH tasks.

## Production acceptance (not executed)

1. Confirm volume capacity, allowed source schemes/hosts/routes, quotas, TLS and OIDC authorization.
2. Recheck pinned toolchain/dependency security at rollout; build nonroot image.
3. Verify true SeaweedFS presigned GET over the configured fixed route, preserving
   signed Host, raw path/query and transport semantics. No unsigned fallback.
4. Finite test blobs only: source-side build, consumer Range/If-Range download, independent
   BagIt/object SHA256 verification, restart/authorization recovery and cleanup.
5. Verify measured memory, ZIP64/large file behavior and physical disk exhaustion.
6. Consumer deployment integration is a separate task with its own cache/publish acceptance.

## Management OIDC (Pocket ID)

Management UI and `/api/v1/admin/*` require a server-side OIDC session.
`SEAWEED_ZIP_ADMIN_CIDRS` is a legacy setting and no longer grants management
access, even on localhost. Missing OIDC configuration denies management;
partial/invalid configuration or failed discovery prevents startup. Health,
source GET authorization and consumer capability tokens remain independent.
Do not allow an entire proxy network merely to make management accessible.

Configure a dedicated **Confidential** Web client in Pocket ID with exactly:

- Issuer: `https://pocket.chuandashow.com` (must match discovery issuer).
- Callback: `https://seaweed-zip.chuandashow.com/auth/callback`.
- Scopes: `openid profile email groups`; PKCE S256 is mandatory.
- Restrict client access to the dedicated `seaweed-zip-admins` group. Initially
  assign only the existing approved Pocket ID administrators to that group.
- Include a `groups` array of exact group names in the signed **ID token**.
  A group ID, a string claim, or a userinfo-only claim does not satisfy this contract.
  Verify this in a real authorized login before production acceptance.

Environment contract (all required together, no secrets in Git):

| Variable | Format / value |
| --- | --- |
| `SEAWEED_ZIP_OIDC_ISSUER` | `https://pocket.chuandashow.com` |
| `SEAWEED_ZIP_OIDC_CLIENT_ID` | Dedicated client ID |
| `SEAWEED_ZIP_OIDC_CLIENT_SECRET` | Confidential client secret, injected by deployment |
| `SEAWEED_ZIP_PUBLIC_URL` | `https://seaweed-zip.chuandashow.com`, origin only |
| `SEAWEED_ZIP_OIDC_ADMIN_GROUPS` | `seaweed-zip-admins`; comma-separated exact names, any matching group grants management |
| `SEAWEED_ZIP_SESSION_KEY` | Standard Base64 of exactly 32 cryptographically random bytes |

Generate the session key with `openssl rand -base64 32` in a protected operator
context. Store it independently of the OIDC client secret. Do not paste either
secret into chats or commit it. No credentials are needed at build time.
The key authenticates opaque cookie identifiers; sessions and login transactions
are bounded in-memory stores, never browser-stored access/refresh/ID tokens.
Restart or key rotation logs everyone out. One-hour maximum sessions are capped
by ID-token expiry; group changes become effective on the next login, at expiry,
or immediately by restarting the service to revoke every session. No refresh flow.

HTTPS is required at the public reverse proxy; backend HTTP is supported.
Redirects and CSRF Origin checks use the configured external origin, never
untrusted Host, X-Forwarded-Host or X-Forwarded-Proto headers. Preserve browser
Origin and Cookie headers and route `/auth/*` to the same instance as the UI/API.
Do not cache authenticated pages or endpoints. Do not log callback query strings,
Cookie headers, token responses, authorization headers or presigned URLs.
Discovery/token/JWKS endpoints must use HTTPS with normal certificate validation.

Login binds state and nonce to a signed, Secure/HttpOnly, host-only browser cookie
and a five-minute single-use server transaction; authorization code exchange uses
PKCE S256. JWT signature, issuer, audience, expiry, nonce and exact administrator
group are all checked. Management writes and POST `/auth/logout` require the
exact public Origin and `X-CSRF-Token` from authenticated GET `/auth/session`.
All cookies use the `__Host-` prefix, Path `/` and SameSite=Lax.
Logout revokes the local session and clears its cookie. It does **not** log the
user out of Pocket ID or other applications; signing in may reuse the issuer SSO.
There is no open return URL or guessed issuer logout endpoint.

Deployment acceptance (separate from CI/image publication): verify anonymous
management denial, ordinary-group denial, real administrator login, mutation
CSRF rejection/success, logout revocation, and creation/capability isolation
through the actual HTTPS proxy. Never claim deployed acceptance from mocked tests.
