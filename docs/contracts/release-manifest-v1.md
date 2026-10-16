# Release manifest delivery contract v1

This contract describes the resource administrator / Zip intermediary boundary.
It is not a source-package delivery API. The intermediary keeps ZIPs on its
persistent local volume; it does not upload them to object storage.

## Submission and source authorization

`POST /api/v1/packs`, JSON (no machine API token or trusted CIDR required):

```json
{
  "contractVersion": "release-manifest-v1",
  "sourceId": "resources",
  "bucket": "releases",
  "game": "example",
  "platform": "android",
  "release": "2026-08-23-01",
  "manifestSha256": "<SHA256 of immutable original manifest bytes>",
  "canonicalManifestHash": "<SHA256 of canonical manifest bytes including LF>",
  "manifestUrl": "<short-lived presigned GET>",
  "objects": [{"sha256": "<lowercase SHA256>", "size": 3, "url": "<presigned GET>"}],
  "clientRequestId": "<random unique deployment attempt ID>",
  "node": "<optional safe node label>",
  "deployment": "<optional safe deployment label>"
}
```

Envelope `game/platform/release` identify manifest `gameId/platform/releaseId`.
Manifest schema version is 1, with exactly `schemaVersion`, `gameId`, `platform`,
`releaseId`, `source: {commitSha,branch,projectId}`, `files` and optional
`miniGamePackage` / `androidPackages`. Each file contains exactly
`path,sha256,size,contentType,objectKey`. The immutable manifest key is
`<game>/manifests/<platform>/<release>.json`; each public blob key must be
`<game>/blobs/sha256/<first-two-hex>/<sha256>`.

Canonical JSON uses recursively lexicographically sorted object keys, files
sorted by Unicode code point path, no insignificant whitespace, UTF-8 (no BOM),
Jackson-compatible strings (no HTML/BMP non-ASCII escaping; supplementary Unicode
uses uppercase UTF-16 surrogate escapes), integers in decimal, and one trailing LF.
Optional package arrays retain their order. Duplicate keys, unknown fields,
unsafe/colliding paths and inconsistent sizes are rejected. The current
administrator requires the original immutable bytes to already be canonical.
Thus the two hashes are equal for supported manifests, but are separately
computed and checked: the raw digest authenticates the GET bytes; the canonical
digest authenticates the parsed semantic manifest. Neither is a ZIP digest.
No `manifestHash` field is present in the immutable JSON itself.

The administrator verifies local blobs by length AND SHA256, then submits only
the nonempty unique missing subset. Every selected SHA256 + size must occur in
the complete manifest files; private packages cannot be selected. The
intermediary verifies the manifest and GET authorization for EVERY selected
object on EVERY submission, including READY cache reuse. It binds each URL path
to the expected bucket + immutable object key. Source GET success is the
submission authority; unauthorized/expired sources fail without issuing a token.
Empty selections are handled locally, without submitting a pack.

An allowlisted administrator-configured source binds scheme, signed Host,
bucket/path prefix and optionally one fixed dial target. Request clients cannot
set the dial target. Redirects, arbitrary private dial and metadata targets are
denied. A fixed route preserves URL scheme, Host, raw query and TLS SNI/certificate
verification. Plain internal storage needs presigning against its actual HTTP
endpoint; HTTPS signatures cannot be rewritten to HTTP. Operations may configure
the deployment-specific internal alias as the explicit source Host and dial.
The route is tested with a signed GET before enabling it. See operations docs.

