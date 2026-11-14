# shellcheck shell=bash
# script/.bootstrap/20-go.sh — download Go modules (20- so mise, at 10-,
# has installed the toolchain first; upstream 00-mise is pending
# common-repo/mise#2)
20-go() {
    info "downloading Go modules"
    go mod download
}
