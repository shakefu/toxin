package etcd

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.etcd.io/etcd/client/v3"
)

// etcd.Read ERROR: when nothing matches and the key is not the root,
// signal not-found — a v2-style error whose message is "Key not found",
// whose error code is one hundred and whose cause is the addressed key.
func TestReadMissingKeySignalsNotFound(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	for _, recursive := range []bool{false, true} {
		_, err := c.Read(ctx, "/read-nothing-here", recursive)
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("Read(recursive=%v) error = %#v, want *Error", recursive, err)
		}
		if e.Msg != "Key not found" {
			t.Errorf("message = %q, want %q", e.Msg, "Key not found")
		}
		if e.Code != 100 {
			t.Errorf("code = %d, want 100", e.Code)
		}
		if e.Cause != "/read-nothing-here" {
			t.Errorf("cause = %q, want the addressed key /read-nothing-here", e.Cause)
		}
		if !IsNotFound(err) {
			t.Errorf("IsNotFound = false, want true")
		}
	}

	// The read address trims trailing slashes, so the cause carries the
	// trimmed address.
	_, err := c.Read(ctx, "/read-nothing-here//", false)
	var e *Error
	if !errors.As(err, &e) || e.Cause != "/read-nothing-here" {
		t.Errorf("Read of trailing-slash key: %#v, want cause /read-nothing-here", err)
	}
}

// etcd.Read RETURN: when the key itself holds a value and either the
// recursive flag is unset or no entries exist below it, return the exact
// stored entry.
func TestReadKeyWithValueReturnsExactStoredEntry(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	rev := mustPut(t, c, "/read-exact/key", "hello world")

	for _, recursive := range []bool{false, true} {
		entries, err := c.Read(ctx, "/read-exact/key", recursive)
		if err != nil {
			t.Fatalf("Read(recursive=%v): %v", recursive, err)
		}
		want := []entryFacts{{Key: "/read-exact/key", Value: "hello world"}}
		if got := summarize(entries); !reflect.DeepEqual(got, want) {
			t.Errorf("Read(recursive=%v) = %+v, want %+v", recursive, got, want)
		}
		if entries[0].Created != rev || entries[0].Modified != rev {
			t.Errorf("entry revisions = created %d modified %d, want the write revision %d",
				entries[0].Created, entries[0].Modified, rev)
		}
	}
	// Leading and trailing slashes resolve to the same address.
	entries, err := c.Read(ctx, "//read-exact/key//", false)
	if err != nil || len(entries) != 1 || entries[0].Value != "hello world" {
		t.Errorf("Read of //read-exact/key// = %+v, %v; want the exact stored entry", entries, err)
	}
}

