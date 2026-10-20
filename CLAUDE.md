# gyrus (Nora's fork)

`justnorawr/gyrus`, a fork of `armckinney/gyrus`: a Go context engine (CLI, MCP stdio server, web UI,
Claude/Codex plugin) over Markdown concepts with YAML frontmatter. In the noratics setup it is Part G of
the noratics-ai plan: the prompt-time lookup over the wiki store `N:\UnrealProjects\noratics-wiki`.

The plan is `N:\UnrealProjects\noratics-wiki\plans\noratics-ai\plan.md` (Part G; contracts C2 and C3).
Ask its planner (`planner - noratics-ai`) about the plan; the code is this session's.

## Remotes and what goes where

- `origin` is the fork, `upstream` is armckinney's. The fork's `main` carries everything; upstream gets
  one pull request per fix.
- **`CLAUDE.md` and `noratics-agent.json` never reach upstream.** An upstream PR is a branch cut from
  `upstream/main` with only the fix's commits cherry-picked onto it, opened with
  `gh pr create -R armckinney/gyrus --head justnorawr:<branch>`.
- A fix goes upstream only after it has run here. Whether upstream merges blocks nothing.
- Fork PRs: `gh pr create -R justnorawr/gyrus` (the default is the parent repo, so always pass `-R`).
  A green PR is a go: squash-merge it and say in it what could not be tested.

## Building and testing

Go is not installed on this machine. Build and test in a container (podman runs in WSL, so host paths are
`/mnt/n/...`); the named volumes keep the module and build caches between runs:

```
C:\bin\podman.cmd run --rm -v /mnt/n/UnrealProjects/gyrus:/src -v gyrus-gomod:/go/pkg/mod `
  -v gyrus-gocache:/root/.cache/go-build -w /src -e GYRUS_PROJECT= -e GYRUS_CONFIG= `
  docker.io/library/golang:1.25 sh -c 'go vet ./... && go test ./...'
```

- Shell quoting does not survive the `podman.cmd` shim: anything longer than one command goes in a script
  file mounted into the container.
- `tests/install` fails on this checkout because `core.autocrlf=true` writes `packaging/install.sh` with
  CRLF (the blob is LF; CI is green). Run that package on a fresh clone inside the container, or check the
  file out with LF endings.
- CI (`wf-ci`) runs `go vet`, a `gofmt` check and `go test ./... -v` on ubuntu. Run `gofmt -l .` before
  pushing.

## Layout that matters for Part G

- `internal/cli/commands/suggest_context.go`: the `suggest-context` command (`--max-tokens` is parsed and
  ignored upstream).
- `internal/domain/lifecycle/engine.go`: `SuggestContextWithFilter` assembles the context layer (project
  concepts, then linked `depends_on` references, then general references).
- `internal/provider/shared/okf/query.go`: the default search: a full scan per query, substring matching
  per term, and the scope boost added as a base score to any match.
- `internal/provider/storage/localfs/store.go`: reads `<root>/concepts/**.md`; one invalid concept fails
  the whole scan.
- `internal/domain/okf/validator.go`: OKF requires `id` (no `/`), `title`, `tenant`, `category`,
  `type`, `version` and `status`, and a canonical collection path.
- `packaging/plugins/gyrus/`: the Claude Code and Codex plugin (manifests, MCP config, skills).

## Part G state

G1: fork `main` equals `upstream/main` (`c409437`), upstream had no open issues or PRs on 2026-10-20,
and the full suite passes here. G2 fixes are tracked on the plan's Progress board, not here.