Authorization and object download concurrency are separately bounded (default
4). Admission, object/task/disk limits and timeouts still apply; overload is 429.
URLs/tokens are never stored in durable job records or printed in logs/errors.
Node/deployment labels must not contain URLs or credentials. Labels are opaque
and informational: they are stored and echoed back but never routed, hashed or
used as a path, so they accept printable ASCII (1-256 characters) other than
`/`, `\` and `@` (for example an internal task ID `auto:local:game:android:...`).
`clientRequestId` stays a URL-safe token (`[A-Za-z0-9._-]`).

## Response, polling and recovery

Submission returns 202:

```json
{"packId":"<content ID>","consumerId":"<random ID>","consumerToken":"<random secret>","state":"QUEUED","statusUrl":"/api/v1/consumers/<id>"}
```

Use `Authorization: Bearer <consumerToken>` for all consumer endpoints. Tokens
are bound to a consumer record, stored only as SHA256 digests and never placed
in URLs. The `packId`/ZIP filename is not a credential. Persist the token only in
protected administrator task state. Never forward it to the browser or logs.

Default configured `sourceId` is `resources`; `bucket` is the deployment's actual
S3 bucket and must agree with the configured source rule (not a hardcoded bucket).

`GET statusUrl` returns `{consumer,pack,downloadPath}`. Relevant `pack` fields:
`packId,state,generation,doneBytes,doneObjects,bytes,artifactSha256,error`.
READY is the only downloadable state. Generations can change after retry/restart.
FAILED is terminal for that attempt; retry requires fresh source authorization.
WAITING_AUTH requires renewed URLs, not blind polling. Error codes are sanitized.
Poll with bounded delay/overall deadline, honoring overload instead of busy-looping.

Example READY status (other safe identity/count/timestamp fields may be present):

```json
{
  "consumer": {"consumerId":"<id>","packId":"<id>","generation":1,"ack":false,"cancelled":false,"createdAt":1787489231},
  "pack": {"packId":"<id>","state":"READY","generation":1,"doneBytes":3,"doneObjects":1,"bytes":700,"artifactSha256":"<ZIP SHA256>","createdAt":1787489231,"readyAt":1787489233,"deleteAfter":1787575633},
  "downloadPath":"/api/v1/consumers/<id>/artifact"
}
```

State vocabulary: `QUEUED`, `VALIDATING` (reserved compatibility state), `PACKING`,
`WAITING_AUTH`, `READY`, `FAILED`, `CANCELLED`, `DELETING`, `DELETED`. A cancelled
consumer returns 409 rather than status, even if other consumers keep the pack.
`DELETING/DELETED` are not downloadable; recovery needs fresh source authorization.

`POST /api/v1/consumers/<id>/authorization` with the same content envelope and
fresh URLs requires the existing consumer token and revalidates all sources.
It returns `{consumerToken,state}`; replace the old token atomically because it
is invalidated. The clientRequestId must remain unique to this deployment.
Presigned URL validity is controlled by storage; authorization is not an extension
of it. In-memory build authorizations last at most 15 minutes, never survive
restart, and GETs remain subject to the upstream expiry during build.

Repeated submission with the same clientRequestId and content rotates the token
of the same consumer; a different pack with the same key conflicts (409). Treat
clientRequestId as unpredictable and do not reuse it for independent consumers.
Pack identity binds source/bucket/release/raw manifest hash/selected SHA+sizes and
package format, not URLs/tokens/node/clientRequestId. Shared packs have independent
consumer records. `POST .../cancel` cancels only that consumer. Last-consumer
cancel also stops a queued/running build; use a fresh request ID for a new attempt.
Cancel has no request body and returns HTTP 200 `{"cancelled":true}`; further
calls with that cancelled consumer return 409. Authorization renewal includes the
original `clientRequestId`, identity, hashes and selected objects (never a newly
recomputed missing subset). It returns HTTP 200, not a new consumer ID.

## Missing-object ZIP and consumption

`GET /api/v1/consumers/<id>/artifact` supports Range, If-Range and ETag = ZIP
SHA256. Verify response bounds and resulting complete ZIP bytes/hash against
READY `bytes/artifactSha256`; never concatenate a 200 response to partial data.
Bounded leases protect an active transfer; the download does not acknowledge it.
If generation/hash changes, discard the partial file before resuming.

ZIP entries use stored compression: `bagit.txt`, `bag-info.txt`,
`manifest-sha256.txt`, and exactly `data/blobs/<sha256>` for the requested subset.
The bag is a missing-object package, NOT a complete deployment directory. Do not
extract entries into public paths blindly. Reject unexpected/duplicate entries,
ZIP traversal/links, entry sizes/hashes, and mismatched BagIt payload inventory.
The administrator maps complete-manifest `file.path -> file.sha256`, combines
verified preexisting blobs and verified delivered blobs, stages all files, and
atomically publishes using its existing deployment flow. No private source ZIP,
APK/AAB, manifest credentials or source URLs enter the bag.

Only after complete local verification AND successful deployment publication:

`POST /api/v1/consumers/<id>/ack`, body
`{"generation": <READY generation>, "artifactSha256": "<verified ZIP hash>"}`.
Matching ACK is idempotent; stale generation/hash is 409.
ACK returns HTTP 200 `{"ack":true}`. An already-acknowledged consumer with the
same retained generation/hash also receives 200 in DELETING/DELETED, allowing
recovery if the original success response was lost. A first ACK after expiry
cleanup still returns 409; records eventually expire, and a new generation
invalidates the old ACK. Never redeploy because an ACK retry failed.
Cancellation does not imply successful deployment. Every consumer must independently
ACK/cancel; one consumer cannot delete a shared pack still needed by another.
Once all consumers have ACKed/cancelled and no active download remains, cleanup
is eligible immediately (also retried in the background). Unacknowledged packs
expire after 24 hours, with bounded download leases. File deletion precedes the
DELETED marker; failures retain DELETING and retry. Restart clears dead leases,
requires fresh source authorization for unfinished builds and keeps valid READY
artifacts. Cleanup never deletes another live generation's staging directory.

## Security and compatibility

Pocket ID management authentication and administrator role allowlists remain
unchanged. Source capability access does not grant management privileges.
The legacy unversioned submission is not accepted by this contract. The v1 HTTP
path is retained; `contractVersion` is the explicit schema compatibility gate.
Clients must not send credentials in label fields or expose response secrets.
Actual network/signature routing and deployment acceptance remain environment
verification gates, distinct from local tests and image publication.

All failures use `application/problem+json` with `type,title,status,phase,code,
requestId` only. Representative safe codes: 400 `INVALID_JSON`, `INVALID_PLAN`,
`INVALID_IDEMPOTENCY_KEY`, `UNSUPPORTED_CONTRACT_OR_HASH`; 401
`INVALID_CAPABILITY`; 403 `ADMIN_AUTH_REQUIRED` (management only); 409
`CONTENT_MISMATCH`, `IDEMPOTENCY_OR_STATE_CONFLICT`, `ARTIFACT_MISMATCH`,
`CANCELLED`, `NOT_READY`, `STATE_CONFLICT`; 410 `ARTIFACT_LEASE_WINDOW_EXPIRED`;
422 `SOURCE_AUTHORIZATION_EXPIRED`, `MANIFEST_HASH_MISMATCH`,
`MANIFEST_IDENTITY_MISMATCH`, `SOURCE_AUTHORIZATION_OR_LIMIT_FAILED`, `TASK_LIMIT`,
`CANONICAL_MANIFEST_HASH_MISMATCH`, `MANIFEST_SCHEMA_OR_SELECTION_MISMATCH`,
`MANIFEST_NOT_CANONICAL`, `SOURCE_LOCATOR_MISMATCH`, `SOURCE_SCOPE_DENIED`,
`DISK_QUOTA`; 429 `BUSY` (`Retry-After: 1`); 503 `ARTIFACT_UNAVAILABLE`, `STORAGE_ERROR`.
Clients branch on code/state, not title or arbitrary upstream response text.