// etcd.Read RETURN: with the recursive flag unset a key that holds a value
// answers with that exact entry even when entries also exist below it.
func TestReadNonRecursiveWithValueAndChildrenReturnsExactStoredEntry(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	mustPut(t, c, "/read-both/k", "self")
	mustPut(t, c, "/read-both/k/child", "c")

	entries, err := c.Read(ctx, "/read-both/k", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []entryFacts{{Key: "/read-both/k", Value: "self"}}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

// etcd.Read RETURN: with the recursive flag set and entries below the key,
// return every entry strictly below "key/", dropping the key's own value
// when it holds one.
func TestReadRecursiveReturnsEntriesStrictlyBelowKey(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	mustPut(t, c, "/read-sub/k", "own value, dropped")
	mustPut(t, c, "/read-sub/k/one", "1")
	mustPut(t, c, "/read-sub/k/two/inner", "2")

	entries, err := c.Read(ctx, "/read-sub/k", true)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []entryFacts{
		{Key: "/read-sub/k/one", Value: "1"},
		{Key: "/read-sub/k/two/inner", Value: "2"},
	}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("Read recursive = %+v, want %+v", got, want)
	}
}

// etcd.Read RETURN: with the recursive flag unset and the key itself
// holding no value, return the immediate children of the key, one path
// segment deeper.
func TestReadNonRecursiveWithoutValueReturnsImmediateChildren(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	mustPut(t, c, "/read-dir/a/x", "1")
	mustPut(t, c, "/read-dir/a/y", "2")
	mustPut(t, c, "/read-dir/b", "bval")

	entries, err := c.Read(ctx, "/read-dir", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []entryFacts{
		{Key: "/read-dir/a", Dir: true},
		{Key: "/read-dir/b", Value: "bval"},
	}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("Read non-recursive = %+v, want %+v", got, want)
	}
}

// etcd.Read RETURN: order multiple returned entries by creation revision,
// breaking ties by lexicographic key order.
func TestReadOrdersEntriesByCreationRevisionThenKey(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	// Creation order c, a, b: the listing follows creation, not key
	// order.
	mustPut(t, c, "/read-ord/c", "3")
	mustPut(t, c, "/read-ord/a", "1")
	mustPut(t, c, "/read-ord/b", "2")
	entries, err := c.Read(ctx, "/read-ord", true)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []entryFacts{
		{Key: "/read-ord/c", Value: "3"},
		{Key: "/read-ord/a", Value: "1"},
		{Key: "/read-ord/b", Value: "2"},
	}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v, want %+v", got, want)
	}

	// Two keys written in one revision tie on creation revision; the tie
	// breaks by key order.
	tieRoot := "/read-tie"
	_, err = c.raw.Txn(ctx).
		Then(
			clientv3.OpPut(tieRoot+"/z", "late"),
			clientv3.OpPut(tieRoot+"/a", "early"),
		).
		Commit()
	if err != nil {
		t.Fatalf("txn put: %v", err)
	}
	entries, err = c.Read(ctx, tieRoot, true)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want = []entryFacts{
		{Key: tieRoot + "/a", Value: "early"},
		{Key: tieRoot + "/z", Value: "late"},
	}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("Read of one-revision tie = %+v, want %+v", got, want)
	}
	if entries[0].Created != entries[1].Created {
		t.Errorf("tie entries have creation revisions %d and %d, want one shared revision",
			entries[0].Created, entries[1].Created)
	}
}

// etcd.Read RETURN: when the key is the root and nothing is stored, return
// an empty listing rather than an error.
func TestReadRootWithNothingStoredReturnsEmptyListing(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	// Wipe whatever earlier tests stored so the root is really empty.
	if _, _, err := c.Delete(ctx, "/", true); err != nil && !IsNotFound(err) {
		t.Fatalf("clearing the root: %v", err)
	}

	for _, recursive := range []bool{true, false} {
		entries, err := c.Read(ctx, "/", recursive)
		if err != nil {
			t.Fatalf("Read(/, recursive=%v): %v", recursive, err)
		}
		if len(entries) != 0 {
			t.Errorf("Read(/, recursive=%v) = %+v, want an empty listing", recursive, summarize(entries))
		}
	}

	// The root listing is the root's immediate children once entries
	// exist below it.
	mustPut(t, c, "/read-root/a/x", "1")
	mustPut(t, c, "/read-root/b", "2")
	entries, err := c.Read(ctx, "/", false)
	if err != nil {
		t.Fatalf("Read(/): %v", err)
	}
	want := []entryFacts{
		{Key: "/read-root", Dir: true},
	}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("Read(/) = %+v, want %+v", got, want)
	}
}

// etcd.Read INVARIANT: a subtree read never matches a key that merely
// shares the addressed key as a string prefix.
func TestReadNeverMatchesStringPrefixSibling(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	mustPut(t, c, "/read-pfx/a", "1")
	mustPut(t, c, "/read-pfxation/b", "2")

	entries, err := c.Read(ctx, "/read-pfx", true)
	if err != nil {
		t.Fatalf("Read recursive: %v", err)
	}
	want := []entryFacts{{Key: "/read-pfx/a", Value: "1"}}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("recursive Read = %+v, want %+v and no /read-pfxation entries", got, want)
	}

	entries, err = c.Read(ctx, "/read-pfx", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := summarize(entries); !reflect.DeepEqual(got, []entryFacts{{Key: "/read-pfx/a", Value: "1"}}) {
		t.Errorf("non-recursive Read = %+v, want only the immediate child a", got)
	}

	// The sibling still answers its own reads.
	entries, err = c.Read(ctx, "/read-pfxation/b", false)
	if err != nil || len(entries) != 1 || entries[0].Value != "2" {
		t.Errorf("Read of the sibling = %+v, %v; want its own exact entry", entries, err)
	}
}
