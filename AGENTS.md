# toxin

Go port of pyconfig (Python) and jetconfig (Node.js): etcd3-backed
configuration addressed by dot-notation keys. Spec-first — the yass spec
set is the behavior contract; no Go code exists yet. Porting references
live in the sibling repos `../pyconfig` and `../jetconfig`.

## Layout

- `root.yass.yaml` — project spec; read it first. Owns the `EnvNaming` design (every `TOXIN_*` environment variable).
- `10-etcd3.yass.yaml` — external etcd v3 behavior: key and value forms, client, reads, writes, deletes, watches, error shapes.
- `20-api.yass.yaml` — public Go API: `Config`, Get/Set/Dump/Load/List/Clear/Watch, reload hooks, default instance, file cache.
- `30-cli.yass.yaml` — the `toxin` binary: Go source-scanner mode and etcd management subcommands.

Numeric file prefixes are implementation order: etcd3 layer, then API,
then CLI.

## Required

- Specs must pass `yass validate` and `yass lint`; a change that breaks either is not done.
- New behavior lands in the spec set with, not after, the code that implements it.

## Commands

```sh
yass list                  # every document in the spec set
yass query root@toxin      # one spec, references resolved
yass find PATTERN          # search obligation prose
yass validate && yass lint # the gates
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
