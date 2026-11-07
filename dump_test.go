package toxin

import (
	"errors"
	"testing"

	"github.com/shakefu/toxin/internal/etcd"
)

// 20-api@Config.Dump::RETURN: the dump is a map from prefix-stripped,
// case-coerced key to decoded value.
func TestDumpReturnsPrefixStrippedCaseCoercedDecodedMap(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/dump-ns/app.str", "plain")
	rawPut(t, raw, "/dump-ns/app.num", "42")
	rawPut(t, raw, "/dump-ns/app.list", "[1,2]")
	rawPut(t, raw, "/dump-ns/Mixed.Case", "kept-in-etcd")
	c := newTestConfig(t, WithPrefix("dump-ns/"))

	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if dump["app.str"] != "plain" {
		t.Errorf("dump[app.str] = %v, want %q", dump["app.str"], "plain")
	}
	if !jsonEqual(t, dump["app.num"], 42) {
		t.Errorf("dump[app.num] = %v, want the decoded 42", dump["app.num"])
	}
	if !jsonEqual(t, dump["app.list"], []any{1.0, 2.0}) {
		t.Errorf("dump[app.list] = %v, want the decoded list", dump["app.list"])
	}
	if dump["mixed.case"] != "kept-in-etcd" {
		t.Errorf("dump[mixed.case] = %v, want the case-coerced key", dump["mixed.case"])
	}
	if _, ok := dump["Mixed.Case"]; ok {
		t.Error("dump kept the un-coerced key form")
	}
}

// 20-api@Config.Dump::INPUT, conforming to 10-etcd3@etcd.Read::RETURN:
// when the key is the root and nothing is stored, an empty listing
// returns rather than an error.
func TestDumpRootWithNothingStoredReturnsEmptyListing(t *testing.T) {
	// Wipe the whole keyspace so the root truly holds nothing. Tests that
	// ran before this one are done with their data; later ones create
	// their own.
	raw := newRawClient(t)
	if _, _, err := raw.Delete(t.Context(), "/", true); err != nil {
		if !etcd.IsNotFound(err) {
			t.Fatalf("wiping the keyspace: %v", err)
		}
	}

	c := newTestConfig(t, WithPrefix("/"))
	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump of an empty root errored: %v", err)
	}
	if len(dump) != 0 {
		t.Errorf("Dump of an empty root = %v, want an empty map", dump)
	}
}

// 20-api@Config.Dump::INPUT: the whole subtree under the prefix is read
// as the etcd read produces it — keys below nested directories are
// included, prefix-stripped.
func TestDumpReadsTheWholeSubtree(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/dump-tree/flat.key", "flat")
	rawPut(t, raw, "/dump-tree/dir/nested.key", "nested")
	c := newTestConfig(t, WithPrefix("dump-tree/"))

	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if dump["flat.key"] != "flat" {
		t.Errorf("dump[flat.key] = %v, want %q", dump["flat.key"], "flat")
	}
	if dump["dir/nested.key"] != "nested" {
		t.Errorf("dump[dir/nested.key] = %v, want the subtree entry", dump["dir/nested.key"])
	}
}

// 10-etcd3@EtcdKeys, bound by toxin.New's USES through the API: a range
// over a key never matches a key that merely shares it as a string
// prefix — dumping "dump-bnd/" must not see "/dump-bnd-other/..." keys.
func TestDumpNeverMatchesMereStringPrefixes(t *testing.T) {
	raw := newRawClient(t)
	rawPut(t, raw, "/dump-bnd/inside.key", "inside")
	rawPut(t, raw, "/dump-bnd-other/outside.key", "outside")
	rawPut(t, raw, "/dump-bndother/outside2.key", "outside2")
	c := newTestConfig(t, WithPrefix("dump-bnd/"))

	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if dump["inside.key"] != "inside" {
		t.Errorf("dump[inside.key] = %v, want %q", dump["inside.key"], "inside")
	}
	for _, key := range dump {
		if key == "outside" || key == "outside2" {
			t.Errorf("dump matched a key beyond the slash boundary: %v", dump)
		}
	}
}

// root@toxin::INVARIANT: etcd failures surface to library callers as
// errors — an etcd read failure other than not-found is returned by Dump,
// not rendered as the not-found outcome.
func TestDumpSurfacesEtcdReadFailure(t *testing.T) {
	c := newTestConfig(t, WithPrefix("dump-fail/"))
	if err := c.client.Close(); err != nil {
		t.Fatalf("closing the client: %v", err)
	}

	_, err := c.Dump()
	if err == nil {
		t.Fatal("Dump succeeded against a closed cluster, want the etcd failure")
	}
	var etcdErr *etcd.Error
	if !errors.As(err, &etcdErr) {
		t.Errorf("error %q is not shaped as an etcd failure", err)
	}
}
