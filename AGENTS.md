# toxin

Go port of pyconfig (Python) and jetconfig (Node.js): etcd3-backed
configuration addressed by dot-notation keys. Spec-first — the yass spec
set is the behavior contract. Porting references live in the sibling
repos `../pyconfig` and `../jetconfig`.

## Layout

- `root.yass.yaml` — project spec; read it first. Owns the `EnvNaming`
  design (every `TOXIN_*` environment variable).
- `10-etcd3.yass.yaml` — external etcd v3 behavior: keys and values,
  client, reads, writes, deletes, watches, error shapes.
- `20-api.yass.yaml` — public Go API: `Config`, Get/Set/Dump/Load/List/
  Clear/Watch, reload hooks, default instance, file cache.
- `30-cli.yass.yaml` — the `toxin` binary: Go source-scanner mode and
  etcd management subcommands.
- Root package (`*.go` at the repo root) — the public API of `20-api`:
  `Config`, construction and per-call options, the default instance.
- `cmd/toxin` — the binary's `main`; version is set at link time.
- `internal/etcd` — the etcd v3 client layer behind `10-etcd3`.
- `internal/cli` — the scanner and the six management subcommands behind
  `30-cli`, on venom.
- `internal/log` — the levelled logger behind `TOXIN_LOGLEVEL`.
- `internal/testutil` — the shared testcontainers etcd container the
  tests run against.
- `script/` — scripts-to-rule-them-all harness: thin stubs run drop-ins
  from `script/.<phase>/`; machinery adapted from `textla/cr-golang` and
  its common-repo upstreams, not consumed through common-repo.

Numeric file prefixes are implementation order: etcd3 layer, then API,
then CLI.

## Required

- Specs must pass `yass validate` and `yass lint`; a change that breaks
  either is not done.
- New behavior lands in the spec set with, not after, the code that
  implements it.
- Commits follow Conventional Commits; merging a `feat:` or `fix:` to
  `main` releases via cocogitto (`release.yaml`).

## Commands

```sh
script/test          # yass gates, go test (Docker: testcontainers starts
                     # a real etcd), gofmt and go vet
script/setup         # prek install; checks yass is on PATH
script/server        # local etcd on 127.0.0.1:2379
script/console -- -h # the toxin CLI against local etcd
yass list            # every document in the spec set
yass query root@toxin # one spec, references resolved
yass find PATTERN    # search obligation prose
```

## Maintaining this index

- Update the affected `AGENTS.md` files in the same change when paths,
  responsibilities, commands, dependencies, or conventions change.
- Keep indexes brief: record semantic entry points and non-obvious
  constraints; link to existing documentation instead of duplicating it.
- Add a directory index only when it provides useful navigation beyond its
  parent; omit generated, vendored, and fixture trees.
- Do not add `CLAUDE.md` files; Claude Code reads `AGENTS.md` natively, and a
  `CLAUDE.md` anywhere in the tree stops every `AGENTS.md` from loading.
