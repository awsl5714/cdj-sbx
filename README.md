# sbx

A safety-first CLI for managing [sing-box](https://github.com/SagerNet/sing-box) server configs.

`sbx` turns "editing `config.json`" into a **validated, atomic, auditable** operation — usable by a human or driven safely by an AI agent. It manages users across a paired **VLESS-REALITY + Hysteria2** setup and keeps the two inbounds in sync.

## Why

On a single-node, multi-user sing-box proxy the biggest risk is not the protocol — it's **config mutation safety**: a human, a script, or an AI fat-fingering a single-source-of-truth `config.json`. `sbx` formalizes the guardrail:

```
structured command
   -> sing-box check        (schema, upstream is the authority)
   -> invariant check       (semantics: the two inbounds stay in sync)
   -> atomic apply          (same-dir temp + fsync + rename)
   -> git commit            (diff + rollback)
```

It never reimplements sing-box's schema validation, and it never writes a config that failed validation (fail-closed).

## Install

```sh
go install github.com/awsl5714/cdj-sbx/cmd/sbx@latest
```

Or grab a prebuilt binary from [Releases](https://github.com/awsl5714/cdj-sbx/releases).

## Quickstart

```sh
sbx --config /etc/sing-box/config.json init        # adopt an existing config, set up git baseline
sbx user add alice --dry-run --json                # preview the change, write nothing
sbx user add alice                                 # apply + reload + commit
sbx user list
sbx link alice --server 203.0.113.10               # vless:// + hy2:// share links
sbx verify                                         # schema + invariants
sbx user del alice
```

## Invariants

- **I1** — `reality-in` and `hy2-in` describe the same user set (`name ↔ name`, `reality.uuid == hy2.password`).
- **I2** — secrets are unique.
- **I3** — the live config is only ever replaced atomically, after both schema and invariant checks pass.
- **I4** — every applied change is committed to git.
- **I5** — mutations are serialized with a file lock.

## Agent interface

Every command supports `--json` (stable envelope) and mutating commands support `--dry-run`. Stable exit codes let a script or agent branch on the failure class:

| code | meaning |
|---|---|
| 0 | ok |
| 2 | usage |
| 3 | schema invalid |
| 4 | invariant violated |
| 5 | io / apply error |
| 6 | reload failed |
| 7 | lock timeout |

## Continuous integration

CI (`gofmt` + `vet` + `test` + `build`) is staged at `ci/ci.yml`, outside
`.github/workflows/` because the initial push token lacked the `workflow`
scope. To activate GitHub Actions:

```sh
gh auth refresh -s workflow
git mv ci/ci.yml .github/workflows/ci.yml
git commit -m "ci: enable workflow" && git push
```

## Releases

Cross-platform binaries are built with [GoReleaser](https://goreleaser.com)
(`.goreleaser.yaml`):

```sh
git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

## Status

v1. Not yet: init-from-scratch, WARP split routing, MCP server, multi-node. See `docs/specs/`.

## License

MIT
