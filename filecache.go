package toxin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileCache is the JSON file cache for one configuration namespace. It
// stands alone from Config: it derives its cache file name from the
// prefix and the cache directory, saves the cache map as JSON, and
// loads cached entries back into a cache map.
type FileCache struct {
	dir    string
	prefix string
}

// NewFileCache returns a file cache for the namespace prefix that keeps
// its cache file under dir.
func NewFileCache(prefix, dir string) *FileCache {
	return &FileCache{dir: dir, prefix: prefix}
}

// Filename returns the cache file path: the prefix with leading and
// trailing slashes trimmed, ".." collapsed to "-", and a ".json"
// extension, keeping interior prefix slashes as directory separators
// nested under the cache directory.
func (fc *FileCache) Filename() string {
	name := strings.Trim(fc.prefix, "/")
	name = strings.ReplaceAll(name, "..", "-")
	return filepath.Join(fc.dir, name+".json")
}

// Save writes the cache map as JSON, creating the nested prefix
// directories as needed. It fails with the error
// "Could not write toxin cache: <cause>" when the cache file cannot be
// written.
func (fc *FileCache) Save(cache map[string]any) error {
	name := fc.Filename()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("Could not write toxin cache: %w", err)
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("Could not write toxin cache: %w", err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return fmt.Errorf("Could not write toxin cache: %w", err)
	}
	return nil
}

// Load reads the cache file and fills dst with its entries, only for
// keys not already present, so freshly loaded values win. It reports
// whether cached data was read; when no cache file exists it reports no
// cached data without error and leaves dst untouched. A cache file that
// exists but cannot be read fails with the error
// "Could not read toxin cache: <cause>".
func (fc *FileCache) Load(dst map[string]any) (bool, error) {
	data, err := os.ReadFile(fc.Filename())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("Could not read toxin cache: %w", err)
	}
	var cached map[string]any
	if err := json.Unmarshal(data, &cached); err != nil {
		return false, fmt.Errorf("Could not read toxin cache: %w", err)
	}
	if dst == nil {
		return true, nil
	}
	for key, value := range cached {
		if _, ok := dst[key]; !ok {
			dst[key] = value
		}
	}
	return true, nil
}
