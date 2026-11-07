// Package watchkill holds the watch test that must kill the etcd server to
// observe a stream disconnect. It lives in its own test binary because
// killing the server poisons it for every other test; this binary gets its
// own container from testutil.
package watchkill
