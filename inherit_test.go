package toxin

import (
	"strings"
	"testing"

	"github.com/shakefu/toxin/internal/testutil"
)

// setupInherit seeds a key-based inheritance chain on the real server:
// the child namespace holds an inherit key pointing at base, and base
// holds a value for the shared key.
func setupInherit(t *testing.T, childPrefix string) {
	t.Helper()
	raw := newRawClient(t)
	base := "inh-base-" + strings.TrimSuffix(childPrefix, "/")
	rawPut(t, raw, "/"+childPrefix+"config.inherit", base+"/")
	rawPut(t, raw, "/"+base+"/shared.key", "from-base")
}

// 20-api@Inheritance rule 1 / 20-api@Config.Get::RETURN: with the key
// unset in the child, the inherited chain is consulted and the parent's
// value returns.
func TestGetInheritsParentKey(t *testing.T) {
	setupInherit(t, "inh-child1/")
	c := newTestConfig(t, WithPrefix("inh-child1/"))

	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-base" {
		t.Errorf("inherited Get = %v, want %q", got, "from-base")
	}

	// The strict variant consults inheritance too.
	setupInherit(t, "inh-child1b/")
	c2 := newTestConfig(t, WithPrefix("inh-child1b/"))
	got, err = c2.GetStrict("shared.key")
	if err != nil {
		t.Fatalf("GetStrict: %v", err)
	}
	if got != "from-base" {
		t.Errorf("inherited GetStrict = %v, want %q", got, "from-base")
	}
}

// 20-api@Inheritance rule 5: resolution order is parent first — the
// child's own keys win over equal keys in the parent.
func TestInheritedLookupChildKeysWin(t *testing.T) {
	setupInherit(t, "inh-child2/")
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-child2/shared.key", "from-child")
	c := newTestConfig(t, WithPrefix("inh-child2/"))

	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-child" {
		t.Errorf("own-vs-inherited Get = %v, want the child's own %q", got, "from-child")
	}
}

// 20-api@Config.Get::INPUT: allowInherited false skips the inherited
// chain.
func TestGetAllowInheritedFalseSkipsTheChain(t *testing.T) {
	setupInherit(t, "inh-child3/")
	c := newTestConfig(t, WithPrefix("inh-child3/"))

	got, err := c.Get("shared.key", "default", AllowInherited(false))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get(allowInherited=false) = %v, want the default %q", got, "default")
	}
}

