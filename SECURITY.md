# Security policy

Security fixes target the current main branch. There is no supported release
series or service-level guarantee yet. Deployments must pin an audited digest.

Please use GitHub's private vulnerability report form:
https://github.com/hoomoli/seaweed-zip/security/advisories/new
If private reporting is unavailable, contact the repository owner through their
GitHub profile to arrange a private channel. Do not disclose exploitation details,
credentials, signed source URLs or customer data in public issues.

Include affected revision, a minimal synthetic reproduction, expected/actual
behavior and impact. Never attach live payloads or capability tokens.

The service trusts configured TCP peer CIDRs, not forwarded headers. An allowed
reverse proxy must authenticate and restrict access before forwarding. The admin
interface is not a public authentication system. Source URLs stay in bounded
memory; consumer tokens are hashed at rest. TLS and volume access remain operator
responsibilities. See the architecture and operations documents for boundaries.
