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

By default, creation and all management routes return 403. Health endpoints are
public and contain no task data. Frontend assets in web/dist are generated and ignored; build them before
Go tests or compilation. Docker and CI perform that step automatically.

Development environment (do not copy to a public deployment):

```
export SEAWEED_ZIP_TRUSTED_CIDRS=127.0.0.0/8,::1/128
export SEAWEED_ZIP_ADMIN_CIDRS=127.0.0.0/8,::1/128
export SEAWEED_ZIP_SOURCES='{"assets":{"host":"storage.example.invalid:8333","bucket":"resources","prefix":"/resources/","dial":"seaweedfs:8333"}}'
go run ./cmd/seaweed-zip
```

Source JSON is administrator-only. The example contains no valid source
credentials and is not installed by default. Keep capability tokens in protected
backend memory/storage, never query strings or browser localStorage. Never log
request bodies, Authorization headers, upstream response bodies or presigned URLs
at reverse proxies. TLS is required on any non-loopback consumer connection.

## Limits

Defaults: 10,000 unique objects, 2 GiB/object, 10 GiB payload, 11 GiB ZIP,
16 MiB manifest, one active worker, one synchronous source authorization request,
128 cached plans / 64 MiB authorization memory (15-minute TTL), 1-hour task and 2-hour maximum download lease; volume quota 50 GiB.
Environment byte overrides: SEAWEED_ZIP_MAX_BYTES, MAX_OBJECT_BYTES, MAX_ZIP_BYTES,
MAX_MANIFEST_BYTES, DISK_QUOTA (each prefix SEAWEED_ZIP_). These are development
starting points, not measured production capacity. Additional overrides:
SEAWEED_ZIP_MAX_OBJECTS, SEAWEED_ZIP_TASK_TIMEOUT_SECONDS,
SEAWEED_ZIP_DOWNLOAD_TIMEOUT_SECONDS. Provision enough physical space
and monitor disk usage. Capacity pressure rejects work, never deletes unexpired
READY packages. Requests are limited to 8 MiB JSON. Untrusted upstream failures
return safe problem details; see events for safe lifecycle codes.

## Container

`docker compose up --build` binds only localhost, mounts its own named volume,
uses UID 10001, read-only root FS, no capabilities, memory/CPU limits and healthcheck.
Do not attach SeaweedFS data volumes. Configure CIDRs for actual TCP peers;
Docker forwarded traffic may not appear as 127.0.0.1. Never allow 0.0.0.0/0.
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
No endpoint performs immediate destructive operator cleanup. Changing sources or
limits requires restart and review of WAITING_AUTH tasks.

## Production acceptance (not executed)

1. Confirm volume capacity, allowed hosts, trusted/admin networks, TLS/auth proxy.
2. Recheck pinned toolchain/dependency security at rollout; build nonroot image.
3. Verify true SeaweedFS presigned GET over the configured fixed route, preserving
   signed Host, raw path/query and transport semantics. No unsigned fallback.
4. Finite test blobs only: source-side build, consumer Range/If-Range download, independent
   BagIt/object SHA256 verification, restart/authorization recovery and cleanup.
5. Verify measured memory, ZIP64/large file behavior and physical disk exhaustion.
6. Consumer deployment integration is a separate task with its own cache/publish acceptance.
