package toxin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// splitConfigFiles splits each entry on commas, trims whitespace and drops
// blank entries, the same list handling as WithHosts and TOXIN_ETCD.
func splitConfigFiles(entries ...string) []string {
	var out []string
	for _, entry := range entries {
		for _, p := range strings.Split(entry, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// readConfigFiles reads and parses every configured config file, in order,
// into one fresh flattened map; a later file's key wins over an earlier
// file's. Nothing in the Config is touched, so a failure leaves the live
// settings as they were. With no files configured it returns nil.
func (c *Config) readConfigFiles() (map[string]any, error) {
	if len(c.configFiles) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, path := range c.configFiles {
		obj, err := readConfigFile(path)
		if err != nil {
			return nil, err
		}
		if len(obj) == 0 {
			// An empty object contributes no keys.
			continue
		}
		// Within one file, keys that differ only by case collide under
		// case-insensitive mode and resolve in map iteration order, as with
		// Load(WithConfig(...)). Across files the later file still wins.
		for k, v := range flattenConfig(obj) {
			out[c.coerceKey(k)] = v
		}
	}
	return out, nil
}

// readConfigFile reads one JSON config file holding a single top-level
// object, keeping JSON numbers as json.Number. A missing or unreadable file,
// malformed JSON, trailing data, or a top-level value other than an object
// fails with "Could not read toxin config: <cause>", the cause naming the
// path.
func readConfigFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// The *fs.PathError already names the path.
		return nil, fmt.Errorf("Could not read toxin config: %w", err)
	}
	fail := func(err error) error {
		return fmt.Errorf("Could not read toxin config: %s: %w", path, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if err == io.EOF {
			err = errors.New("empty file, want a JSON object")
		}
		return nil, fail(err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fail(err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fail(fmt.Errorf("top-level JSON value is %s, want an object", jsonKind(v)))
	}
	return obj, nil
}

// jsonKind names the kind of a decoded JSON value for an error message.
func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case []any:
		return "an array"
	case string:
		return "a string"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// overlayConfigFiles stores the config-file values over the cache and
// records them as the active config-file overrides that watch events do
// not displace. A nil files map clears the overrides.
func (c *Config) overlayConfigFiles(files map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range files {
		c.cache[k] = v
	}
	c.fileOverrides = files
}

// fileOverridesSnapshot copies the active config-file overrides.
func (c *Config) fileOverridesSnapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]any, len(c.fileOverrides))
	for k, v := range c.fileOverrides {
		out[k] = v
	}
	return out
}

// isFileOverride reports whether a config file supplies the key. The caller
// holds c.mu.
func (c *Config) isFileOverride(key string) bool {
	_, ok := c.fileOverrides[key]
	return ok
}
