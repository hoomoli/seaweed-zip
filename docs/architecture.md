# Seaweed Zip

An independent, single-instance resource packaging gateway. It is not an S3
transparent proxy and does not perform consumer-side deployment.

## Trust boundaries

- Submission and reauthorization prove source GET access, without trusted CIDRs
  or an extra machine API token. Bounded admission/source work and quotas remain.
- Management UI/API require Pocket ID OIDC sessions and exact administrator
  group membership. Consumer capabilities never grant management privileges.
- Consumers use 256-bit random Bearer capabilities. SQLite stores SHA256 only.
  Repeated authorized submission with the same clientRequestId rotates the token.
  Same key with different content returns 409. The key is global to this trusted
  single-tenant service, not a user authentication mechanism.
- Every submission verifies a fresh manifest GET and every unique selected
  object GET (up to 4096 bytes, not Range/HEAD). Reusing a READY pack never bypasses
  this check. No upstream response bodies, credentials or signed URLs are saved.
- Only configured source host, port, bucket and decoded path prefix are accepted.
  Redirects, userinfo, unsafe paths and unconfigured private connections fail.
  DNS answers are checked and the checked IP is dialed directly. Fixed dial
  routes are administrator authority, not request fields. HTTPS remains HTTPS
  with original hostname certificate validation and SNI; no automatic downgrade.
  To use a plaintext internal endpoint, obtain a valid HTTP presign from the
  caller's supported signer. Actual SeaweedFS compatibility is an external gate.

## Manifest contract

The versioned [release-manifest-v1 contract](contracts/release-manifest-v1.md)
uses the resource administrator's strict schemaVersion=1/gameId/platform/releaseId
manifest. Both raw-byte manifestSha256 and independently reconstructed canonical
manifestHash are checked. Supported immutable manifests must already be canonical;
the hashes therefore agree but their purposes remain separate. Unknown/duplicate
fields, unsafe/colliding paths, wrong immutable locators, inconsistent sizes and
objects outside the full files manifest fail. Only selected missing public blobs
enter the bag; private source packages are excluded.

## Content and state

Sorted unique SHA256/size pairs, sourceId, bucket, game/platform/release, raw
manifest hash and `bagit-store-v1` determine packId. Signed URLs and request
labels do not. Blob hash/size conflicts fail before reading any source.

SQLite WAL + one pooled connection + foreign keys + busy timeout. INSERT ON
CONFLICT creates a single pack; consumer insertion and key conflict checks are
transactional. Worker scans SQLite rather than request callbacks. Serial worker
uses conditional claims, an incrementing generation, one-second cancellation/lease heartbeat and
conditional READY publication. ZIP paths include generation; hash/size-checked
Store entries stream with a 64 KiB buffer and an explicit output bound. Both
authorization probes and blob downloads default to four concurrent requests.
A bounded window downloads size/hash-verified temporary files on the persistent
volume. A single ZIP writer consumes/removes the window before another is admitted;
quota reservation includes ZIP output plus maximum staging-window bytes. Go's
archive/zip automatically uses ZIP64 where required.

The ZIP contains bagit.txt, bag-info.txt, manifest-sha256.txt and one
`data/blobs/<sha256>` entry per selected digest. bag-info binds raw manifest hash
and package format. SHA256 of the transport ZIP is returned independently.

On restart, unfinished packs become WAITING_AUTH and are fenced; READY persists.
Caller submits the original identity and fresh presigns to recover, or uses its
same clientRequestId and fresh authorization to recover a lost capability.
Canceled final consumer fences unfinished work; other consumers are unaffected.

## Retention

READY expires after 24 hours if unacknowledged. Once every independent consumer
ACKs/cancels, cleanup is eligible immediately, but active downloads protect shared
packages. Cleanup claims atomically exclude nonexpired leases. At most 16 concurrent
download leases per pack are allowed; each is time bounded and the overall renewal
window cannot retain a pack forever. ACK/cancel/download exit trigger cleanup;
startup, daily Shanghai 04:10 and minute retries recover missed cleanup.
HTTP Range/HEAD and If-Range use the file's ZIP hash ETag. Sending a response never
acknowledges a consumer. ACK must match current generation and ZIP hash.

Deletion first enters DELETING, then removes only its controlled local artifact,
then commits DELETED. Failure remains traceable for retry. Records/events expire
three calendar months from creation, clamping month-end dates; records with
uncleaned artifacts are retained. No S3 PUT or DELETE implementation exists.

## Current verification boundary

Local simulated sources are not proof of real SeaweedFS SigV4 interoperability.
Verify your own real signed source routes and consumer validation before deployment.
