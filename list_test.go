package toxin

import (
	"sort"
	"testing"
)

// buildListTree stores a small namespace tree through the raw client:
// two leaves directly under the prefix and one nested directory.
func buildListTree(t *testing.T, prefix string) {
	t.Helper()
	raw := newRawClient(t)
	rawPut(t, raw, "/"+prefix+"alpha", "1")
	rawPut(t, raw, "/"+prefix+"beta", "2")
	rawPut(t, raw, "/"+prefix+"sub/deep", "3")
	rawPut(t, raw, "/"+prefix+"sub/other", "4")
}

// 20-api@Config.List::RETURN: dirOnly defaults to true, returning only
// directory children without a trailing slash.
func TestListDirOnlyDefaultReturnsDirectoriesWithoutSlash(t *testing.T) {
	buildListTree(t, "list-ns/")
	c := newTestConfig(t, WithPrefix("list-ns/"))

	got, err := c.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0] != "sub" {
		t.Errorf("List() = %v, want just the directory [sub]", got)
	}
}

// 20-api@Config.List::RETURN: with dirOnly unset, leaf keys come back
// as-is and directory keys with a trailing slash.
func TestListWithoutDirOnlyReturnsLeavesAndSlashedDirectories(t *testing.T) {
	buildListTree(t, "list-all/")
	c := newTestConfig(t, WithPrefix("list-all/"))

	got, err := c.List("", DirOnly(false))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"alpha", "beta", "sub/"}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("List(dirOnly=false) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List(dirOnly=false) = %v, want %v", got, want)
			break
		}
	}
}

// 20-api@Config.List::RETURN: the children are relative to the combined
// prefix-and-key address.
func TestListKeyBelowPrefixAddressesCombined(t *testing.T) {
	buildListTree(t, "list-sub/")
	c := newTestConfig(t, WithPrefix("list-sub/"))

	got, err := c.List("sub", DirOnly(false))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "deep" || got[1] != "other" {
		t.Errorf("List(sub) = %v, want [deep other]", got)
	}
}

// 20-api@Config.List::RETURN: nothing under the namespace returns an
// empty list.
func TestListNothingUnderNamespaceReturnsEmptyList(t *testing.T) {
	c := newTestConfig(t, WithPrefix("list-nothing/"))

	got, err := c.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List of an empty namespace = %v, want an empty list", got)
	}
}
