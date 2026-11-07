package etcd

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// notFoundCode is the error code a not-found error carries, in the etcd v2
// style pyconfig and jetconfig used.
const notFoundCode = 100

// Error is the error shape this layer produces for any failure raised by
// the cluster or the transport, and for not-found reads and deletes.
type Error struct {
	// Code is 100 for a not-found error; any other failure carries the
	// gRPC status code, or zero when there is none.
	Code int
	// Msg is the failure's message, unchanged; a not-found error's
	// message is "Key not found".
	Msg string
	// Cause is the detail string: the addressed key for a not-found
	// error, otherwise the failure's gRPC detail.
	Cause string
}

func (e *Error) Error() string { return e.Msg }

// notFound builds the not-found error for the addressed key.
func notFound(key string) *Error {
	return &Error{Code: notFoundCode, Msg: "Key not found", Cause: key}
}

// IsNotFound reports whether err is a not-found error produced by this
// layer.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == notFoundCode
}

// wrapError shapes any cluster or transport failure as an *Error, carrying
// the failure's message unchanged and its detail string as the cause.
// Errors this layer already shaped pass through untouched.
func wrapError(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	code := 0
	cause := ""
	if s, ok := status.FromError(err); ok {
		code = int(s.Code())
		cause = s.Message()
	}
	if code == int(codes.OK) {
		code = 0
	}
	return &Error{Code: code, Msg: err.Error(), Cause: cause}
}
