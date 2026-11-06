# Project maintenance record

Keep entries newest first, factual and free of private operational details.
Behavior and limitations belong in the README; publishing steps belong in
`docs/RELEASING.md`.

## 2026-11-06 - Prepare version 0.1.0

- Participants: Marcel Buehring approved image publication; Codex prepared
  the validation documentation and local release tag `v0.1.0`.
- Scope: documentation only; proxy code, Dockerfile and workflow match the
  successful initial build. No deployment is part of this release.
- Publication: the maintainer will push `main` and the tag. After the release
  workflow succeeds, the new container package must be made public in GitHub.

## 2026-11-06 - Verify the first public workflow run

- Participants: Marcel Buehring published the source; Codex checked the public
  repository and GitHub Actions results.
- Evidence: public `main` matches `4bb6aa2`.
  [Run 42185650421](https://github.com/marcel-buehr-ing/mysql-tls-proxy/actions/runs/42185650421)
  passed formatting, vet, race tests, the scratch image build and container
  startup checks, including non-root/read-only execution and missing-config
  rejection. Completed at 08:10:35 UTC.
- Release state: image publication was correctly skipped for the branch push.
  Registry publication still awaits a version tag and its successful workflow.

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
