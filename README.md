# toxin

etcd3 based cross-platform configuration

Go port of pyconfig (Python) and jetconfig (Node.js): settings live in
etcd v3 under a namespace prefix and are addressed as dot-notation keys —
the key `app.setting` under the prefix `config/` names the etcd key
`/config/app.setting`. A `Config` caches reads, can inherit from a parent
namespace, watches etcd for changes, and mirrors itself to a file cache.
The `toxin` binary manages namespaces and doubles as a scanner over Go
source.

## Library usage

Construct a `Config` with options; unset options take defaults (prefix
`config/`, members `127.0.0.1:2379`, caching on, case sensitivity off,
inheritance on with inherit key `config.inherit` and depth two, watching
off, clearing off, log level critical).

```go
config, err := toxin.New(
	toxin.WithPrefix("myapp/"),                              // default "config/"
	toxin.WithHosts("etcd1.example.com:2379", "etcd2:2379"), // default 127.0.0.1:2379
	toxin.WithWatch(true),                                   // start the watcher now
)
if err != nil {
	log.Fatal(err)
}
defer config.Close() // also stops the watcher
```

More options: `WithCache`, `WithCaseSensitive`, `WithInherit`,
`WithInheritPrefix`, `WithInheritKey`, `WithInheritDepth`,
`WithAllowClear`, `WithLogLevel`, `WithFileCache`.

Reads fall back to a default; the strict variant turns an unset key into
the error `No setting "<key>"`:

```go
env, err := config.Get("app.env", "production") // default when unset
env, err = config.GetStrict("app.env")          // error when unset
err = config.Set("http.port", 9090)             // writes etcd, then the cache
```

Values are JSON-typed: a string crosses verbatim, anything else as its
JSON encoding; a read decodes JSON, so the number above comes back as a
`json.Number` and a plain non-JSON string as itself.

One process-wide `Config` is shared by every caller, constructed on first
use, with package-level shortcuts that operate on it:

```go
config, err := toxin.Default()
err = toxin.Set("app.env", "production")
env, err = toxin.Get("app.env", "development")
```

`toxin.GetStrict`, `toxin.Clear`, `toxin.Reload` and `toxin.ReloadHook`
operate on the same instance.

With watching enabled — `toxin.WithWatch(true)` at construction, or
`config.Watch()` later — the watcher keeps the cache current: set events
decode into the cache, deleted keys drop out, and `config.Close()` stops
it. Watching needs caching; a watch on a cacheless config is skipped with
a warning.

## CLI usage

Build with `go build ./cmd/toxin`.

```sh
# etcd management subcommands — prefix first, then the key
toxin set config/ http.port 9090   # write a value
toxin get config/ http.port        # print it as JSON; exit 1 when unset
toxin dump                         # the namespace as JSON (default prefix config/)
toxin list config/                 # immediate children of a prefix (default /)
toxin clear config/                # clear the namespace; a second operand is
                                   # rejected with "You forgot a slash didn't you?"
toxin load config/ settings.json   # load a JSON file into the namespace
```

Global options in command mode: `-n/--no-inherit` disables inheritance,
`-v/--verbose` (repeatable) steps the log level from critical toward
silly, `--version` prints the version.

With no subcommand, `toxin` is the pyconfig-style source scanner: it
parses Go files for calls to `toxin.Get`, `toxin.Set` and
`toxin.GetStrict` and prints one `key = default` line per call, values in
Go literal syntax.

```sh
toxin -f ./internal/cli              # scan a file, or a directory recursively
toxin -m github.com/shakefu/toxin    # scan a package or module
toxin -m github.com/shakefu/toxin -l # also print each key's current value
toxin -f config.go -v                # show the calls as written
```

Scanner display flags: `-v/--view-call`, `-l/--load-configs`,
`-a/--all`, `-k/--only-keys`, `-n/--natural-sort`, `-s/--source`,
`-c/--color`. Mind the mode: `-v` and `-n` are display flags here, not
the command-mode `--verbose` and `--no-inherit`.

## Configuration

toxin's own runtime configuration comes from `TOXIN_*` environment
variables.

| Variable | Meaning | Default |
|---|---|---|
| `TOXIN_ETCD` | Comma-separated etcd member list; each entry `host[:port]`, optionally prefixed `scheme://` and `user:password@`. Overrides programmatic hosts. | `127.0.0.1:2379` |
| `TOXIN_PREFIX` | Namespace prefix for keys. | `config/` |
| `TOXIN_SSL_CA` | CA certificate file path; the file may hold concatenated PEM. | |
| `TOXIN_SSL_CERT` | Client certificate file path. | |
| `TOXIN_SSL_KEY` | Client key file path. | |
| `TOXIN_AUTH` | Basic auth credentials `username:password`. | |
| `TOXIN_LOGLEVEL` | One of silly, debug, info, warn, critical. | critical |
| `TOXIN_CACHE` | Directory for the file cache. | |
| `TOXIN_WATCH` | Any non-empty value enables watching. | |
| `TOXIN_INHERIT_DEPTH` | Maximum inheritance depth. | `2` |

A variable set to the empty string reads as unset — except `TOXIN_WATCH`,
where any non-empty value is truthy. `TOXIN_LOGLEVEL` and `TOXIN_CACHE`
override the corresponding options; `TOXIN_PREFIX`, `TOXIN_WATCH` and
`TOXIN_INHERIT_DEPTH` are consulted only when the corresponding option is
not supplied.

Per-setting overrides are not runtime configuration and carry no prefix:
`toxin.Env` checks the environment variable named by the key, then the
setting named by its lowercased dotted form, so `MY_DATABASE_HOST`
shadows `my.database.host`; `toxin.EnvKey` maps a setting to its override
name (`my.database.host` → `MY_DATABASE_HOST`).

## Development

```sh
script/setup          # prek install; checks yass is on PATH
script/test           # yass gates + go test (needs Docker) + gofmt/vet
script/server         # local etcd on 127.0.0.1:2379
script/console -- -h  # the toxin CLI against local etcd
```

`script/` follows the
[scripts-to-rule-them-all](https://github.com/github/scripts-to-rule-them-all)
pattern: thin stubs run drop-ins from `script/.<phase>/`. The machinery is
adapted from [textla/cr-golang](https://github.com/textla/cr-golang) and
its common-repo upstreams (scripts-to-rule-them-all, semantic-release),
carried in this repo directly rather than consumed through common-repo.

Tests never run against a mock: testcontainers-go launches a real etcd v3
container, one per test binary, and the first run pulls the image. The
yass spec set (`*.yass.yaml`) is the behavior contract; a change that
breaks `yass validate` or `yass lint` is not done.

[prek](https://github.com/j178/prek) runs the pre-commit hooks: go
tidy/fmt/vulncheck, markdownlint, shellcheck, shfmt, actionlint, zizmor,
and conventional-commit enforcement. Commits follow [Conventional
Commits](https://www.conventionalcommits.org/); merging a `feat:` or
`fix:` to `main` cuts a release automatically (cocogitto, see
`.github/workflows/release.yaml`). The repo has no release tag yet — cut
the first one by dispatching that workflow with an explicit version.
