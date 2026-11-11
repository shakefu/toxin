# shellcheck shell=bash
# script/.update/10-go.sh — tidy and refresh Go modules
10-go() {
    info "tidying Go modules"
    go mod tidy
    go mod download
}
