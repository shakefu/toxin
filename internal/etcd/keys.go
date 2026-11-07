package etcd

import "strings"

// NormalizeKey returns key in the slash-directory form every key crossing
// the etcd boundary uses: every leading '/' is stripped and exactly one is
// prepended. The empty key names the root '/'.
func NormalizeKey(key string) string {
	stripped := strings.TrimLeft(key, "/")
	if stripped == "" {
		return "/"
	}
	return "/" + stripped
}

// TrimKey returns the address form reads and deletes use: the key
// normalized, with trailing '/' characters additionally trimmed, keeping
// the root '/'.
func TrimKey(key string) string {
	trimmed := strings.TrimRight(NormalizeKey(key), "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

// childPrefix returns the prefix holding the children of a trimmed
// address: strictly below "addr/", with the root's children below "/".
func childPrefix(addr string) string {
	if addr == "/" {
		return "/"
	}
	return addr + "/"
}

// inRange reports whether key is the trimmed address itself or lives
// strictly below "addr/". A key that merely shares addr as a string
// prefix ("/configuration" for addr "/config") is out of range.
func inRange(addr, key string) bool {
	if addr == "/" || key == addr {
		return true
	}
	return strings.HasPrefix(key, addr+"/")
}
