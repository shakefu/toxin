# toxin

Cross-platform configuration with optional etcd3 storage

Go port of pyconfig (Python) and jetconfig (Node.js): settings are
addressed as dot-notation keys under a namespace prefix. With etcd
configured, the key `app.setting` under the prefix `config/` names the etcd
key `/config/app.setting`. Without etcd, a `Config` uses in-process values,
supplied maps, its file cache, JSON config files, and call-site defaults
without dialing or logging. A `Config` can inherit from a parent
namespace, watch etcd for changes, and mirror itself to a file cache. The
`toxin` binary manages namespaces and doubles as a scanner over Go source.

## Library usage

Construct a `Config` with options; unset options take defaults (prefix
`config/`, no etcd members, caching on, case sensitivity off, inheritance
on with inherit key `config.inherit` and depth two, watching off, clearing
off, log level critical). Supply `WithHosts` or `TOXIN_ETCD` to enable etcd.

```go
config, err := toxin.New(
	toxin.WithPrefix("myapp/"),                              // default "config/"
	toxin.WithHosts("etcd1.example.com:2379", "etcd2:2379"), // enables etcd
	toxin.WithWatch(true),                                   // start the watcher now
)
if err != nil {
	log.Fatal(err)
}
defer config.Close() // also stops the watcher
```

More options: `WithCache`, `WithCaseSensitive`, `WithInherit`,
`WithInheritPrefix`, `WithInheritKey`, `WithInheritDepth`,
`WithAllowClear`, `WithLogLevel`, `WithFileCache`, `WithConfigFile`,
`WithConfigFiles`, `WithEnv`.

`WithEnv(false)` opts one `Config` out of every `TOXIN_*` runtime
variable, so it is built from its options and defaults alone; inherited
parents follow it. Environment reading is on by default, and
`toxin.Default()` always reads the environment.

### Config files

`WithConfigFile` and `WithConfigFiles` add JSON config files as a source;
`TOXIN_CONFIG` appends more after them. Each entry may be a
comma-separated list. Each file holds one JSON object whose nested maps
flatten into dot-notation keys, as `Load` does with a supplied map, and
JSON numbers stay `json.Number`.

```go
config, err := toxin.New(
	toxin.WithConfigFiles("base.json", "local.json"), // local.json wins per key
)
port, err := config.Get("http.port", 8080) // served at once, no Load needed
```

Precedence, lowest first: the inherited chain, then the etcd namespace or
the file cache standing in for it, then the config files in order, then
explicit writes (`Set`, `Load` with a supplied map) made after the last
load. The caller's default answers only when no source has the key.

Files are read at construction, so a missing or malformed file fails
`New` with `Could not read toxin config: <cause>`. Every `Load` without a
supplied map, and every `Reload`, reads them again. A file that has gone
bad fails the reload with the same error, keeps the live settings, and
runs no reload hooks. Watch events from etcd never displace a
config-file value. `Clear` does: it empties config-file values along with
the rest, and etcd events apply to those keys again until the next
`Load` or `Reload` re-reads the files. toxin never writes a config file,
and writes its values to etcd only on `Load(toxin.CacheOnly(false))`.
Config files are served through the settings cache, so `WithCache(false)`
and `Cached(false)` reads do not see them; with etcd, a `Cached(false)`
read caches the etcd value over the file's until the next load.

### Reads and writes

Reads fall back to a default; the strict variant turns an unset key into
the error `No setting "<key>"`:

```go
env, err := config.Get("app.env", "production") // default when unset
env, err = config.GetStrict("app.env")          // error when unset
err = config.Set("http.port", 9090)             // etcd when configured; cache otherwise
```

With etcd configured, values are JSON-typed: a string crosses verbatim,
anything else as its JSON encoding; a read decodes JSON, so the number above
comes back as a `json.Number` and a plain non-JSON string as itself.
Without etcd, `Set` stores the original typed value in the in-process cache.
Inherited parents load their saved settings once from the same file-cache
directory under the parent's normalized prefix; a missing parent file is
empty.

One process-wide `Config` is shared by every caller, constructed on first
use, with package-level shortcuts that operate on it:

```go
config, err := toxin.Default()
err = toxin.Set("app.env", "production")
env, err = toxin.Get("app.env", "development")
```

