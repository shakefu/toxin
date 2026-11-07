package cli

import "strings"

// ANSI styles for the Go syntax colorizer: comments dim, strings green,
// numbers cyan and the literal keywords magenta.
const (
	ansiReset   = "\x1b[0m"
	ansiComment = "\x1b[90m"
	ansiString  = "\x1b[32m"
	ansiNumber  = "\x1b[36m"
	ansiKeyword = "\x1b[35m"
)

// goKeywords are the identifier tokens of the scanner's output colored as
// keywords.
var goKeywords = map[string]bool{
	"true":  true,
	"false": true,
	"nil":   true,
}

// colorizeGo colorizes the scanner's output as Go syntax. Output is
// colorized by default, with -c toggling the setting off. A "#" annotation
// line reads as a comment; string literals, numbers and the literal
// keywords are styled on the value lines.
func colorizeGo(src string) string {
	var b strings.Builder
	for i, line := range strings.Split(src, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			b.WriteString(ansiComment + line + ansiReset)
			continue
		}
		colorizeLine(&b, line)
	}
	return b.String()
}

// colorizeLine writes one line colorized as Go syntax.
func colorizeLine(b *strings.Builder, line string) {
	rs := []rune(line)
	for i := 0; i < len(rs); {
		switch c := rs[i]; {
		case c == '"' || c == '\'':
			// A string or rune literal, escapes included.
			j := i + 1
			for j < len(rs) && rs[j] != c {
				if rs[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(rs) {
				j++
			}
			b.WriteString(ansiString + string(rs[i:j]) + ansiReset)
			i = j
		case c == '`':
			j := i + 1
			for j < len(rs) && rs[j] != '`' {
				j++
			}
			if j < len(rs) {
				j++
			}
			b.WriteString(ansiString + string(rs[i:j]) + ansiReset)
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(rs) && (isWordRune(rs[j]) || rs[j] == '.') {
				j++
			}
			b.WriteString(ansiNumber + string(rs[i:j]) + ansiReset)
			i = j
		case isWordRune(c):
			j := i
			for j < len(rs) && isWordRune(rs[j]) {
				j++
			}
			word := string(rs[i:j])
			if goKeywords[word] {
				b.WriteString(ansiKeyword + word + ansiReset)
			} else {
				b.WriteString(word)
			}
			i = j
		default:
			b.WriteRune(c)
			i++
		}
	}
}

// isWordRune reports whether r can appear in an identifier or number.
func isWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
