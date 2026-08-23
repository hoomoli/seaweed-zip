# Open-source audit

Audit snapshot: 2026-10-14. This records technical checks, not a legal opinion or
a promise of future vulnerability-free behavior. Recheck dependencies and images
at each release.

## Licensing and provenance

- Project: MIT, copyright 2026 hoomoli. The owner must hold the necessary rights
  to the original code and documentation; automation cannot establish ownership.
- Distributed Go graph: eight external modules, derived from the union of
  linux/amd64 and linux/arm64, with CGO disabled. Original module license files,
  modernc libc's nested third-party notices, and SQLite's public-domain
  declaration are preserved in `THIRD_PARTY_NOTICES.txt`.
- Frontend: 70 locked production npm dependencies declare MIT. Installed license
  text and author attribution are retained. Vite, Rollup and esbuild license
  notices also cover potentially injected runtime helpers.
- `@ant-design/icons-svg@4.6.0` omits its license file from the package. Its MIT
  text is retained from upstream commit
  `7f2516ac91226d2b41f93b35cb5197c8d94f7189`.
- `toggle-selection@1.0.6` has a metadata-only MIT declaration, not a standalone
  upstream copyright/license file. This limitation is explicitly documented in
  `docs/licenses/toggle-selection.txt`; no copyright claim is invented.
- The scratch image includes Go/IANA timezone data and an unmodified Mozilla CA
  trust bundle. MPL-2.0 text and the exact corresponding Alpine source archive
  link are included in the notices. No Alpine shell, package manager or other
  OS executable is included. Dependency logos are not distributed.

## Public-tree and security checks

Repository policy tests and a real Gitleaks scan of the publication candidate
snapshot pass. Internal planning material stays ignored and local. Runtime
databases, credentials, agent rules, generated frontend assets, browser reports
and screenshots are not publication candidates; Docker uses a context allowlist.
At this snapshot the checkout has no commit history, so the local reachable
history scan has no history to exercise. CI scans all reachable history after
checkout with full fetch depth.

The official npm audit reports zero known vulnerabilities. Go govulncheck reports
no vulnerabilities. The final locally built linux/amd64 and linux/arm64 images pass Trivy
vulnerability and secret scanning, including unfixed findings: zero detected.
This does not cover flaws absent from the scanners' databases, browser
dependencies embedded inside minified JavaScript, or actual operator deployment.
The npm dependency audit covers the locked JavaScript dependency tree separately.
Dockerfile configuration scanning also reports no failed checks.

Race tests, vet/build, frontend unit/build tests, browser tests and actionlint
pass. Image smoke checks verify UID 10001, a read-only root filesystem with
dedicated writable storage, readiness, healthcheck and management default deny.
Both architectures are built and smoke-tested locally; amd64 execution uses local
emulation on an arm64 host, not a hosted native runner.

## Release boundaries

GitHub repository visibility is public. Private vulnerability reporting,
Dependabot alerts and automated security fixes are enabled and read back.
Secret scanning and push protection were already enabled.

No commit, push, remote Actions run or GHCR publication is part of this snapshot.
The workflow is configured, not yet proven on hosted runners. Package public
visibility, final multiarch manifest and registry attestation require verification
after the first successful release. No production source, real signed request,
consumer deployment or restore drill is accepted by these local checks.

Publication blocks on secrets, HIGH/CRITICAL fixable vulnerabilities and test or
policy failures. Unfixed vulnerabilities are counted explicitly, not hidden
behind a zero-findings claim. A license declaration is evidence to review, not a
substitute for provenance assurance.
