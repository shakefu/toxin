package toxin

import (
	"os"
	"testing"
)

// unsetenv unsets an environment variable for the duration of a test,
// restoring its prior state afterward.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			if err := os.Setenv(key, orig); err != nil {
				t.Errorf("restore %s: %v", key, err)
			}
		} else if err := os.Unsetenv(key); err != nil {
			t.Errorf("restore unset %s: %v", key, err)
		}
	})
}

// 20-api@toxin.Env::INPUT: the environment variable named by the key is
// checked first, ahead of the configuration and the default.
func TestEnvChecksEnvironmentVariableNamedByKeyFirst(t *testing.T) {
	t.Setenv("TOXIN_TEST_SETTING", "from-env")

	got := Env("TOXIN_TEST_SETTING", func(setting string) (any, bool) {
		return "from-config:" + setting, true
	}, "from-default")

	if got != "from-env" {
		t.Errorf("Env = %v, want %q", got, "from-env")
	}
}

// 20-api@toxin.Env::INPUT: when the variable is unset, the setting
// named by the lowercased dotted form of the key is checked next.
func TestEnvFallsBackToSettingNamedByLowercasedDottedFormOfKey(t *testing.T) {
	unsetenv(t, "TOXIN_TEST_SETTING")
	var lookedUp string
	get := func(setting string) (any, bool) {
		lookedUp = setting
		return 42, true
	}

	got := Env("TOXIN_TEST_SETTING", get, "from-default")

	if lookedUp != "toxin.test.setting" {
		t.Errorf("lookup setting = %q, want %q", lookedUp, "toxin.test.setting")
	}
	if got != 42 {
		t.Errorf("Env = %v, want 42", got)
	}
}

// 20-api@toxin.Env::INPUT: when both the variable and the setting are
// absent, the given default is returned.
func TestEnvFallsBackToGivenDefault(t *testing.T) {
	unsetenv(t, "TOXIN_TEST_SETTING")
	got := Env("TOXIN_TEST_SETTING", func(setting string) (any, bool) {
		return nil, false
	}, "from-default")

	if got != "from-default" {
		t.Errorf("Env = %v, want %q", got, "from-default")
	}
}

// 20-api@toxin.Env::INPUT: the mapping from setting name to variable
// name is exposed as the companion helper EnvKey, which consults the
// variable the setting maps to.
func TestEnvExposesSettingNameToVariableNameMappingAsEnvKey(t *testing.T) {
	t.Setenv("MY_DATABASE_HOST", "db.example.com")

	if got := EnvKey("my.database.host", "fallback"); got != "db.example.com" {
		t.Errorf("EnvKey = %q, want %q", got, "db.example.com")
	}
}

// 20-api@toxin.EnvKey::RETURN: the variable name is formed by
// uppercasing the setting name and replacing dots with underscores.
func TestEnvKeyFormsVariableNameByUppercasingAndReplacingDots(t *testing.T) {
	t.Setenv("MY_DATABASE_HOST", "db.example.com")

	// A mixed-case dotted setting must resolve through the uppercased,
	// underscored variable name.
	if got := EnvKey("My.Database.Host", "fallback"); got != "db.example.com" {
		t.Errorf("EnvKey = %q, want %q", got, "db.example.com")
	}
}

// 20-api@toxin.EnvKey::RETURN: the variable's value is returned when
// the variable is set.
func TestEnvKeyReturnsVariablesValue(t *testing.T) {
	t.Setenv("MY_DATABASE_HOST", "db.example.com")

	if got := EnvKey("my.database.host", "fallback"); got != "db.example.com" {
		t.Errorf("EnvKey = %q, want %q", got, "db.example.com")
	}
}

// 20-api@toxin.EnvKey::RETURN: the default is returned when the
// variable is unset.
func TestEnvKeyReturnsDefaultWhenVariableUnset(t *testing.T) {
	unsetenv(t, "TOXIN_TEST_UNSET_VARIABLE")

	if got := EnvKey("toxin.test.unset.variable", "fallback"); got != "fallback" {
		t.Errorf("EnvKey = %q, want %q", got, "fallback")
	}
}