`toxin.GetStrict`, `toxin.Clear`, `toxin.Reload` and `toxin.ReloadHook`
operate on the same instance.

With watching enabled and etcd configured — `toxin.WithWatch(true)` at
construction, or `config.Watch()` later — the watcher keeps the cache
current: set events decode into the cache, deleted keys drop out, and
`config.Close()` stops it. Watching needs caching; a watch on a cacheless
config is skipped with a warning. Without etcd, watching is a silent no-op.

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

Management commands and scanner `--load-configs` default to
`127.0.0.1:2379` when `TOXIN_ETCD` is unset or empty, independently of the
library's optional-etcd default. A non-empty `TOXIN_ETCD` overrides that
default; one containing no usable members fails with
`etcd is not configured` rather than running in local-only mode.

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
| `TOXIN_ETCD` | Comma-separated etcd member list; each entry `host[:port]`, optionally prefixed `scheme://` and `user:password@`. Overrides programmatic hosts. | No library client; management commands use `127.0.0.1:2379` |
| `TOXIN_PREFIX` | Namespace prefix for keys. | `config/` |
| `TOXIN_SSL_CA` | CA certificate file path; the file may hold concatenated PEM. | |
| `TOXIN_SSL_CERT` | Client certificate file path. | |
| `TOXIN_SSL_KEY` | Client key file path. | |
| `TOXIN_AUTH` | Basic auth credentials `username:password`; both parts non-empty. | |
| `TOXIN_LOGLEVEL` | One of silly, debug, info, warn, critical; governs every toxin log line, etcd client diagnostics included. | critical |
| `TOXIN_CACHE` | Directory for the file cache. | |
| `TOXIN_CONFIG` | Comma-separated JSON config file paths, read after, and so overriding, the programmatic config files. | |
| `TOXIN_WATCH` | Any non-empty value enables watching. | |
| `TOXIN_INHERIT_DEPTH` | Maximum inheritance depth. | `2` |

A variable set to the empty string reads as unset — except `TOXIN_WATCH`,
where any non-empty value is truthy. `TOXIN_LOGLEVEL` and `TOXIN_CACHE`
override the corresponding options, and `TOXIN_CONFIG` appends to the
programmatic config files; `TOXIN_PREFIX`, `TOXIN_WATCH` and
`TOXIN_INHERIT_DEPTH` are consulted only when the corresponding option is
not supplied. A `Config` built with `WithEnv(false)` reads none of them.

The diagnostics the etcd client logs itself go through toxin's logger at
`TOXIN_LOGLEVEL`. toxin never lets the client build its own stderr logger
or derive one from `ETCD_CLIENT_DEBUG`. grpc's process-wide logger is
grpc's: clientv3 installs a stderr one when `ETCD_CLIENT_DEBUG` is set at
process start, `GRPC_GO_LOG_*` tune it, and toxin leaves it alone.
Warnings about ignored credentials, such as an incomplete `TOXIN_AUTH`,
print at every level and never echo the value.

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
pattern: thin stubs run drop-ins from `script/.<phase>/`. The devops
machinery is consumed from the [common-repo](https://github.com/common-repo)
org upstreams through `.common-repo.yaml` — semantic-release (cocogitto
releases, published as the shakefu bot), renovate (self-hosted; it opens
ref-bump PRs that run `common-repo apply`), and mise (the whole dev
toolchain: go, prek, cocogitto, yass, and the docker trio — colima,
docker-cli, docker-compose; `script/bootstrap` installs everything locked
and starts colima when no docker daemon answers). Go's pin also travels
in `.go-version` for goenv and CI's setup-go. Files those upstreams
manage are upstream-owned: `common-repo diff` must stay clean.

Tests never run against a mock: testcontainers-go launches a real etcd v3
container, one per test binary, and the first run pulls the image. The
yass spec set (`*.yass.yaml`) is the behavior contract; a change that
breaks `yass validate` or `yass lint` is not done.

[prek](https://github.com/j178/prek) runs the pre-commit hooks: go
tidy/fmt/vulncheck, markdownlint, shellcheck, shfmt, actionlint, zizmor,
and conventional-commit enforcement. Commits follow [Conventional
Commits](https://www.conventionalcommits.org/); merging a `feat:` or
`fix:` to `main` cuts a release automatically (cocogitto, see
`.github/workflows/release.yaml`).
