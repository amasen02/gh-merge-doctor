# Contributing

Contributions should help developers and AI coding agents explain a merge blocker
from bounded, point-in-time evidence. Keep the tool read-only: it must not merge a
pull request, rerun a workflow, change branch protection, comment, publish, install
hooks, or send telemetry.

## Development checks

Use Go 1.23 or newer, then run the complete local gate from the repository root:

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
```

The race detector needs a working CGO toolchain on Windows. If that toolchain is
unavailable locally, record the environment limitation and rely on the CI race gate;
do not weaken or skip the repository's required checks.

The offline demo must remain deterministic and must not require `gh`, credentials,
network access, or a hosted service. Live collection should use explicit bounded
`gh` requests and preserve `unknown` when GitHub returns incomplete data. JSON and
human output must not expose tokens, raw PR bodies, comments, patches, or file
contents.

## Focused contribution opportunities

Please keep a proposed change tied to a concrete source gap and add a fixture or
regression test that demonstrates the behavior. Potentially useful future slices
include:

- Add an optional SARIF export. Acceptance: the export maps every current finding to
  a stable rule identifier, preserves `unknown` and limitations, and has a fixture
  test that parses the generated SARIF.
- Publish a JSON Schema for the current report. Acceptance: the schema validates
  every checked-in fixture and demo report, rejects an unknown status or missing
  required field, and documents compatibility changes.
- Explain merge-queue state only when the exact GitHub API response is represented
  by a fixture. Acceptance: the fixture test distinguishes queue evidence from an
  unavailable permission and never turns missing queue data into `ready`.
- Add shell completion only after the CLI's flags are stable. Acceptance: generated
  completion for the supported shell lists the actual flags and has a command-level
  smoke check.

Keep additions focused on user-visible behavior and evidence quality; avoid
popularity or activity claims.

## Pull requests

Describe the user-visible diagnostic behavior, the evidence model, and the command
used to verify it. Keep unrelated formatting out of the change. A pull request
should explain any new GitHub permission, endpoint, fixture field, or output field.
Review [SECURITY.md](SECURITY.md) before reporting a vulnerability.
