// Package toxin is an etcd3-backed configuration library addressed by
// dot-notation keys under a namespace prefix: construct a Config bound to a
// prefix and a cluster with New, then Get, Set, Dump, Load, List, Clear and
// Watch settings through it. Default returns the process-wide instance the
// package-level shortcuts operate on.
package toxin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/shakefu/toxin/internal/etcd"
	"github.com/shakefu/toxin/internal/log"
)

// Option configures a Config at construction. New accepts any subset of the
// options and supplies defaults for the rest.
type Option func(*options)

// options carries the construction options together with the record of
// which options were supplied, so the TOXIN_* variables that apply only to
// unsupplied options can honor that contract.
type options struct {
	hosts         []string
	prefix        string
	cache         bool
	caseSensitive bool
	inherit       bool
	inheritPrefix string
	inheritKey    string
	inheritDepth  int
	watch         bool
	allowClear    bool
	logLevel      string
	fileCache     string

	prefixSet bool
	watchSet  bool
	depthSet  bool
}

// WithPrefix sets the namespace prefix every setting key is resolved under.
// The prefix is trimmed of whitespace and slashes and given exactly one
// trailing slash; a prefix that trims to nothing is the root "/".
func WithPrefix(prefix string) Option {
	return func(o *options) { o.prefix, o.prefixSet = prefix, true }
}

// WithHosts sets the etcd member list: a single comma-separated string, one
// member per argument, or a mix. TOXIN_ETCD, when set, overrides it.
func WithHosts(hosts ...string) Option {
	return func(o *options) { o.hosts = hosts }
}

// WithCache enables (the default) or disables the in-process settings
// cache.
func WithCache(on bool) Option {
	return func(o *options) { o.cache = on }
}

// WithCaseSensitive preserves key case through storage and lookup; without
// it (the default) keys are lowercased.
func WithCaseSensitive(on bool) Option {
	return func(o *options) { o.caseSensitive = on }
}

// WithInherit enables (the default) or disables inheritance. Inheritance
// resolves the parent prefix from the inherit key on first use; a depth of
// zero disables it outright.
func WithInherit(on bool) Option {
	return func(o *options) { o.inherit = on }
}

// WithInheritPrefix fixes the parent prefix at construction, instead of
// resolving it from the inherit key.
func WithInheritPrefix(prefix string) Option {
	return func(o *options) { o.inheritPrefix = prefix }
}

// WithInheritKey sets the key holding the parent prefix (default
// "config.inherit").
func WithInheritKey(key string) Option {
	return func(o *options) { o.inheritKey = key }
}

// WithInheritDepth sets how many levels of inheritance may be consumed
// (default two). TOXIN_INHERIT_DEPTH applies only when this option is not
// supplied.
func WithInheritDepth(depth int) Option {
	return func(o *options) { o.inheritDepth, o.depthSet = depth, true }
}

// WithWatch enables watching the namespace so etcd changes keep the cache
// up to date (the default is off). TOXIN_WATCH applies only when this
// option is not supplied. Watching is skipped with a warning when caching
// is disabled.
func WithWatch(on bool) Option {
	return func(o *options) { o.watch, o.watchSet = on, true }
}

// WithAllowClear permits Clear to delete the namespace from etcd; without
// it (the default) Clear refuses.
func WithAllowClear(on bool) Option {
	return func(o *options) { o.allowClear = on }
}

// WithLogLevel sets the log level, one of silly, debug, info, warn or
// critical (the default). TOXIN_LOGLEVEL overrides this option.
func WithLogLevel(level string) Option {
	return func(o *options) { o.logLevel = level }
}

// WithFileCache keeps a JSON file cache of the settings under dir.
// TOXIN_CACHE overrides this option.
func WithFileCache(dir string) Option {
	return func(o *options) { o.fileCache = dir }
}

