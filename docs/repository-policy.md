# Repository and publication policy

The project uses MIT, matching the owner's open-source licensing convention.
The license does not relicense dependencies, public-domain material or CA roots.
Third-party notices are generated from the actual Go runtime graph and production
npm dependencies. Unknown license text fails generation. Review dependency/source
changes for copyright ownership; automated tools cannot prove provenance or
replace legal advice. No trademark rights are granted.

## Public tree

English code/docs; Chinese product strings are limited to `web/src/locales/zh-CN.ts`.
ASCII paths only. Reserved examples, not internal/customer configuration.
Do not publish agent rules/notes, environment files, credentials, local databases,
archives, screenshots, reports, node_modules or frontend build output. Git ignore
is convenience, not a security boundary: the policy also rejects forbidden tracked
files, and secret scanning checks the candidate snapshot and reachable Git history.
Findings report rule/path only, never matched secret contents. History cleanup is
an explicit operation, never an automatic rewrite.

## CI and registry

Actions and Docker bases are pinned. Pull requests have read-only permissions;
there is no `pull_request_target` execution. Validation includes policy, secret
scanning, license notices, dependency vulnerability audits, race tests, frontend
and browser tests. Native architecture jobs build images without registry write
credentials, scan them and upload only image tar/checksum artifacts for one day.
Separate publication jobs load and verify those exact archives before pushing.
Only main pushes with image inputs changed publish. Docs/tests-only changes still
validate but do not build/upload images. Layer caching avoids retransmitting
unchanged registry layers; changed inputs may still yield a new image manifest.

Image vulnerability gates reject HIGH/CRITICAL fixable findings; unfixed findings
are also checked separately and recorded in the audit log, not called zero-risk.
Scanners and signatures are evidence, not proof that software is vulnerability-free.
The final multiarch digest has a GitHub build attestation. Consumers should verify
that attestation and pin the digest. `latest` promotion rechecks the main tip;
production should not depend on a mutable tag.

First publication needs repository Actions enabled and package permissions.
A new GHCR package can default private: its owner must verify package visibility
and link/access settings after first publication. Repository visibility and the
presence of this policy do not establish registry visibility or production readiness.

The isolated HTTP denial fixture and policy scanner tests contain synthetic private
addresses deliberately; these three exact files are the only address-check exceptions.
The release image uses scratch: no shell, APK tools, BusyBox or Alpine OS binaries
are redistributed. It includes Go timezone data and the builder's Mozilla CA trust
bundle; those notices remain part of the distribution.

License audit caveat: `toggle-selection@1.0.6` declares MIT in its release metadata
but has no standalone license/copyright text in the package or source commit.
The distribution preserves the declared license, standard MIT grant and available
author attribution without inventing an upstream copyright claim. Contributors
must not treat metadata as universal provenance proof. Vite, Rollup and esbuild
notices are also retained for potentially injected runtime helpers.
The Go notices graph is the union of linux/amd64 and linux/arm64 with CGO disabled,
not the developer's host-specific dependency graph. Module logos are not shipped.
