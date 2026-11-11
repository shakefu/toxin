# shellcheck shell=bash
# script/.test/20-unit.sh — run the Go test suite (Docker needed: tests
# launch real etcd containers through testcontainers-go)
20-unit() {
    info "running go test (etcd containers via Docker)"
    go test ./... -count=1 "$@"
}
