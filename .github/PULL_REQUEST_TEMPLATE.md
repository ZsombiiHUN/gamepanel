## What and why

## Checklist
- [ ] I have signed the [CLA](../CLA.md) (the bot asks on your first PR)
- [ ] `make check` passes (Go lint, govulncheck, tests with the coverage floor, web lint/format/types/tests)
- [ ] UI changes: `make e2e` passes
- [ ] New `//nolint` or `eslint-disable` comments name the rule and give a reason
- [ ] Security-sensitive change (files, auth, containers)? Explain the threat model
