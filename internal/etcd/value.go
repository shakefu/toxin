package etcd

import (
	"encoding/json"
	"strings"
)

// EncodeValue encodes v in the form values cross the etcd boundary in: a
// value that is a string is stored verbatim; any other value is stored as
// its JSON encoding.
func EncodeValue(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeValue decodes a stored value: it is parsed as JSON, and a value
// that fails to parse is delivered as the raw string. Numbers decode as
// json.Number so numeric literals survive a write-read cycle intact.
func DecodeValue(s string) any {
	if !json.Valid([]byte(s)) {
		return s
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return s
	}
	return v
}
