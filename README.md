# gh-merge-doctor

Your agent says the checks passed. GitHub still blocks the pull request. Find the
missing prerequisite. `gh-merge-doctor` explains which required context, commit,
provider, review, or branch policy keeps the PR from being ready, then gives an
agent-readable handoff with evidence.

For example, a PR can show a green matrix while branch protection still requires
`ci`. The useful answer is: “`ci` has no acceptable result for the selected SHA,”
with the SHA and evidence URL attached. The tool does not call that PR ready merely
because visible checks are green.

## Install as a GitHub CLI extension

Install the extension from the repository, then invoke it as `gh merge-doctor`:

```sh
gh extension install amasen02/gh-merge-doctor
gh merge-doctor --demo
```

The demo intentionally exits `1` because its synthetic missing-`ci` case is
blocked. Use `gh merge-doctor --version` to record the installed version.

## Standalone installation from source

Build a standalone executable with Go 1.23 or newer:

```sh
git clone https://github.com/amasen02/gh-merge-doctor.git
cd gh-merge-doctor
go build -o gh-merge-doctor ./cmd/gh-merge-doctor
go test ./...
./gh-merge-doctor --demo
./gh-merge-doctor --version
```

On Windows, build `gh-merge-doctor.exe` and invoke that executable from PowerShell.
The project uses only the Go standard library and has no telemetry or hosted service.

Native v0.1.0 binaries are available on the [v0.1.0 release
page](https://github.com/amasen02/gh-merge-doctor/releases/tag/v0.1.0), with the
matching [SHA256SUMS file](https://github.com/amasen02/gh-merge-doctor/releases/download/v0.1.0/SHA256SUMS).
Download the raw executable for your platform and compare its digest with the
matching manifest line before running it. On Unix, add execute permission if needed:
`chmod +x ./gh-merge-doctor-*`.

## Thirty-second offline demo

The demo is deterministic and does not invoke `gh`, read credentials, or use the
network. It reproduces the missing-required-context case with synthetic evidence:

```sh
./gh-merge-doctor --demo
./gh-merge-doctor --demo --json
```

The human output identifies the observed blocker and next step. The JSON output has
stable top-level fields such as `status`, `summary`, `blockers`, `next_steps`, and
`limitations`. This human excerpt is from the native demo executable; the response
hash is stable for the same captured content, while `observed_at` varies. Run the
command above for the complete output.

```text
merge is blocked by 1 observed prerequisite(s) (blocked)
head: demo-head-sha
- MISSING_REQUIRED_CHECK [blocker]: required check "ci" has no result on demo-head-sha
  next: restore or run the required workflow for this exact commit
receipt response sha256: 1e3cd0de8a2a5c4e3f7155ae35c4cf5ddfe932a7a217a01d2ea8379d7f91aed7
```

The machine-readable output includes the same diagnosis. These stable fields are a
short excerpt; the complete response also includes findings, blockers, evidence,
source URLs, `observed_at`, and `response_sha256`:

```json
{
  "schema_version": 1,
  "status": "blocked",
  "summary": "merge is blocked by 1 observed prerequisite(s)",
  "head_sha": "demo-head-sha",
  "selected_sha": "demo-head-sha",
  "next_steps": [
    "restore or run the required workflow for this exact commit"
  ],
  "limitations": []
}
```

## Read-only diagnosis

Live diagnosis uses the native GitHub CLI for its existing authentication. The tool
passes bounded, explicit read-only requests to `gh`; it does not print or persist the
token, merge branches, rerun workflows, change protection, comment, or publish:

```sh
./gh-merge-doctor --repo owner/repo --pr 21
./gh-merge-doctor --repo owner/repo --pr 21 --json --receipt merge-doctor.json
```

The live mode currently targets `github.com`. Authenticate `gh` using its normal
interactive flow before running it; do not pass a token as a command argument.

Use the checked-in strict local fixture when reproducing a report without network access:

```sh
./gh-merge-doctor --fixture ./fixtures/missing-required-context.json --json
```

This fixture produces the same missing-`ci` diagnosis as the demo and exits with
status `1` (`blocked`).

Fixture mode must remain offline and must not read `gh` credentials. A receipt is
explicit output selected by the caller; the tool does not save raw PR bodies,
comments, patches, tokens, or file contents. Its response hash supports comparison
of the captured content; it is not a signature, attestation, or proof of provenance.

## Interpreting the result

| Exit | Status | Meaning |
| ---: | --- | --- |
| `0` | `ready` | No supported blocker observed and the required evidence is complete for the selected SHA. |
| `1` | `blocked` | A supported blocker was observed, such as a missing, pending, or failed required check. |
| `2` | — | Invalid arguments or configuration. |
| `3` | `unknown` or `terminal` | Evidence is incomplete, permission-limited, stale, or the PR is already merged/closed. |

`unknown` is a useful result. Do not turn missing API data into “unprotected” or
“ready.” A diagnosis is a point-in-time observation for one exact head or test-merge
SHA. It does not prove code quality or guarantee a future merge.

Completed check runs with `success`, `neutral`, or `skipped` conclusions are
acceptable. A skipped workflow that never creates a required context can still leave
that context missing or pending. If GitHub returns both a check run and a commit
status for a required name, both are considered. Provider/app identity is part of
the evidence when branch protection specifies it.

Supported findings include `MISSING_REQUIRED_CHECK`, `REQUIRED_CHECK_FAILED`,
`REQUIRED_CHECK_PENDING`, `CHECK_SOURCE_MISMATCH`, `DRAFT_PR`,
`REVIEW_REQUIRED`, `CHANGES_REQUESTED`, `MERGE_CONFLICT`, and `BRANCH_BEHIND`.
Unsupported merge queues, deployments, custom rules, or unavailable permissions are
reported as limitations or unknown states.

See [docs/agent-handoff.md](docs/agent-handoff.md) for a copyable integration
instruction for shell-capable AI coding agents.

## Development

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
```

Contributions should preserve the read-only boundary and the `unknown` behavior.
See [CONTRIBUTING.md](CONTRIBUTING.md) for focused contribution ideas and
[SECURITY.md](SECURITY.md) for private vulnerability reports.

## License

MIT. See [LICENSE](LICENSE).
