# mysql-tls-proxy

A small Go proxy for terminating frontend TLS in the classic MySQL/MariaDB
protocol and forwarding the connection over verified TLS to one backend.
Authentication remains with the database. The proxy has no user list, password
cache, connection pool, SQL parser or query retries. It uses only the Go
standard library.

We built and tested this tool for our own use and are sharing it as an open-source
project. Feedback, reproducible bug reports and compatibility results are welcome.

**The software is provided AS IS, without warranties of any kind. The authors
and copyright holders disclaim liability as set out in the [MIT License](LICENSE).
You are responsible for evaluating, configuring and validating it for your own
environment. Our test results are not a guarantee of correctness, security,
availability or suitability for your use.**

```text
Client -- TLS / frontend certificate --> Proxy -- verified TLS --> Database
```

The proxy forwards the database greeting, requires a client SSLRequest, and
establishes the two TLS connections. It then relays bytes in both directions,
including authentication exchanges. Each client gets its own backend connection.
TLS 1.2 or newer is required on both sides; backend CA and hostname verification
are mandatory. There is no option to disable either check.

## Build and test

Requires Go 1.27 or newer. No container engine or database is needed for the
included automated tests.

```sh
go test -race ./...
go vet ./...
go build -trimpath -o mysql-tls-proxy .
./mysql-tls-proxy --help
```

The built binary is ignored by Git. Optional bounded handshake fuzzing:

```sh
go test -fuzz=FuzzHandshake -fuzztime=15s -parallel=2
```

## Run manually

Supply a frontend certificate chain/private key and the CA that signed the
backend certificate. Certificates and credentials do not belong in this project.

```sh
./mysql-tls-proxy \
  --listen 127.0.0.1:3306 \
  --backend db.example.org:3306 \
  --cert /path/to/frontend-fullchain.pem \
  --key /path/to/frontend-key.pem \
  --backend-ca /path/to/backend-ca.pem
```

The backend certificate must match `db.example.org`. When connecting through a
local tunnel listening on port 13306, use `--backend 127.0.0.1:13306` together with
`--backend-server-name db.example.org` to retain verification of its real name.

The listener defaults to `127.0.0.1:3306`. For a local test, use a frontend certificate
with `127.0.0.1` in its IP subject alternative names:

```sh
mariadb --host=127.0.0.1 --port=3306 \
  --user=test_user --password \
  --ssl --ssl-ca=/path/to/frontend-ca.pem --ssl-verify-server-cert
```

The username and password are ordinary database credentials. A frontend
certificate issued by a public CA can use the client's system trust store;
the client must still connect using a name covered by that certificate.

The defaults are 100 simultaneous connections and a ten-second deadline for
connection/TLS setup. `--max-connections` and `--handshake-timeout` adjust them.
Packet contents, usernames, passwords and SQL are never logged.

## Minimal container image

The Dockerfile tests and builds a static binary with `CGO_ENABLED=0`, then copies
the binary and license into `FROM scratch`. The builder uses Go 1.27.3 with a pinned
image digest. The runtime has no shell, package manager or system CA store;
the required backend CA is supplied explicitly at startup. Its default user is
the unprivileged numeric UID/GID `65532:65532`.

```sh
docker build -t mysql-tls-proxy:test .
docker run --rm mysql-tls-proxy:test --help
```

Example with read-only certificate mounts and a host-local published port:

```sh
docker run --rm --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --user "$(id -u):$(id -g)" \
  --publish 127.0.0.1:3306:3306 \
  --mount type=bind,src=/absolute/path/to/tls,dst=/tls,readonly \
  mysql-tls-proxy:test \
  --listen 0.0.0.0:3306 \
  --backend db.example.org:3306 \
  --cert /tls/frontend-fullchain.pem \
  --key /tls/frontend-key.pem \
  --backend-ca /tls/backend-ca.pem
```

The local example uses your UID/GID to read your mounted private key without
loosening its permissions. In a deployment, keep the default UID/GID and grant
it read access through the secret volume. Certificates and private keys are
never included in the build context: `.dockerignore` allows only the build
inputs. No writable filesystem or persistent storage is required.