// 20-api@Inheritance rule 3: each level consumes one unit of depth; with
// depth one the grandparent is never consulted, and with depth zero
// inheritance is disabled outright.
func TestInheritDepthConsumedPerLevel(t *testing.T) {
	raw := newRawClient(t)
	// child -> base -> root, each hop one unit.
	rawPut(t, raw, "/inh-d1/config.inherit", "inh-d1-base")
	rawPut(t, raw, "/inh-d1-base/config.inherit", "inh-d1-root")
	rawPut(t, raw, "/inh-d1-root/deep.key", "from-root")

	// Depth two (the default) reaches the grandparent.
	deep := newTestConfig(t, WithPrefix("inh-d1/"))
	got, err := deep.Get("deep.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-root" {
		t.Errorf("depth-2 Get = %v, want the grandparent's %q", got, "from-root")
	}

	// Depth one stops at the parent.
	shallow := newTestConfig(t, WithPrefix("inh-d1/"), WithInheritDepth(1))
	got, err = shallow.Get("deep.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("depth-1 Get = %v, want the default %q", got, "default")
	}

	// Depth zero disables inheritance entirely.
	off := newTestConfig(t, WithPrefix("inh-d1/"), WithInheritDepth(0))
	got, err = off.Get("deep.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("depth-0 Get = %v, want the default %q", got, "default")
	}
}

// 20-api@toxin.New::INPUT: TOXIN_INHERIT_DEPTH is consulted when the
// inherit depth option is not supplied, so a valid value there is applied —
// depth one stops inheritance one level earlier than the default two.
func TestNewAppliesValidTOXINInheritDepth(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-env/config.inherit", "inh-env-base")
	rawPut(t, raw, "/inh-env-base/config.inherit", "inh-env-root")
	rawPut(t, raw, "/inh-env-root/deep.key", "from-root")

	// The default depth two reaches the grandparent.
	deep := newTestConfig(t, WithPrefix("inh-env/"))
	got, err := deep.Get("deep.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-root" {
		t.Errorf("default-depth Get = %v, want the grandparent's %q", got, "from-root")
	}

	// TOXIN_INHERIT_DEPTH=1 stops at the parent, one level earlier.
	clearToxinEnv(t)
	t.Setenv("TOXIN_ETCD", testutil.Etcd(t))
	t.Setenv("TOXIN_INHERIT_DEPTH", "1")
	shallow, err := New(WithPrefix("inh-env/"))
	if err != nil {
		t.Fatalf("New with TOXIN_INHERIT_DEPTH=1: %v", err)
	}
	if shallow.inheritDepth != 1 {
		t.Errorf("inheritDepth = %d, want 1 applied from TOXIN_INHERIT_DEPTH", shallow.inheritDepth)
	}
	got, err = shallow.Get("deep.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("depth-from-env Get = %v, want inheritance stopped one level earlier than the default (%q)", got, "default")
	}
}

// 20-api@Inheritance rule 4: an unreadable inherit value — the inherit-key
// read itself failing — disables inheritance for the rest of the config's
// life.
func TestUnreadableInheritValueDisablesInheritanceForLife(t *testing.T) {
	setupInherit(t, "inh-unreadable/")
	c := newTestConfig(t, WithPrefix("inh-unreadable/"))
	buf := captureLog(c)

	// Dump consults inheritance before its own read, so with the cluster
	// connection closed its first etcd contact is the inherit-key read —
	// which fails without a not-found shape.
	if err := c.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}
	_, _ = c.Dump()
	if !strings.Contains(buf.String(), "Error getting inheritance value") {
		t.Errorf("warning %q does not report the failed inherit-key read", buf.String())
	}

	// Inheritance stays disabled for later lookups: the connection works
	// again, the chain is resolvable, and the parent's value is still not
	// consulted.
	c.client = newRawClient(t)
	got, err := c.Get("shared.key", "default", Cached(false))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get after an unreadable inherit key = %v, want inheritance disabled for life (%q)", got, "default")
	}
	if c.inherit {
		t.Error("inherit is still enabled after an unreadable inherit value")
	}
}

// 20-api@Inheritance rule 2: a string passed as the inherit option fixes
// the parent prefix at construction.
func TestConstructorInheritPrefixFixesParentAtConstruction(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-ctor-base/shared.key", "from-ctor-base")
	// No inherit key anywhere in the child namespace.
	c := newTestConfig(t, WithPrefix("inh-ctor-child/"), WithInheritPrefix("inh-ctor-base/"))

	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-ctor-base" {
		t.Errorf("constructor-inherited Get = %v, want %q", got, "from-ctor-base")
	}
	if c.parent == nil || c.parent.prefix != "inh-ctor-base/" {
		t.Errorf("parent prefix = %v, want it fixed at construction", c.parent)
	}
}

// 20-api@Inheritance rule 4: a missing inherit value disables
// inheritance for the rest of the config's life.
func TestMissingInheritValueDisablesInheritanceForLife(t *testing.T) {
	c := newTestConfig(t, WithPrefix("inh-miss/"))

	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get with no inherit key = %v, want the default %q", got, "default")
	}

	// The inherit key appears later; inheritance stays disabled. The
	// bypassed cache keeps the earlier cached default out of the answer.
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-miss/config.inherit", "inh-miss-base")
	rawPut(t, raw, "/inh-miss-base/shared.key", "from-late-base")
	got, err = c.Get("shared.key", "default", Cached(false))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get after late inherit key = %v, want inheritance still disabled", got)
	}
}

// 20-api@Inheritance rule 4: a non-string inherit value disables
// inheritance.
func TestNonStringInheritValueDisablesInheritance(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-nonstr/config.inherit", "42")
	rawPut(t, raw, "/42/shared.key", "from-numbered-base")
	c := newTestConfig(t, WithPrefix("inh-nonstr/"))

	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "default" {
		t.Errorf("Get with a numeric inherit value = %v, want the default %q", got, "default")
	}
}

