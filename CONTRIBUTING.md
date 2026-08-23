# Contributing

Open an issue describing the behavior and expected result before substantial work.
Use small pull requests with regression tests. By submitting a contribution you
confirm you have the right to submit it under the project's MIT license.

Public documentation, code, comments and paths use English/ASCII paths.
Product translations belong in `web/src/locales/zh-CN.ts`. Do not include real
signed URLs, tokens, customer resources, private networks or internal deployments.
Use reserved example domains and finite synthetic fixtures.

```sh
npm ci --prefix web
npm run test --prefix web
npm run build --prefix web
go test -race ./...
go vet ./...
python3 -m unittest discover -s scripts/tests
python3 scripts/repository_policy.py
python3 scripts/generate_notices.py --check
```

Run browser tests with `npm run test:e2e --prefix web` after installing Chromium
using `cd web && npx playwright install chromium`. Update dependency notices when
changing dependencies. Keep generated files, credentials and agent notes out of
commits. See `docs/repository-policy.md` for release gates.
