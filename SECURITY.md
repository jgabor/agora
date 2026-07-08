# Security

## Reporting a vulnerability

If you believe you have found a security issue in Agora, please report it responsibly:

1. **Do not** open a public GitHub issue for exploitable vulnerabilities.
2. Contact the maintainer through a private channel (GitHub Security Advisories on [jgabor/agora](https://github.com/jgabor/agora), or the contact listed on [jgabor.se](https://jgabor.se)).
3. Include steps to reproduce, affected versions, and impact.

We will acknowledge receipt and work on a fix before public disclosure when appropriate.

## Scope notes

Agora orchestrates LLM agents through OpenCode and may read local files you pass with `--context`. Treat the following as sensitive:

- **API keys and credentials** — configure models through OpenCode and your environment; do not commit `.env`, private keys, or secrets into the repository.
- **`config.yaml`** — managed by `agora config`; may contain model identifiers. `agora prime` and `agora config get --all` redact secret-like values, but the on-disk file is still user-owned data.
- **Transcripts** — JSONL deliberation logs under the configured transcript store can contain full agent responses and topic text. Back them up and share them only when appropriate.
- **Local context paths** — `--context` reads bounded text from files and directories you specify. Agora skips common secret-looking filenames and binary files, but you remain responsible for what paths you include.

## Safe defaults

- Agora does not enable web research unless you opt in (`--research` or config `research: true`).
- Model subprocesses receive a read-only filesystem guard and avoid OpenCode dangerous auto-approval flags.
- Resume reuses prior evidence and rejects evidence-changing flags to avoid accidental re-fetch.

## Out of scope

Issues in OpenCode, upstream model providers, or misconfiguration of API keys in the user's environment are outside this repository's direct control. Report those to the respective projects when applicable.
