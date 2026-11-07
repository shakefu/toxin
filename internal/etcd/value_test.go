package etcd

import (
	"encoding/json"
	"reflect"
	"testing"
)

// ValueEncoding: a written value that is a string is stored verbatim; any
// other value is stored as its JSON encoding.
func TestEncodeValueStoresStringsVerbatimOthersAsJSON(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{"", ""},
		{"42", "42"},
		{json.Number("3.14"), "3.14"},
		{42, "42"},
		{int64(-7), "-7"},
		{3.5, "3.5"},
		{true, "true"},
		{false, "false"},
		{nil, "null"},
		{[]any{json.Number("1"), "two", true}, `[1,"two",true]`},
		{map[string]any{"a": json.Number("1"), "b": nil}, `{"a":1,"b":null}`},
	}
	for _, c := range cases {
		got, err := EncodeValue(c.in)
		if err != nil {
			t.Errorf("EncodeValue(%#v): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("EncodeValue(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ValueEncoding: a read value is parsed as JSON; a value that fails to
// parse is delivered as the raw string.
func TestDecodeValueParsesJSONFallingBackToRawString(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"42", json.Number("42")},
		{"-3.14", json.Number("-3.14")},
		{"1e3", json.Number("1e3")},
		{"true", true},
		{"false", false},
		{"null", nil},
		{`{"a":1}`, map[string]any{"a": json.Number("1")}},
		{`[1,2]`, []any{json.Number("1"), json.Number("2")}},
		// Values that fail to parse come back as the raw string.
		{"hello world", "hello world"},
		{"", ""},
		{"1 2", "1 2"},
		{`{"a":1}x`, `{"a":1}x`},
		{"bare word", "bare word"},
	}
	for _, c := range cases {
		if got := DecodeValue(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("DecodeValue(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// ValueEncoding: numbers, booleans, null, arrays and objects survive a
// write-read cycle intact; a plain string that is not valid JSON comes
// back as itself.
func TestValuesSurviveWriteReadCycleIntact(t *testing.T) {
	values := []any{
		json.Number("42"),
		json.Number("3.14"),
		json.Number("1e3"),
		json.Number("-0.5"),
		true,
		false,
		nil,
		[]any{json.Number("1"), "two", false, nil},
		map[string]any{"a": json.Number("1"), "b": []any{json.Number("2")}},
	}
	for _, v := range values {
		stored, err := EncodeValue(v)
		if err != nil {
			t.Fatalf("EncodeValue(%#v): %v", v, err)
		}
		if got := DecodeValue(stored); !reflect.DeepEqual(got, v) {
			t.Errorf("write-read cycle of %#v gave %#v (stored %q)", v, got, stored)
		}
	}
	// A plain string that is not valid JSON comes back as itself, on both
	// sides of the boundary.
	plain := "just a string"
	stored, err := EncodeValue(plain)
	if err != nil {
		t.Fatalf("EncodeValue(string): %v", err)
	}
	if stored != plain {
		t.Errorf("plain string stored as %q, want it verbatim", stored)
	}
	if got := DecodeValue(stored); got != plain {
		t.Errorf("plain string read back as %#v, want %q", got, plain)
	}
}
