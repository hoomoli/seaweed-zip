# Seaweed Zip

An independent, single-instance resource packaging gateway. It is not an S3
transparent proxy and does not perform consumer-side deployment.

## Trust boundaries

- Submission and reauthorization require explicit trusted peer CIDRs.
- Management UI/API require independent admin peer CIDRs. Forwarded headers
  are intentionally ignored. Protect any allowed reverse proxy with real
  authentication and access rules; allowing a proxy CIDR trusts all its traffic.
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

The v1 manifest must be JSON with string fields `game`, `platform`, `release`.
Additional manifest fields are allowed. Hash is SHA256 of the original bytes.
The gateway compares all three identity fields exactly. It does not recompute
a caller-specific canonical manifestHash. `canonicalManifestHash` is a separate informational
label, excluded from pack identity. Adapting an existing differently-shaped
manifest needs a source-specific, golden-tested adapter, not JSON reserialization.
Selected blobs are an authorized selection, not a claim that the package contains
all resources in the manifest.

## Content and state

Sorted unique SHA256/size pairs, sourceId, bucket, game/platform/release, raw
manifest hash and `bagit-store-v1` determine packId. Signed URLs and request
labels do not. Blob hash/size conflicts fail before reading any source.

SQLite WAL + one pooled connection + foreign keys + busy timeout. INSERT ON
CONFLICT creates a single pack; consumer insertion and key conflict checks are
transactional. Worker scans SQLite rather than request callbacks. Serial worker
uses conditional claims, an incrementing generation, five-second heartbeat and
conditional READY publication. ZIP paths include generation; hash/size-checked
Store entries stream with a 64 KiB buffer and an explicit output bound. Go's
archive/zip automatically uses ZIP64 where required.

The ZIP contains bagit.txt, bag-info.txt, manifest-sha256.txt and one
`data/blobs/<sha256>` entry per selected digest. bag-info binds raw manifest hash
and package format. SHA256 of the transport ZIP is returned independently.

On restart, unfinished packs become WAITING_AUTH and are fenced; READY persists.
Caller submits the original identity and fresh presigns to recover, or uses its
same clientRequestId and fresh authorization to recover a lost capability.
Canceled final consumer fences unfinished work; other consumers are unaffected.

## Retention

READY gets a minimum 24-hour expiry; cleanup runs daily at Shanghai 04:10 and
on startup. Open downloads register individual bounded leases before opening the
file; cleanup claims atomically exclude nonexpired leases. Lease renewal cannot
retain a pack forever: all download leases are capped at the first scheduled
cleanup after expiry plus the configured maximum lease duration. Deferred/failed
cleanup retries each minute without advancing other packages' daily eligibility. HTTP Range/HEAD and
If-Range use the file's ZIP hash ETag. Sending a response never acknowledges a
consumer. ACK must match current generation and ZIP hash.

Deletion first enters DELETING, then removes only its controlled local artifact,
then commits DELETED. Failure remains traceable for retry. Records/events expire
three calendar months from creation, clamping month-end dates; records with
uncleaned artifacts are retained. No S3 PUT or DELETE implementation exists.

## Current verification boundary

Local simulated sources are not proof of real SeaweedFS SigV4 interoperability.
Verify your own real signed source routes and consumer validation before deployment.
