# Project maintenance record

Keep entries newest first, factual and free of private operational details.
Behavior and limitations belong in the README; publishing steps belong in
`docs/RELEASING.md`.

## 2026-11-06 - Use the standard database port

- Participants: Marcel Buehring and Codex.
- Change: at Marcel's request, the listener default, container EXPOSE and
  examples use port 3306. The local tunnel example uses a separate backend
  port to avoid connecting the proxy to itself. No deployment or publication.

## 2026-11-06 - Prepare public source and container publication

- Participants: Marcel Buehring and Codex, within the project team credited
  in the README.
- Scope: neutral English documentation, MIT licensing and AS IS notice,
  shared agent context, and a GitHub Actions workflow. No proxy behavior change.
- Publication gate: successful checks precede version-tag image publication.
  No source, image or release has been published from this workspace.
- Initial evidence: isolated MariaDB 11.4.13 integration, Go race/vet/fuzz
  checks and a static Go 1.27.3 build passed; see the README for exact coverage.
  Container execution and the GitHub-hosted workflow await their first run.
- Publication checks: Go 1.27.3 race tests and vet passed; actionlint 1.7.12,
  embedded shell syntax, release-tag validation, local documentation links,
  whitespace and a scan for known private references passed. The MIT license
  is included in the image. No credentials or runtime artifacts are build inputs.