// 20-api@Inheritance rule 1: the resolved parent is cached — a later
// change to the inherit key does not re-resolve it.
func TestResolvedParentIsCachedAfterFirstUse(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-cache/config.inherit", "inh-cache-a")
	rawPut(t, raw, "/inh-cache-a/shared.key", "from-a")
	rawPut(t, raw, "/inh-cache-b/shared.key", "from-b")
	c := newTestConfig(t, WithPrefix("inh-cache/"))

	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-a" {
		t.Fatalf("first inherited Get = %v, want %q", got, "from-a")
	}

	rawPut(t, raw, "/inh-cache/config.inherit", "inh-cache-b")
	c.emptyCache()
	got, err = c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-a" {
		t.Errorf("Get after inherit key change = %v, want the cached parent's %q", got, "from-a")
	}
}

// 20-api@Inheritance rule 6: a parent namespace that holds nothing is not
// an error; the child simply has no parent values.
func TestEmptyParentNamespaceIsNotAnError(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-emptyp/config.inherit", "inh-nothing-here")
	rawPut(t, raw, "/inh-emptyp/own.key", "own")
	c := newTestConfig(t, WithPrefix("inh-emptyp/"))

	got, err := c.Get("own.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "own" {
		t.Errorf("own Get with empty parent = %v, want %q", got, "own")
	}
	got, err = c.Get("absent.key", "default")
	if err != nil {
		t.Fatalf("Get against an empty parent namespace errored: %v", err)
	}
	if got != "default" {
		t.Errorf("Get against an empty parent = %v, want the default %q", got, "default")
	}

	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump against an empty parent errored: %v", err)
	}
	if dump["own.key"] != "own" {
		t.Errorf("Dump with empty parent = %v, want the own key present", dump)
	}
}

// 20-api@Config.Dump::RETURN: with inheritance allowed the parent dump is
// overlaid first, so own keys win.
func TestDumpOverlaysParentFirstOwnKeysWin(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-dump-base/shared.key", "base")
	rawPut(t, raw, "/inh-dump-base/base.only", "base")
	rawPut(t, raw, "/inh-dump/config.inherit", "inh-dump-base")
	rawPut(t, raw, "/inh-dump/shared.key", "child")
	rawPut(t, raw, "/inh-dump/child.only", "child")
	c := newTestConfig(t, WithPrefix("inh-dump/"))

	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if dump["shared.key"] != "child" {
		t.Errorf("dump[shared.key] = %v, want the child's own %q", dump["shared.key"], "child")
	}
	if dump["base.only"] != "base" {
		t.Errorf("dump[base.only] = %v, want the parent's %q", dump["base.only"], "base")
	}
	if dump["child.only"] != "child" {
		t.Errorf("dump[child.only] = %v, want %q", dump["child.only"], "child")
	}

	// allowInherited false dumps the own namespace alone.
	own, err := c.Dump(AllowInherited(false))
	if err != nil {
		t.Fatalf("Dump(allowInherited=false): %v", err)
	}
	if _, ok := own["base.only"]; ok {
		t.Error("Dump(allowInherited=false) includes parent keys")
	}
}

// 20-api@toxin.New: WithInheritKey renames the key holding the parent
// prefix; the default key is not consulted when a custom one is set.
func TestWithInheritKeyRenamesTheInheritKey(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/inh-childkey/custom.parent", "inh-base-key/")
	rawPut(t, raw, "/inh-base-key/shared.key", "from-custom-key")
	// A parent under the default key must be ignored.
	rawPut(t, raw, "/inh-childkey/config.inherit", "inh-base-def/")
	rawPut(t, raw, "/inh-base-def/shared.key", "from-default-key")

	c := newTestConfig(t, WithPrefix("inh-childkey/"), WithInheritKey("custom.parent"))
	got, err := c.Get("shared.key", "default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-custom-key" {
		t.Errorf("custom inherit key Get = %v, want %q", got, "from-custom-key")
	}
}
