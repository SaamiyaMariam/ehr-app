# Browser checks

Real-browser checks driven through an installed Google Chrome with
`playwright-core` (no browser download). They create clearly named `E2E ...`
records in the development database; remove them with
`scripts/cleanup_e2e_data.sql` (dry run by default).

Prerequisites

- API on `:8080` (`cd backend && set -a && . ./.env && set +a && go run ./cmd/api`)
- Frontend on `:3000` (`cd frontend && npm run dev`)
- `go` on `PATH` (the checks create the first administrator the way an
  operator would, with `go run ./cmd/bootstrap-admin`)
- Google Chrome (`E2E_BROWSER=msedge` for Edge, `E2E_HEADED=1` to watch)

```
npm install
node module9.mjs     # insurance payments
node module10.mjs    # balance engine
node module11.mjs    # statements
node module12.mjs    # dashboard / reports
node module13.mjs    # secondary insurance
node final.mjs       # the whole workflow (records named "E2E FINAL ...")
```

Screenshots are written to `artifacts/` (git-ignored). Each script exits
non-zero if a step fails or the browser logs a console error.
