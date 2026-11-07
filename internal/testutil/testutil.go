// Package testutil provides the shared etcd server toxin's tests run
// against. The server is a real etcd v3 launched via testcontainers-go's
// etcd module — tests never run against a mock.
//
// One container serves a whole test binary: the first call to Etcd starts
// it (guarded by a sync.Once) and every later call returns the same
// endpoint. Terminate it from TestMain via Cleanup once m.Run has
// returned.
package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/etcd"
)

// The etcd image and start budget. The first run must pull the image,
// which takes a minute or more.
const (
	etcdImage     = "gcr.io/etcd-development/etcd:v3.5.14"
	containerWait = 5 * time.Minute
)

var (
	startOnce sync.Once
	endpoint  string
	terminate func()
	startErr  error
)

// Etcd starts the shared etcd container for this test binary and returns
// its client endpoint, in the "http://127.0.0.1:<port>" form the etcd
// client layer accepts. The container is started once per binary; tests
// share it, so never stop the server from a test — call Cleanup from
// TestMain instead. If the container cannot start (Docker down, image
// pull failure) Etcd fails the test with a clear message rather than
// skipping.
func Etcd(t *testing.T) string {
	t.Helper()
	startOnce.Do(start)
	if startErr != nil {
		t.Fatalf("testutil: could not start the etcd container %s (is Docker running?): %v", etcdImage, startErr)
	}
	return endpoint
}

// Cleanup terminates the etcd container Etcd started, if any. Call it
// from TestMain after m.Run.
func Cleanup() {
	if terminate != nil {
		terminate()
	}
}

func start() {
	ctx, cancel := context.WithTimeout(context.Background(), containerWait)
	defer cancel()

	container, err := etcd.Run(ctx, etcdImage)
	if err != nil {
		startErr = err
		return
	}
	ep, err := container.ClientEndpoint(ctx)
	if err != nil {
		startErr = err
		_ = container.Terminate(context.Background())
		return
	}
	endpoint = ep
	terminate = func() {
		tctx, tcancel := context.WithTimeout(context.Background(), time.Minute)
		defer tcancel()
		_ = container.Terminate(tctx)
	}
}
