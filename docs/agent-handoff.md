# Handoff for AI coding agents

`gh-merge-doctor` is a read-only diagnostic for the moment when an agent sees
green checks but cannot explain why GitHub still blocks a pull request. It
returns observed evidence, a bounded next step, and an explicit `unknown` state
when the available API data is insufficient.

## Copyable instruction

Give a shell-capable agent this instruction:

```text
When a pull request appears green but its merge state is unclear, run:

  gh-merge-doctor --repo OWNER/REPOSITORY --pr PR_NUMBER --json --receipt merge-doctor.json

Read status, summary, blockers, next_steps, limitations, head_sha, and selected_sha.
Treat exit 3 or status unknown as incomplete evidence. Do not infer that absent API
data means the repository is unprotected or that the PR is ready. Include the exact
SHA and evidence URLs in the human handoff. If status is terminal, report that the
PR is already merged or closed. Present the evidence and next step to a human.
Do not install hooks, edit repository settings, rerun workflows, apply fixes, or merge.
```

This is provider-neutral shell guidance. It is not a tested vendor plugin and does
not install an automatic hook or perform an automatic action.

## Result contract

The JSON handoff keeps these top-level fields stable:

| Field | Agent use |
| --- | --- |
| `status` | `ready`, `blocked`, `unknown`, or `terminal` |
| `summary` | Short explanation of the observed state |
| `head_sha` | PR head observed by the collector |
| `selected_sha` | Commit used for required-check evaluation |
| `blockers` | Findings that prevent readiness |
| `next_steps` | Bounded actions for a human or repository owner |
| `limitations` | Missing permissions, unsupported policies, or incomplete evidence |

Completed check runs with `success`, `neutral`, or `skipped` conclusions can satisfy
a required context. A skipped workflow that does not create that context remains a
possible blocker. When a required check has provider or app identity, preserve that
identity in the evidence; a same-name result from another source is not equivalent.

The receipt is optional and must be requested with `--receipt PATH`. Its canonical
hash excludes volatile capture time, so two otherwise identical observations can be
compared while the observation time remains visible separately. For identical
captured content, the hash is deterministic. The hash is for
content comparison; it is not a signature, attestation, or proof of provenance. Do
not paste tokens, PR bodies, comments, patches, or file contents into an agent prompt.

## Offline handoff

For a local, credential-free demonstration:

```sh
./gh-merge-doctor --demo --json
```

Use `--fixture PATH --json` for a checked-in or locally generated snapshot. Fixture
mode does not call GitHub or read `gh` credentials. Live mode currently supports
`github.com` and uses native `gh` authentication for bounded read-only requests.
Use `--version` when recording the tool version alongside a handoff.
