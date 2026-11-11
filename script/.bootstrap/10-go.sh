# shellcheck shell=bash
# script/.bootstrap/10-go.sh — download Go modules
10-go() {
    info "downloading Go modules"
    go mod download
}
