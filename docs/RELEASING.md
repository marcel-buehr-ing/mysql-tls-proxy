# Releasing

The [GitHub Actions workflow](../.github/workflows/ci.yml) tests the source and
builds the scratch image. A pushed `vMAJOR.MINOR.PATCH` tag publishes the image
only after the checks pass. The initial architecture is Linux/amd64.

## One-time setup

1. Create a public GitHub repository with `main` as its default branch and
   upload the reviewed source. Enable GitHub Actions if it is disabled.
2. Confirm the first branch workflow succeeds. It runs formatting checks,
   `go vet`, race tests, an image build and container startup checks.
3. After the first tagged image is published, open the package's settings and
   change its visibility to **Public**. A public repository does not by itself
   make a new container package public.

No custom registry secret is required. The release job uses the temporary
`GITHUB_TOKEN` with `contents: read` and `packages: write`. The test job has
only `contents: read`; pull requests, branch pushes and manual workflow runs
cannot publish. The image is linked to its source repository through its OCI
source label and the workflow token.

Public images can be pulled anonymously. See GitHub's
[Container registry documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
for package visibility and permissions.

## Publish a version

Start from the reviewed commit on `main`, with a clean worktree and successful
checks. Update the validation record when compatibility or behavior changes.
The project maintainer pushes the source and release tag:

```sh
git push origin main
git tag -a v0.1.0 -m 'Release 0.1.0'
git push origin v0.1.0
```

Replace `0.1.0` with the intended version. Use a new version for each change;
do not move or reuse a published tag. Pre-release suffixes are not currently
accepted by the workflow.

The image name follows the repository, in lowercase:

```text
ghcr.io/<owner>/<repository>:0.1.0
```

Wait for both jobs to succeed. Check the published image, then pin its registry
digest in deployments that require an exact artifact. For example, after
substituting the actual owner and repository:

```sh
release_image='ghcr.io/<owner>/<repository>:0.1.0'
docker pull "$release_image"
docker run --rm --read-only --network none \
  "$release_image" --help
```

Only explicit version tags are published; there is no moving `latest` tag.
The workflow does not create a GitHub Release or deploy to a database environment.
Container startup checks verify packaging, not SQL compatibility or availability.
Database integration testing and deployment review remain separate steps.

## Maintenance

GitHub Actions are pinned to reviewed commit SHAs, with their release versions
in comments. Review upstream releases before updating those pins. Keep the Go
version in the workflow and the Dockerfile builder aligned; update the builder
digest when upgrading Go. The module's `go` directive records the minimum
supported Go version. The runtime has no operating-system packages; rebuilding
with a maintained Go toolchain is still necessary for standard-library fixes.

Retain the MIT license in source and container distributions. Keep the README's
AS IS notice and documented test limits; a successful release is not a warranty.
