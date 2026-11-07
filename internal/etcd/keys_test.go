package etcd

import "testing"

// EtcdKeys: normalize a key by stripping every leading '/' and prepending
// exactly one; the empty key names the root '/'.
func TestNormalizeKeyProducesSlashDirectoryForm(t *testing.T) {
	cases := map[string]string{
		"":                     "/",
		"/":                    "/",
		"//":                   "/",
		"///":                  "/",
		"config/app.setting":   "/config/app.setting",
		"/config/app.setting":  "/config/app.setting",
		"//config/app.setting": "/config/app.setting",
		// Inner slashes are kept; only leading ones are stripped.
		"//a//b": "/a//b",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
	// The design's example: prefix "config/" with key "app.setting" names
	// "/config/app.setting".
	if got := NormalizeKey("config/" + "app.setting"); got != "/config/app.setting" {
		t.Errorf("prefix + key = %q, want /config/app.setting", got)
	}
}

// EtcdKeys: a read or delete address additionally trims trailing '/'
// characters, keeping the root '/'.
func TestTrimKeyTrimsTrailingSlashesKeepingRoot(t *testing.T) {
	cases := map[string]string{
		"":              "/",
		"/":             "/",
		"//":            "/",
		"config/":       "/config",
		"/config/":      "/config",
		"/config//":     "/config",
		"///config///":  "/config",
		"/config//app/": "/config//app",
	}
	for in, want := range cases {
		if got := TrimKey(in); got != want {
			t.Errorf("TrimKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// EtcdKeys: children of a key K live strictly below "K/"; a range over K
// never matches a key that merely shares K as a string prefix.
func TestChildRangeNeverMatchesStringPrefixSibling(t *testing.T) {
	if got := childPrefix("/config"); got != "/config/" {
		t.Errorf("childPrefix(/config) = %q, want /config/", got)
	}
	if got := childPrefix("/"); got != "/" {
		t.Errorf("childPrefix(/) = %q, want /", got)
	}
	cases := []struct {
		addr, key string
		want      bool
	}{
		{"/config", "/configuration", false},
		{"/config", "/config", true},
		{"/config", "/config/", true},
		{"/config", "/config/a", true},
		{"/config", "/config/a/b", true},
		{"/", "/anything/at/all", true},
		{"/", "/", true},
	}
	for _, c := range cases {
		if got := inRange(c.addr, c.key); got != c.want {
			t.Errorf("inRange(%q, %q) = %v, want %v", c.addr, c.key, got, c.want)
		}
	}
}
