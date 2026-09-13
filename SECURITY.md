# Security policy

Please report a suspected vulnerability privately through [GitHub's security
advisory form](https://github.com/amasen02/gh-merge-doctor/security/advisories/new).
Do not disclose an unpatched vulnerability in a public issue or pull request.

Include the affected version or commit, operating system, reproducible command or
fixture, impact, and any relevant logs. Redact access tokens, private repository
names, private URLs, PR bodies, comments, patches, and file contents before sending
material. There is no response-time guarantee; reports are handled as maintainers
become available.

## Data and permissions

The tool is designed for bounded, read-only GitHub diagnosis. Live mode currently
uses the native `gh` authentication flow and targets `github.com`; it does not
accept tokens as command arguments, merge PRs, rerun workflows, alter protection,
comment, publish, or install hooks. Fixture mode is offline. Receipts are written
only when the caller supplies an output path and should be treated as potentially
sensitive evidence.

If a report depends on missing permissions or incomplete API data, preserve the
`unknown` result and limitation rather than treating the absence as proof of safety.
