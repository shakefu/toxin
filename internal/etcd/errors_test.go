package etcd

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// etcd.Error RETURN: carry the failure's message unchanged with its detail
// string as the cause.
func TestWrapErrorCarriesMessageUnchangedWithDetailAsCause(t *testing.T) {
	raw := status.Error(codes.Unavailable, "etcdserver: down")
	err := wrapError(raw)
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("wrapError returned %T, want *Error", err)
	}
	if e.Msg != raw.Error() {
		t.Errorf("message = %q, want the failure's message unchanged %q", e.Msg, raw.Error())
	}
	if e.Cause != "etcdserver: down" {
		t.Errorf("cause = %q, want the failure's detail string", e.Cause)
	}
	if e.Code != int(codes.Unavailable) {
		t.Errorf("code = %d, want %d", e.Code, codes.Unavailable)
	}
}

// etcd.Error INPUT: take any failure — a plain error with no gRPC detail
// keeps its message and carries no cause.
func TestWrapErrorOfPlainFailureCarriesMessageWithNoDetail(t *testing.T) {
	raw := errors.New("boom")
	e, ok := wrapError(raw).(*Error)
	if !ok {
		t.Fatalf("wrapError returned %T, want *Error", raw)
	}
	if e.Msg != "boom" {
		t.Errorf("message = %q, want %q", e.Msg, "boom")
	}
	if e.Cause != "" {
		t.Errorf("cause = %q, want empty", e.Cause)
	}
	if e.Code != 0 {
		t.Errorf("code = %d, want 0", e.Code)
	}
}

// Errors this layer already shaped pass through untouched.
func TestWrapErrorPassesShapedErrorsThrough(t *testing.T) {
	e := notFound("/k")
	if got := wrapError(e); got != error(e) {
		t.Errorf("wrapError of a shaped error returned %#v, want it untouched", got)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(notFound("/k")) {
		t.Error("IsNotFound(not-found) = false, want true")
	}
	if IsNotFound(status.Error(codes.Unavailable, "down")) {
		t.Error("IsNotFound(unavailable) = true, want false")
	}
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) = true, want false")
	}
}
