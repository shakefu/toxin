package toxin

import "sync"

// The process-wide Config the package-level shortcuts operate on,
// constructed and loaded on first use rather than at package
// initialization.
var (
	defaultMu   sync.Mutex
	defaultConf *Config
	defaultErr  error
)

// Default returns the one process-wide Config shared by every caller in
// the process, constructing and loading it on first use. Its construction
// options come from the TOXIN_* environment. When construction fails the
// failure is returned to every caller; when only the initial load fails
// the Config is kept and the failure is reported to the first caller
// alone.
func Default() (*Config, error) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultConf == nil {
		if defaultErr != nil {
			// An earlier construction failed; the process environment has
			// not changed.
			return nil, defaultErr
		}
		conf, err := New()
		if err != nil {
			defaultErr = err
			return nil, err
		}
		defaultConf = conf
		_, loadErr := conf.Load()
		if loadErr != nil {
			return conf, loadErr
		}
		return conf, nil
	}
	return defaultConf, nil
}

// defaultConfig returns the shared instance for a package-level shortcut,
// failing when it could not be constructed.
func defaultConfig() (*Config, error) {
	c, err := Default()
	if err != nil && c == nil {
		return nil, err
	}
	return c, nil
}

// Get returns the value of the setting key on the process-wide Config, or
// def when the key is unset. See Config.Get.
func Get(key string, def any, opts ...CallOption) (any, error) {
	c, err := defaultConfig()
	if err != nil {
		return nil, err
	}
	return c.Get(key, def, opts...)
}

// GetStrict returns the value of the setting key on the process-wide
// Config, rejecting an unset key with 'No setting "<key>"'. See
// Config.GetStrict.
func GetStrict(key string, opts ...CallOption) (any, error) {
	c, err := defaultConfig()
	if err != nil {
		return nil, err
	}
	return c.GetStrict(key, opts...)
}

// Set stores value under the setting key on the process-wide Config. See
// Config.Set.
func Set(key string, value any, opts ...CallOption) error {
	c, err := defaultConfig()
	if err != nil {
		return err
	}
	return c.Set(key, value, opts...)
}

// Clear clears the process-wide Config's namespace. See Config.Clear.
func Clear(opts ...CallOption) error {
	c, err := defaultConfig()
	if err != nil {
		return err
	}
	return c.Clear(opts...)
}

// Reload loads the process-wide configuration anew; with clear set the
// cached settings are wiped first. See Config.Reload.
func Reload(clear bool) error {
	c, err := defaultConfig()
	if err != nil {
		return err
	}
	return c.Reload(clear)
}

// ReloadHook registers a function to run, bare, after every subsequent
// load of the process-wide configuration, in registration order. See
// Config.AddReloadHook.
func ReloadHook(hook func()) {
	c, err := defaultConfig()
	if err != nil {
		return
	}
	c.AddReloadHook(hook)
}
