package etcd

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// etcd.Delete ERROR: when the count of deleted entries is zero, signal
// not-found — message "Key not found", code one hundred, cause the
// addressed key.
func TestDeleteOfNothingSignalsNotFound(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	for _, recursive := range []bool{false, true} {
		_, _, err := c.Delete(ctx, "/delete-gone", recursive)
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("Delete(recursive=%v) error = %#v, want *Error", recursive, err)
		}
		if e.Msg != "Key not found" {
			t.Errorf("message = %q, want %q", e.Msg, "Key not found")
		}
		if e.Code != 100 {
			t.Errorf("code = %d, want 100", e.Code)
		}
		if e.Cause != "/delete-gone" {
			t.Errorf("cause = %q, want the addressed key /delete-gone", e.Cause)
		}
	}

	// The delete address trims trailing slashes.
	_, _, err := c.Delete(ctx, "/delete-gone///", false)
	var e *Error
	if !errors.As(err, &e) || e.Cause != "/delete-gone" {
		t.Errorf("Delete of trailing-slash key: %#v, want cause /delete-gone", err)
	}
}

// etcd.Delete SIDE-EFFECT: with the recursive flag unset, delete the exact
// key only.
func TestDeleteNonRecursiveDeletesExactKeyOnly(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	mustPut(t, c, "/delete-one/k", "v")
	mustPut(t, c, "/delete-one/k/child", "c")
	mustPut(t, c, "/delete-oneother", "sibling")

	rev, count, err := c.Delete(ctx, "/delete-one/k", false)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if count != 1 {
		t.Errorf("deleted %d entries, want exactly the one key", count)
	}
	if rev == 0 {
		t.Error("returned revision 0, want the revision the delete landed on")
	}
	// The child and the string-prefix sibling survive untouched.
	if _, err := c.Read(ctx, "/delete-one/k/child", false); err != nil {
		t.Errorf("child read after non-recursive delete: %v, want it untouched", err)
	}
	if _, err := c.Read(ctx, "/delete-oneother", false); err != nil {
		t.Errorf("sibling read after non-recursive delete: %v, want it untouched", err)
	}
	// The deleted key itself is gone: a non-recursive read of it now
	// answers with the surviving child's listing, never an entry for the
	// deleted key.
	entries, err := c.Read(ctx, "/delete-one/k", false)
	if err != nil {
		t.Fatalf("read of the deleted key: %v", err)
	}
	want := []entryFacts{{Key: "/delete-one/k/child", Value: "c"}}
	if got := summarize(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("read of the deleted key = %+v, want the child listing %+v", got, want)
	}
}

// etcd.Delete SIDE-EFFECT: with the recursive flag set and the key not the
// root, delete the exact key and everything below "key/" in a single
// transaction that lands on one revision.
func TestDeleteRecursiveRemovesKeyAndSubtreeInOneRevision(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	mustPut(t, c, "/delete-tree", "own value")
	mustPut(t, c, "/delete-tree/a", "1")
	mustPut(t, c, "/delete-tree/b/inner", "2")
	mustPut(t, c, "/delete-treeother", "sibling")

	rev, count, err := c.Delete(ctx, "/delete-tree", true)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if count != 3 {
		t.Errorf("deleted %d entries, want the key and its 2 descendants", count)
	}
	if rev == 0 {
		t.Error("returned revision 0, want the revision the delete landed on")
	}
	if _, err := c.Read(ctx, "/delete-tree", true); !IsNotFound(err) {
		t.Errorf("recursive read of the deleted subtree: %v, want not-found", err)
	}
	// A key that merely shares the string prefix survives.
	if _, err := c.Read(ctx, "/delete-treeother", false); err != nil {
		t.Errorf("sibling read: %v, want it untouched", err)
	}
}

// etcd.Delete SIDE-EFFECT: with the recursive flag set and the key the
// root, delete the entire keyspace in one range delete.
func TestDeleteRecursiveRootClearsKeyspace(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	// Start from an empty keyspace so the deleted count is exact whatever
	// earlier tests stored.
	if _, _, err := c.Delete(ctx, "/", true); err != nil && !IsNotFound(err) {
		t.Fatalf("clearing the keyspace: %v", err)
	}
	mustPut(t, c, "/delete-root/a", "1")
	mustPut(t, c, "/delete-root/b/c", "2")
	mustPut(t, c, "/delete-rooted", "3")

	rev, count, err := c.Delete(ctx, "/", true)
	if err != nil {
		t.Fatalf("Delete(/): %v", err)
	}
	if count != 3 {
		t.Errorf("deleted %d entries, want the whole keyspace (3 keys)", count)
	}
	if rev == 0 {
		t.Error("returned revision 0, want the revision the delete landed on")
	}
	entries, err := c.Read(ctx, "/", true)
	if err != nil || len(entries) != 0 {
		t.Errorf("root read after clear = %+v, %v; want an empty keyspace", summarize(entries), err)
	}
}
