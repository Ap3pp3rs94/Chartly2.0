# Chartly TypeScript SDK  Examples

These examples assume you have built/linked the SDK package locally.

## Environment variables

- `CHARTLY_BASE_URL` (required)  e.g. `http://localhost:8080`
- `CHARTLY_TENANT_ID` (optional)
- `CHARTLY_REQUEST_ID` (optional)

## Run (ts-node)

From `sdk/typescript`:

```bash
# install dev tool (if you use it)
npm i -D ts-node typescript

# run examples
npx ts-node examples/health.ts
npx ts-node examples/request-json.ts
npx ts-node examples/middleware-logging.ts
```

## Run (compiled JS)

```bash
npm run build
node dist/examples/health.js
node dist/examples/request-json.js
node dist/examples/middleware-logging.js
```

## Notes

- These examples only call `/health` and `/ready`.
- No secrets are required.
- Use env vars to point at your local gateway.