## GitHub Actions and container releases

The [workflow](.github/workflows/ci.yml) runs formatting checks, `go vet`, race
tests, an image build and container startup checks for pull requests and pushes
to `main`. Checks also run when a version tag is pushed. Only a successful
`vMAJOR.MINOR.PATCH` tag run can publish an image to GitHub Container Registry.

Linux/amd64 and Linux/arm64 are tested and built on native runners. A tag such
as `v0.1.1` publishes architecture-specific images and then a shared
`ghcr.io/<owner>/<repository>:0.1.1` image index after both succeed. Clients
select the matching architecture automatically. The owner and repository are
taken from the workflow context. Publication uses GitHub's temporary `GITHUB_TOKEN`.
Pull requests and branch pushes do not publish images or deploy anything.

See [Releasing](docs/RELEASING.md) for repository setup, public package visibility,
release commands and dependency maintenance. No image release is claimed until
the corresponding GitHub Actions run has succeeded.

## Current validation and limits

Validated locally on 2026-11-06 with Go 1.27.1 and an isolated MariaDB 11.4.13
server, using its matching command-line client and dump utility:

- Verified frontend TLS and a backend account with `REQUIRE SSL`.
- DDL, writes, transaction rollback, and a 20 MB value.
- Dump and restore through the proxy, with matching row count, length and SHA-256.
- Password change through the proxy: old password rejected and new password
  accepted immediately on subsequent connections.
- Plaintext clients and clients trusting the wrong frontend CA rejected.

Automated tests additionally cover untrusted, expired and wrong-name backend
certificates; unchanged byte streams larger than 16 MiB; fragmented and malformed
handshake packets; connection timeout and shutdown. Race detection, `go vet` and
a 15-second fuzz run passed. These are prototype checks, not production acceptance.

The real-server test used `mysql_native_password`. Other authentication plugins,
backend accounts requiring client certificates and MariaDB's automatic trust
fallback are outside the tested scope. Use explicit CA/hostname verification.

Certificates and backend trust are read at startup. Restart to reload them.
SIGINT/SIGTERM and either peer disconnecting close both connections; there is
no graceful draining or transparent reconnect. Clients must handle connection
loss and uncertain transaction outcomes. Existing authenticated sessions follow
the database's normal password-change behavior.

The initial local validation did not run a container engine. A separate Go
1.27.3 build with `CGO_ENABLED=0` and the Dockerfile's linker flags passed the
automated tests; ELF inspection confirmed no dynamic loader or shared-library
dependencies. Its Linux/amd64 binary is approximately 6.2 MB and its `--help`
command runs successfully. GitHub Actions verified the Linux/amd64 image build
and container startup for commit `4bb6aa2` on 2026-11-06; see the
[successful run](https://github.com/marcel-buehr-ing/mysql-tls-proxy/actions/runs/42185650421).
Container startup checks do not replace the documented database integration
tests or validation in your own environment.

Version `0.1.0` was published for Linux/amd64 and verified as anonymously
accessible. Native amd64 and arm64 race tests, image builds and container
startup checks passed for commit `a2660fd` on 2026-11-06; see the
[multi-platform test run](https://github.com/marcel-buehr-ing/mysql-tls-proxy/actions/runs/42189986779).
Shared image-index publication awaits the `v0.1.1` release run. The documented
MariaDB integration tests were performed on amd64.

Protocol reference: [MariaDB connection phase](https://mariadb.com/docs/server/reference/clientserver-protocol/1-connecting/connection).

## Team and feedback

Built and tested by our project team: **Marcel Buehring (Platform Engineer),
Codex, Claude and Hermes**, working with **AgentCLI**.

- Website: [marcel.buehr.ing](https://marcel.buehr.ing)
- Email: [marcel@buehr.ing](mailto:marcel@buehr.ing)
- Matrix: `@marcel:buehr.ing`

Please use repository issues for ordinary feedback and reproducible bugs. Include
the proxy, database and client versions and a sanitized reproduction. Report
security-sensitive findings privately by email; do not include credentials,
private keys or customer data. Contributions are welcome under the MIT license.