// Config is a configuration bound to one namespace prefix in an etcd v3
// cluster. Its methods are safe for concurrent use.
type Config struct {
	prefix        string // normalized: "config/" or the root "/"
	client        *etcd.Client
	cacheOn       bool
	caseSensitive bool
	allowClear    bool
	inherit       bool
	inheritKey    string
	inheritDepth  int
	log           *log.Logger
	fileCache     *FileCache

	// parent is the resolved parent configuration: fixed at construction
	// for constructor-based inheritance, or resolved from the inherit key
	// on the first lookup that needs it. inherit turns false, permanently,
	// when inheritance is disabled by construction, by depth, or by rule 4
	// of the inheritance design.
	parent *Config

	mu    sync.Mutex
	cache map[string]any
	hooks []func()

	// resolveMu serializes the lazy resolution of parent.
	resolveMu sync.Mutex

	watchOnce sync.Once
	watcher   *etcd.Watcher
}

// New constructs a Config bound to the namespace prefix and the cluster.
//
// The member list defaults to 127.0.0.1:2379 (TOXIN_ETCD overrides any
// programmatic list), the prefix to "config/", caching on, case
// sensitivity off, inheritance on with inherit key "config.inherit" and
// depth two, watching off, clearing off, and the log level to critical.
// TOXIN_LOGLEVEL overrides the log level option and TOXIN_CACHE supplies
// the file cache directory; TOXIN_PREFIX, TOXIN_WATCH and
// TOXIN_INHERIT_DEPTH are consulted only when the corresponding option is
// not supplied. Watching starts at construction when enabled, and a
// configured file cache directory is created and opened at construction.
func New(opts ...Option) (*Config, error) {
	o := options{
		prefix:       "config/",
		cache:        true,
		inherit:      true,
		inheritKey:   "config.inherit",
		inheritDepth: 2,
		logLevel:     "critical",
	}
	for _, opt := range opts {
		opt(&o)
	}

	// An environment variable set to the empty string reads as unset.
	if v := os.Getenv("TOXIN_LOGLEVEL"); v != "" {
		o.logLevel = v
	}
	if v := os.Getenv("TOXIN_CACHE"); v != "" {
		o.fileCache = v
	}

	level, err := log.ParseLevel(o.logLevel)
	if err != nil {
		return nil, err
	}

	if !o.depthSet {
		if v := os.Getenv("TOXIN_INHERIT_DEPTH"); v != "" {
			depth, err := strconv.Atoi(v)
			if err != nil || depth < 0 {
				return nil, fmt.Errorf("invalid TOXIN_INHERIT_DEPTH %q", v)
			}
			o.inheritDepth = depth
		}
	}
	if o.inheritDepth < 0 {
		return nil, fmt.Errorf("inheritDepth must be an integer greater than or equal to 0")
	}
	if !o.prefixSet {
		if v := os.Getenv("TOXIN_PREFIX"); v != "" {
			o.prefix = v
		}
	}
	if !o.watchSet {
		// Any non-empty TOXIN_WATCH value is truthy.
		if v := os.Getenv("TOXIN_WATCH"); v != "" {
			o.watch = true
		}
	}

	c := &Config{
		prefix:        normalizePrefix(o.prefix, o.caseSensitive),
		cacheOn:       o.cache,
		caseSensitive: o.caseSensitive,
		allowClear:    o.allowClear,
		inheritKey:    o.inheritKey,
		inheritDepth:  o.inheritDepth,
		log:           log.New(os.Stderr, level),
		cache:         map[string]any{},
	}

	if o.fileCache != "" {
		if err := openFileCacheDir(o.fileCache); err != nil {
			return nil, err
		}
		c.fileCache = NewFileCache(c.prefix, o.fileCache)
	}

	client, err := etcd.New(o.hosts...)
	if err != nil {
		return nil, err
	}
	c.client = client

	switch {
	case o.inheritDepth == 0:
		// A depth of zero disables inheritance.
		c.inherit = false
	case o.inheritPrefix != "":
		// Constructor-based inheritance fixes the parent at construction,
		// sharing this config's cluster connection.
		c.inherit = true
		c.parent = c.makeParent(o.inheritPrefix)
	default:
		c.inherit = o.inherit
	}

	if o.watch {
		c.Watch()
	}
	return c, nil
}

// Prefix returns the normalized namespace prefix.
func (c *Config) Prefix() string { return c.prefix }

// Endpoints returns the resolved etcd member list the config is bound to.
func (c *Config) Endpoints() []string { return c.client.Endpoints() }

