# shellcheck shell=bash
# script/.bootstrap/15-docker.sh — the tests launch real etcd containers,
# so a reachable docker daemon is part of the toolchain
15-docker() {
    if docker info >/dev/null 2>&1; then
        info "docker daemon reachable"
        return 0
    fi
    if command -v colima >/dev/null 2>&1; then
        info "starting colima (docker daemon)"
        colima start
        return 0
    fi
    return "$(fail "no docker daemon: start Docker Desktop or run 'colima start'")"
}
