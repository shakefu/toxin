package toxin

// CallOption tunes one Config call. Which options apply depends on the
// method: Get and GetStrict consult cached, cacheResult, cacheOnly and
// allowInherited; Set consults ttl and cacheOnly; Dump consults
// allowInherited; Load consults config, cacheOnly, merge, allowInherited
// and fileCacheOnly; List consults dirOnly; Clear consults cacheOnly.
type CallOption func(*callConfig)

// callConfig carries every per-call option. Each method fills its own
// defaults and applies the options over them.
type callConfig struct {
	// Get: consult the cache (default true).
	cached bool
	// Get: store the result in the cache (default true, effective only
	// while caching is enabled).
	cacheResult bool
	// Get, Set, Load, Clear: touch only the cache (default false).
	cacheOnly bool
	// Get, Dump, Load: consult inheritance (default true).
	allowInherited bool
	// Set: seconds for a one-shot lease (zero for none).
	ttl int64
	// Load: merge into the cache rather than clearing it first (default
	// true).
	merge bool
	// Load: never contact etcd (default false).
	fileCacheOnly bool
	// List: return only directory children (default true).
	dirOnly bool
	// Load: the nested configuration map to load.
	config    map[string]any
	hasConfig bool
}

// Cached sets whether Get consults the cache for the key. It defaults to
// true.
func Cached(on bool) CallOption {
	return func(o *callConfig) { o.cached = on }
}

// CacheResult sets whether Get stores the result — a found value or the
// default — in the cache. It defaults to true, effective while caching is
// enabled.
func CacheResult(on bool) CallOption {
	return func(o *callConfig) { o.cacheResult = on }
}

// CacheOnly sets whether the call touches only the cache, never etcd. It
// defaults to false; Load defaults it to true.
func CacheOnly(on bool) CallOption {
	return func(o *callConfig) { o.cacheOnly = on }
}

// AllowInherited sets whether the call consults inheritance. It defaults
// to true.
func AllowInherited(on bool) CallOption {
	return func(o *callConfig) { o.allowInherited = on }
}

// WithTTL grants the written key a fresh one-shot lease of the given
// seconds, so it expires that long after its most recent write. A ttl
// below one writes without a lease.
func WithTTL(seconds int64) CallOption {
	return func(o *callConfig) { o.ttl = seconds }
}

// Merge sets whether Load merges the loaded keys into the cache rather
// than clearing the cache first. It defaults to true.
func Merge(on bool) CallOption {
	return func(o *callConfig) { o.merge = on }
}

// FileCacheOnly sets whether Load never contacts etcd, reading at most the
// file cache. It defaults to false.
func FileCacheOnly(on bool) CallOption {
	return func(o *callConfig) { o.fileCacheOnly = on }
}

// DirOnly sets whether List returns only directory children. It defaults
// to true.
func DirOnly(on bool) CallOption {
	return func(o *callConfig) { o.dirOnly = on }
}

// WithConfig supplies Load a nested configuration map, in place of loading
// the etcd configuration under the prefix.
func WithConfig(config map[string]any) CallOption {
	return func(o *callConfig) { o.config, o.hasConfig = config, true }
}

// applyCalls fills defaults and applies options.
func getDefaults(opts []CallOption) callConfig {
	o := callConfig{cached: true, cacheResult: true, allowInherited: true}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func setDefaults(opts []CallOption) callConfig {
	var o callConfig
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func dumpDefaults(opts []CallOption) callConfig {
	o := callConfig{allowInherited: true}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func loadDefaults(opts []CallOption) callConfig {
	o := callConfig{cacheOnly: true, merge: true, allowInherited: true}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func listDefaults(opts []CallOption) callConfig {
	o := callConfig{dirOnly: true}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func clearDefaults(opts []CallOption) callConfig {
	var o callConfig
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
