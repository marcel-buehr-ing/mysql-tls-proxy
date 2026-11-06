# Project maintenance record

Keep entries newest first, factual and free of private operational details.
Behavior and limitations belong in the README; publishing steps belong in
`docs/RELEASING.md`.

## 2026-11-06 - Add certificate reload and bounded connection draining

- Participants: Marcel Buehring approved automatic frontend certificate reload
  and a 60-second drain; Codex implemented and tested both with standard-library
  Go. Authentication forwarding and the default connection limit are unchanged.
- Behavior: validated certificate/key replacements affect new handshakes;
  invalid replacements retain the previous valid certificate. Shutdown stops
  acceptance and waits for sessions to close until the configured deadline.
  Runtime configuration and limits are documented in the README.
- Evidence: Go 1.27.3 race tests and vet passed. An isolated MariaDB 11.4.13
  repeated the existing SQL/dump/restore/password tests, preserved one live
  transaction across certificate rotation and committed it while draining.
  A second transaction rolled back after forced deadline closure. No shared
  database or deployment was used; all local test processes were stopped.
- Packaging: CGO-disabled tests and static Linux/amd64 and Linux/arm64 builds
  passed; Docker build inputs include all new source/test files. No local
  container engine is available; native image execution remains a CI check.
- Release gate: proposed next version `0.2.0`; hosted native image checks and
  image publication remain pending. No release tag or publication performed.

## 2026-11-06 - Verify public multi-platform version 0.1.1

- Participants: Marcel Buehring pushed `main` and `v0.1.1`; Codex followed the
  release and verified public registry metadata without account credentials.
- Evidence: [run 42190419001](https://github.com/marcel-buehr-ing/mysql-tls-proxy/actions/runs/42190419001)
  passed native checks and publication for amd64/arm64, then created their
  shared index; completed at 08:51:39 UTC.
- Artifact: `ghcr.io/marcel-buehr-ing/mysql-tls-proxy:0.1.1`, index digest
  `sha256:59fa4f45789b9e17838fce70a68b30ac3d4eca50f63e5d70fd065c335fb82893`.
  Both child manifests/configurations match their declared platform, revision
  `0317c31`, version `0.1.1`, user `65532:65532` and exposed port `3306/tcp`.
  This is publication acceptance; no database deployment was performed.

## 2026-11-06 - Validate native arm64 and prepare version 0.1.1

- Participants: Marcel Buehring pushed the multi-platform workflow; Codex
  verified both native jobs and prepared the local release tag `v0.1.1`.
- Evidence: [run 42189986779](https://github.com/marcel-buehr-ing/mysql-tls-proxy/actions/runs/42189986779)
  at `a2660fd` passed formatting, vet, race tests, image build and container
  startup on amd64 and arm64; completed at 08:47:06 UTC.
- Release gate: branch publication jobs were correctly skipped. The tagged
  workflow must still publish both architecture images and their shared index.
  Only the validation documentation changed after the successful native runs.

## 2026-11-06 - Add native arm64 builds and multi-platform publication

- Participants: Marcel Buehring requested arm64 support; Codex prepared native
  amd64/arm64 test and image-build matrices plus a gated image-index job.
- Release design: both platforms must pass before publishing the shared version
  tag. The existing builder digest supports both; proxy code and Dockerfile are
  unchanged. The next release is intended as `0.1.1`, preserving `0.1.0`.
- Boundary: native arm64 execution and image-index publication await the first
  hosted run. The existing real-database integration evidence is amd64-only.
- Local checks: workflow lint/shell syntax, platform/tag selection and index
  completeness checks passed. Go 1.27.3 cross-compiled a static Linux/AArch64
  executable successfully; no ARM execution is claimed from this x86_64 host.

## 2026-11-06 - Verify version 0.1.0 publication

- Participants: Marcel Buehring pushed the tag and made the package public;
  Codex checked the public workflow and anonymous registry access.
- Evidence: [release run 42188679820](https://github.com/marcel-buehr-ing/mysql-tls-proxy/actions/runs/42188679820)
  succeeded. The public `0.1.0` manifest resolves to Linux/amd64, user
  `65532:65532`, digest
  `sha256:014e0cda18ddec098486f1e5f107d869f66ba1d12cea3a71fa05e048bb691bf2`.

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
