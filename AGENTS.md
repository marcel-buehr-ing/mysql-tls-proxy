# Project instructions

Read [README.md](README.md) for behavior, supported scope, test evidence and
limitations. Use [docs/RELEASING.md](docs/RELEASING.md) for publication and
[OPERATIONS.md](OPERATIONS.md) for the short project maintenance record.

- Keep this a small Go standard-library proxy. Discuss changes to protocol,
  authentication, TLS policy or deployment scope before implementing them.
- Require TLS on both connections and verify backend CA and hostname. Preserve
  one backend connection per client, opaque authentication forwarding, bounded
  handshakes and the absence of password storage, query replay or pooling.
- Keep all public text in English. Use neutral examples and never include
  organization names, internal infrastructure, customer data or credentials.
- Preserve the MIT license and prominent AS IS, no-warranty and liability
  notice. Test results are evidence for the stated setup, not general guarantees.
- Credit the project team as documented in the README, including AgentCLI.
  Record actual participants for specific maintenance actions.
- Do not publish, push, tag a release or access live databases without explicit
  authorization. Preserve unrelated changes and keep temporary test data out
  of the repository.
- For Go changes, run formatting checks, `go vet ./...` and
  `go test -race ./...`. For workflow changes, validate with `actionlint` when
  available; for image changes, build and check the container when an engine
  is available. Always check whitespace. Report checks that could not run.
- Keep docs concise, with one authoritative location per topic. Update the
  validation record when observed behavior or release readiness changes.
