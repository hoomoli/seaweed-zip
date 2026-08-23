# Open-source audit

Dependency-upgrade candidate snapshot: 2026-10-14. This records technical checks, not a legal opinion or
a promise of future vulnerability-free behavior. Recheck dependencies and images
at each release.

## Licensing and provenance

- Project: MIT, copyright 2026 hoomoli. The owner must hold the necessary rights
  to the original code and documentation; automation cannot establish ownership.
- Distributed Go graph: locked external modules, derived from the union of
  linux/amd64 and linux/arm64, with CGO disabled. Original module license files,
  modernc libc's nested third-party notices, and SQLite's public-domain
  declaration are preserved in `THIRD_PARTY_NOTICES.txt`.
- OIDC: coreos/go-oidc v3.16.0 and go-jose v4.1.3 use Apache-2.0; original
  license texts are preserved and their reviewed hashes/version tuples are pinned
  in the notice generator. OAuth2 v0.36.0 uses BSD-3-Clause. These additions are
  necessary for discovery, JWT/JWKS validation and PKCE code exchange.
- Frontend: 70 locked production npm dependencies declare MIT. Installed license
  text and author attribution are retained. Vite 8 and Rolldown license notices,
  including Rolldown's bundled third-party declarations, also cover potentially
  injected runtime helpers. React and react-dom are both pinned to 19.3.0.
- `@ant-design/icons-svg@4.6.0` omits its license file from the package. Its MIT
  text is retained from upstream commit
  `7f2516ac91226d2b41f93b35cb5197c8d94f7189`.
- `is-mobile@5.0.0` omits a standalone license file but ships the complete MIT
  grant and copyright attribution in README.md. That exact release text is
  retained in `docs/licenses/is-mobile.txt`, with its upstream source commit.
  The generator rejects fallback version drift and missing unreviewed licenses.
- The old toggle-selection fallback is no longer part of the current runtime
  graph; its historical metadata-only limitation remains documented in the
  retained fallback file, rather than being represented as upstream evidence.
- The scratch image includes Go/IANA timezone data and an unmodified Mozilla CA
  trust bundle. MPL-2.0 text and the exact corresponding Alpine source archive
  link are included in the notices. No Alpine shell, package manager or other
  OS executable is included. Dependency logos are not distributed.

## Public-tree and security checks

Repository policy tests and a real Gitleaks scan of the publication candidate
snapshot pass. Internal planning material stays ignored and local. Runtime
databases, credentials, agent rules, generated frontend assets, browser reports
and screenshots are not publication candidates; Docker uses a context allowlist.
The publication snapshot and all locally reachable Git history pass the secret
scan. CI scans all reachable history after checkout with full fetch depth.

The official npm audit reports zero known vulnerabilities. Go govulncheck reports
no vulnerabilities for both Linux distribution architectures.
This does not cover flaws absent from the scanners' databases, browser
dependencies embedded inside minified JavaScript, or actual operator deployment.
The npm dependency audit covers the locked JavaScript dependency tree separately.
The upgraded Action references were checked against their official release tags;
the Node 26.11.1 Alpine index digest was verified against the registry. CI and
the Docker UI build use the same Node version.
The exact Node 26.11.1 Linux/arm64 image also passes a clean frontend install,
unit tests and production build with temporary host-backed dependency storage.
The upgraded Dockerfile also passes the Trivy configuration gate with no findings.

Race tests, vet/build, frontend unit/build tests, browser tests and actionlint
pass. Ant Design 6's Drawer, Alert and Timeline APIs were migrated; browser tests
also reject deprecated Ant Design API warnings.

The initial upgraded Docker build ran out of storage while downloading Go
modules. User-approved cleanup removed unused build cache only; existing images,
operational containers and data volumes were not pruned. After cleanup, both
linux/arm64 and linux/amd64 images build successfully and pass Trivy 0.69.3
vulnerability and secret scanning with no findings, including unfixed findings.
Both images pass smoke checks for UID 10001, a read-only root filesystem with
dedicated writable storage, readiness, healthcheck and management default deny.
The local amd64 smoke check uses emulation on an arm64 host; native hosted
validation of this candidate remains a release gate.

## Release boundaries

The recreated GitHub repository is public; secret scanning and push protection
are enabled. Private vulnerability reporting, Dependabot alerts and automated
security fixes are currently disabled, as confirmed by read-only API checks.
These repository settings are separate from the weekly dependency-update PRs
configured in `.github/dependabot.yml` and require restoration by the owner.

No commit, push, remote Actions run or GHCR publication of these dependency
upgrades is part of this snapshot. The previous baseline commit `985f630` passed
hosted validation, native image builds and publication; its public GHCR image was
anonymously pulled and smoke-tested. That release evidence does not validate the
new dependency tree. No production source, real signed request, consumer
deployment or restore drill is accepted by these local checks.

Publication blocks on secrets, HIGH/CRITICAL fixable vulnerabilities and test or
policy failures. Unfixed vulnerabilities are counted explicitly, not hidden
behind a zero-findings claim. A license declaration is evidence to review, not a
substitute for provenance assurance.