// openFileCacheDir creates the file cache directory and opens a file in it,
// so an unusable cache location surfaces at construction.
func openFileCacheDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("Could not open toxin cache: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ".toxin"), os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("Could not open toxin cache: %w", err)
	}
	return f.Close()
}

// normalizePrefix trims whitespace and slashes from the prefix and gives it
// exactly one trailing slash. A prefix that trims to nothing is the root
// "/". The prefix is lowercased unless case sensitivity is enabled.
func normalizePrefix(prefix string, caseSensitive bool) string {
	trimmed := strings.TrimSpace(prefix)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "/"
	}
	if !caseSensitive {
		trimmed = strings.ToLower(trimmed)
	}
	return trimmed + "/"
}

// coerceKey lowercases a setting key unless case sensitivity is enabled.
func (c *Config) coerceKey(key string) string {
	if c.caseSensitive {
		return key
	}
	return strings.ToLower(key)
}

// etcdKey names the etcd key for a setting key: the coerced key joined to
// the prefix, in the slash-directory form.
func (c *Config) etcdKey(key string) string {
	return etcd.NormalizeKey(c.prefix + c.coerceKey(key))
}

// namespace is the etcd-side boundary of this config's namespace, with its
// leading slash: "/config/" for prefix "config/", "/" for the root prefix.
func (c *Config) namespace() string {
	if c.prefix == "/" {
		return "/"
	}
	return "/" + c.prefix
}

// stripKey removes the namespace from a full etcd key, reporting whether
// the key lies inside the namespace. The remainder keeps the stored case;
// callers case-coerce as their contract requires.
func (c *Config) stripKey(key string) (string, bool) {
	ns := c.namespace()
	if !strings.HasPrefix(key, ns) {
		return "", false
	}
	return strings.TrimPrefix(key, ns), true
}

// makeParent builds the parent configuration for an inherit prefix: the
// same cluster, cache, case sensitivity and inherit key, one unit of
// inherit depth consumed, inheriting further only with depth remaining.
func (c *Config) makeParent(prefix string) *Config {
	depth := c.inheritDepth - 1
	p := &Config{
		prefix:        normalizePrefix(prefix, c.caseSensitive),
		client:        c.client,
		cacheOn:       c.cacheOn,
		caseSensitive: c.caseSensitive,
		allowClear:    c.allowClear,
		inheritKey:    c.inheritKey,
		inheritDepth:  depth,
		log:           c.log,
		cache:         map[string]any{},
	}
	p.inherit = depth > 0
	return p
}

// parentConfig returns the parent configuration, resolving it from the
// inherit key on the first lookup that needs it; the resolved parent is
// cached afterward. The inherit key is read from the config's own
// namespace, without consulting inheritance. A missing, non-string or
// unreadable inherit value disables inheritance for the rest of the
// config's life.
func (c *Config) parentConfig() *Config {
	if !c.inherit {
		return nil
	}
	c.resolveMu.Lock()
	defer c.resolveMu.Unlock()
	if c.parent != nil || !c.inherit {
		return c.parent
	}

	v, found, err := c.lookup(c.coerceKey(c.inheritKey), callConfig{
		cached:         true,
		cacheResult:    true,
		allowInherited: false,
	})
	switch {
	case err != nil:
		c.log.Warnf("Error getting inheritance value: %v", err)
		c.inherit = false
	case !found:
		c.inherit = false
	default:
		prefix, ok := v.(string)
		if !ok {
			c.inherit = false
			break
		}
		c.parent = c.makeParent(prefix)
	}
	return c.parent
}

// cachedValue returns a cached setting, reporting whether it was present.
func (c *Config) cachedValue(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.cache[key]
	return v, ok
}

// storeCached stores a setting in the cache.
func (c *Config) storeCached(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = value
}

// dropCached drops one cached setting.
func (c *Config) dropCached(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, key)
}

// emptyCache empties the cache.
func (c *Config) emptyCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]any{}
}

// cacheSnapshot copies the cache map.
func (c *Config) cacheSnapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]any, len(c.cache))
	for k, v := range c.cache {
		out[k] = v
	}
	return out
}
