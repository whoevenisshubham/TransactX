## Summary

<!-- Briefly describe what this PR changes and why. -->

## Type of change

- [ ] Bug fix
- [ ] Feature / UI improvement
- [ ] Refactoring / cleanup
- [ ] Documentation
- [ ] CI / deployment / tooling

## Verification

- [ ] Frontend unit & money-boundary tests (`npm test`, `npm run test:money`)
- [ ] Frontend typecheck & build (`npm run lint`, `npm run build`)
- [ ] Backend tests & vet (`go test ./...`, `go vet ./...`)
- [ ] Browser / Playwright checks (if UI or offline flow changed)

## Checklist

- [ ] No secrets, tokens, or `.env` files committed
- [ ] Financial invariants preserved (integer paise, idempotency, double-entry ledger)
- [ ] Documentation updated where behavior or setup changed
