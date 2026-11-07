package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// renderValue renders a runtime value in Go literal syntax — the scanner's
// value rendering for live configuration values. Decoded JSON values cover
// strings, numbers, booleans, nulls, arrays and objects; numbers decode as
// json.Number, whose literal is its own text.
func renderValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "nil"
	case bool:
		return strconv.FormatBool(t)
	case string:
		return strconv.Quote(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, renderValue(e))
		}
		return "[]any{" + strings.Join(parts, ", ") + "}"
	case map[string]any:
		parts := make([]string, 0, len(t))
		for _, k := range sortedKeys(t) {
			parts = append(parts, strconv.Quote(k)+": "+renderValue(t[k]))
		}
		return "map[string]any{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%#v", t)
	}
}

// truthy reports whether v is truthy in the pyconfig sense: nil, false,
// zero, the empty string and empty arrays and objects are falsy. A found
// key's falsy current value falls back to the call's default.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case json.Number:
		f, err := t.Float64()
		return err == nil && f != 0
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}
