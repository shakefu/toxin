# shellcheck shell=bash
# script/.test/30-lint.sh — gofmt and go vet
30-lint() {
    info "checking gofmt"
    local unformatted
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
        return "$(fail "these files need gofmt: ${unformatted//$'\n'/ }")"
    fi
    info "running go vet"
    go vet ./...
}
