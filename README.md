# Seaweed Zip

A self-hosted resource packaging gateway. Submit a manifest and short-lived
presigned GET URLs; stream verified blobs into a BagIt / Store ZIP and download
with a consumer capability. No S3 secret keys, source writes or source deletes.

- Go HTTP server, SQLite WAL, transactional idempotency and generation fencing.
- Per-object SHA256/length verification, bounded streaming and ZIP64.
- Range / If-Range / HEAD, explicit consumer acknowledgment and authorization recovery.
- Minimum 24-hour artifact retention, daily cleanup at 04:10 Asia/Shanghai,
  and three-calendar-month history retention.
- Compact desktop/mobile management UI; English and Simplified Chinese.
- Creation and management denied by default; configure trusted TCP peer networks.

## Run locally

```sh
npm ci --prefix web
npm run build --prefix web
go test -race ./...
go run ./cmd/seaweed-zip
# Or build the container with its embedded UI:
docker compose up --build
```

Requires Go 1.27.2 and Node 22.20.0+. Generated frontend assets are **not** committed:
build the frontend before running Go commands. Health endpoints are public;
management and creation remain denied until explicitly configured.

Read [operations](docs/operations.md), [architecture](docs/architecture.md),
and the [OpenAPI contract](api/openapi.yaml) before configuring a real source.
One process owns one dedicated volume. This is not a transparent S3 proxy.
Real SeaweedFS interoperability, restore drills and production capacity must be
verified in your environment; simulated local tests are not production acceptance.

## Images and CI

GitHub Actions validates pull requests, main pushes and manual runs. Automatic
publication to `ghcr.io/hoomoli/seaweed-zip` requires a push to main changing
production/build inputs. Documentation and tests-only pushes do not publish images.
To publish without an input change, open Actions, select **Validate and publish**,
click **Run workflow**, and select **main**. Manual main runs execute the full
validation, image build, scan and publication pipeline regardless of input changes;
manual runs on other branches cannot publish. Images are scanned
before push. Tags use `sha-<commit>`; rerunning publication for the same commit
can replace those tags. `latest` is promoted only while the commit is still the
main tip. Prefer a verified digest in deployment.

The workflow supports linux/amd64 and linux/arm64. It reuses build layers and
never uploads runtime data, local configuration, source credentials or build
contexts as artifacts. See [repository policy](docs/repository-policy.md).
Registry images are available only after the configured workflow successfully runs.

Validation caches Go modules and compilation outputs, npm downloads, and Playwright
browser files. Browser caches are isolated by runner OS, architecture and frontend
lockfile; Go cache keys also include the workflow to track audit-tool version changes.
Dependency installation, audits, tests and browser system-dependency installation
still run on cache hits. No credentials or application runtime data are cached.
Go tests disable test-result reuse while retaining the compilation cache.

## Open source

[MIT](LICENSE), copyright 2026 hoomoli. Dependencies retain their own licenses;
see [third-party notices](THIRD_PARTY_NOTICES.txt). Contributions follow
[CONTRIBUTING](CONTRIBUTING.md). Report vulnerabilities privately as described
in [SECURITY](SECURITY.md), never through a public issue containing secrets.
The [audit snapshot](docs/compliance.md) records evidence and remaining limitations.
