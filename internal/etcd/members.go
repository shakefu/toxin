package etcd

import (
	"fmt"
	"strings"
)

// member is one resolved member of the member list.
type member struct {
	// endpoint is the member URL the client dials: scheme://host[:port].
	endpoint string
	// username and password carry the member's user:password credentials,
	// when it had any.
	username string
	password string
	hasCreds bool
}

// parseMember resolves one member of the member list. A member is host,
// host:port, scheme://host:port, or scheme://user:password@host:port; a
// member without a scheme gets defaultScheme. A member matching none of
// the forms, including credentials with an empty username or password, is
// rejected with an error naming it.
func parseMember(raw string, defaultScheme string) (member, error) {
	m := member{}
	rest := raw
	scheme := ""
	if i := strings.Index(rest, "://"); i >= 0 {
		scheme = rest[:i]
		if !validScheme(scheme) {
			return m, invalidMember(raw)
		}
		rest = rest[i+3:]
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		// Credentials ride on a scheme:// form only.
		if scheme == "" {
			return m, invalidMember(raw)
		}
		// Both the username and the password must be non-empty.
		user, pass, ok := strings.Cut(rest[:at], ":")
		if !ok || user == "" || pass == "" {
			return m, invalidMember(raw)
		}
		m.username, m.password, m.hasCreds = user, pass, true
		rest = rest[at+1:]
	}

	host := rest
	port := ""
	if i := strings.Index(host, ":"); i >= 0 {
		host, port = host[:i], host[i+1:]
		if port == "" || !isDigits(port) {
			return m, invalidMember(raw)
		}
	}
	if host == "" || strings.ContainsAny(host, "/@ ") {
		return m, invalidMember(raw)
	}

	if scheme == "" {
		scheme = defaultScheme
	}
	m.endpoint = scheme + "://" + host
	if port != "" {
		m.endpoint += ":" + port
	}
	return m, nil
}

// invalidMember names a rejected member with any password redacted.
func invalidMember(raw string) error {
	return fmt.Errorf("invalid etcd member %q", redactMember(raw))
}

// redactMember replaces the password in a member's user:password@ part
// with "***", so an error naming the member never leaks the secret. The
// credentials are located the way parseMember locates them: after any
// scheme://, up to the last '@'.
func redactMember(raw string) string {
	head, rest := "", raw
	if i := strings.Index(rest, "://"); i >= 0 {
		head, rest = rest[:i+3], rest[i+3:]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return raw
	}
	user, _, hasPass := strings.Cut(rest[:at], ":")
	if !hasPass {
		return raw
	}
	return head + user + ":***" + rest[at:]
}

// validScheme matches a URL scheme: a letter followed by letters, digits,
// '+' '-' or '.'.
func validScheme(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
