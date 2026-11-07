package toxin

import (
	"os"
	"strings"
)

// SettingLookup reports whether a configuration holds a value for a
// setting key, and that value.
type SettingLookup func(setting string) (value any, found bool)

// Env resolves a setting from an environment variable before the
// configuration before a default.
//
// It checks the environment variable named by key first, then the
// setting named by the lowercased dotted form of key via get, then def.
// For example, with key "MY_DATABASE_HOST" the variable
// MY_DATABASE_HOST is checked first, then the setting
// "my.database.host", then def. A nil get skips the configuration step.
func Env(key string, get SettingLookup, def any) any {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	if get != nil {
		setting := strings.ReplaceAll(strings.ToLower(key), "_", ".")
		if v, ok := get(setting); ok {
			return v
		}
	}
	return def
}

// EnvKey maps a setting name to its per-setting environment override:
// the setting name uppercased with dots replaced by underscores, so
// "my.database.host" names MY_DATABASE_HOST. It returns the variable's
// value, or def when the variable is unset.
func EnvKey(setting, def string) string {
	if v, ok := os.LookupEnv(envKeyName(setting)); ok {
		return v
	}
	return def
}

// envKeyName forms the environment variable name for a setting by
// uppercasing the setting name and replacing dots with underscores.
func envKeyName(setting string) string {
	return strings.ReplaceAll(strings.ToUpper(setting), ".", "_")
}
