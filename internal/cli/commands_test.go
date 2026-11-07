package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shakefu/toxin"
)

// cli.Get RETURN: prints the value returned for the key as two-space
// indented JSON.
func TestGetPrintsValueAsIndentedJSON(t *testing.T) {
	conf := newTestConfig(t, "tget/")
	if err := conf.Set("greeting", "hello world"); err != nil {
		t.Fatal(err)
	}
	if err := conf.Set("obj", map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runCLI("get", "tget/", "greeting")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "\"hello world\"\n" {
		t.Errorf("string value = %q", out)
	}

	out, _, code = runCLI("get", "tget/", "obj")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out != "{\n  \"a\": 1\n}\n" {
		t.Errorf("object value = %q", out)
	}
}

// cli.Get ERROR: an unset key exits one without printing anything.
func TestGetUnsetKeyExitsOneSilently(t *testing.T) {
	newTestConfig(t, "tget-unset/")
	out, errOut, code := runCLI("get", "tget-unset/", "missing")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" || errOut != "" {
		t.Errorf("stdout %q stderr %q, wanted nothing printed", out, errOut)
	}
}

// cli.Set SIDE-EFFECT/RETURN: sets the key to the value and prints
// nothing on success. The value is the command-line string, stored
// verbatim.
func TestSetWritesValueAndPrintsNothing(t *testing.T) {
	conf := newTestConfig(t, "tset/")

	out, errOut, code := runCLI("set", "tset/", "greeting", "hello world")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "" || errOut != "" {
		t.Errorf("stdout %q stderr %q, wanted nothing printed", out, errOut)
	}

	got, err := conf.Get("greeting", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("Get(greeting) = %#v", got)
	}

	// A numeric-looking value is stored verbatim, and reads parse JSON,
	// so it comes back as the number literal it spells.
	if _, _, code := runCLI("set", "tset/", "num", "10"); code != 0 {
		t.Fatalf("set num: exit %d", code)
	}
	got, err = conf.Get("num", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := got.(json.Number); !ok || n.String() != "10" {
		t.Errorf("Get(num) = %#v, wanted the number 10", got)
	}
}

// cli.Dump INPUT/RETURN: the prefix positional argument defaults to
// "config/", and the dump prints as two-space indented JSON.
func TestDumpDefaultPrefixPrintsIndentedJSON(t *testing.T) {
	conf := newTestConfig(t, "config/")
	if err := conf.Set("dumpdefault.alpha", "one"); err != nil {
		t.Fatal(err)
	}
	if err := conf.Set("dumpdefault.beta", 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clearer, err := toxin.New(toxin.WithPrefix("config/"), toxin.WithAllowClear(true))
		if err == nil {
			_ = clearer.Clear()
		}
	})

	out, errOut, code := runCLI("dump")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "{\n  \"dumpdefault.alpha\": \"one\",\n  \"dumpdefault.beta\": 2\n}\n" {
		t.Errorf("stdout = %q", out)
	}
}

// cli.Dump RETURN: an explicit prefix dumps that namespace.
func TestDumpExplicitPrefix(t *testing.T) {
	conf := newTestConfig(t, "tdump/")
	if err := conf.Set("key", "value"); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI("dump", "tdump/")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out != "{\n  \"key\": \"value\"\n}\n" {
		t.Errorf("stdout = %q", out)
	}
}

// cli.List INPUT/RETURN: the prefix defaults to "/" — the root — and the
// immediate children print as two-space indented JSON.
func TestListDefaultPrefixIsRoot(t *testing.T) {
	conf := newTestConfig(t, "tlist-root-a/")
	if err := conf.Set("x", "1"); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runCLI("list")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	var children []string
	if err := json.Unmarshal([]byte(out), &children); err != nil {
		t.Fatalf("list output is not a JSON array: %v\n%s", err, out)
	}
	found := false
	for _, c := range children {
		if c == "tlist-root-a/" {
			found = true
		}
	}
	if !found {
		t.Errorf("root listing lacks the created namespace:\n%s", out)
	}
}

// cli.List RETURN: with dirOnly false the listing carries leaves as-is
// and directories with a trailing slash.
func TestListLeavesAndDirectoriesAlike(t *testing.T) {
	conf := newTestConfig(t, "tlistd/")
	if err := conf.Set("leaf", "1"); err != nil {
		t.Fatal(err)
	}
	// Two keys below "sub/" make it a directory child.
	if err := conf.Set("sub/deep", "2"); err != nil {
		t.Fatal(err)
	}
	if err := conf.Set("sub/other", "3"); err != nil {
		t.Fatal(err)
	}

	out, _, code := runCLI("list", "tlistd/")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out != "[\n  \"leaf\",\n  \"sub/\"\n]\n" {
		t.Errorf("stdout = %q", out)
	}
}

// cli.Clear SIDE-EFFECT: with only the prefix, the namespace is cleared.
func TestClearClearsNamespace(t *testing.T) {
	conf := newTestConfig(t, "tclear/")
	if err := conf.Set("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := conf.Set("b.c", "2"); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runCLI("clear", "tclear/")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}

	children, err := conf.List("", toxin.DirOnly(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 0 {
		t.Errorf("namespace still lists %v", children)
	}
}

// cli.Clear RETURN: a second positional argument prints the JSON object
// with member "error" holding the joke, as two-space indented JSON, exits
// zero, and clears nothing.
func TestClearExtraArgumentPrintsErrorJSON(t *testing.T) {
	conf := newTestConfig(t, "tclear-extra/")
	if err := conf.Set("a", "1"); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runCLI("clear", "tclear-extra/", "oops")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "{\n  \"error\": \"You forgot a slash didn't you?\"\n}\n" {
		t.Errorf("stdout = %q", out)
	}

	got, err := conf.Get("a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1" {
		t.Errorf("the namespace was cleared despite the extra argument: %v", got)
	}
}

// cli.Load SIDE-EFFECT: the file's configuration loads into the
// namespace, written through to etcd.
func TestLoadWritesThroughToEtcd(t *testing.T) {
	conf := newTestConfig(t, "tload/")

	out, errOut, code := runCLI("load", "tload/", "testdata/load.json")
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}

	got, err := conf.Get("loaded.string", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "value" {
		t.Errorf("Get(loaded.string) = %#v", got)
	}
	// A nested map flattens to dot-notation keys.
	got, err = conf.Get("nested.key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Errorf("Get(nested.key) = %#v", got)
	}
}

// cli.Load ERROR: a file that cannot be found prints
// "No such file: <file>" to stderr and exits one.
func TestLoadNoSuchFile(t *testing.T) {
	newTestConfig(t, "tload-missing/")
	out, errOut, code := runCLI("load", "tload-missing/", "/nonexistent/config.json")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if errOut != "No such file: /nonexistent/config.json\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

// cli.Load ERROR: a file that cannot be parsed prints the parse error to
// stderr and exits one.
func TestLoadParseError(t *testing.T) {
	newTestConfig(t, "tload-broken/")
	out, errOut, code := runCLI("load", "tload-broken/", "testdata/load-broken.json")
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "unexpected end of JSON input") {
		t.Errorf("stderr = %q, wanted the JSON parse error", errOut)
	}
}
