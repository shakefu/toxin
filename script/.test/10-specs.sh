# shellcheck shell=bash
# script/.test/10-specs.sh — validate and lint the yass spec set
10-specs() {
    if ! command -v yass >/dev/null 2>&1; then
        return "$(fail "yass is not installed")"
    fi
    info "running yass validate"
    yass validate
    info "running yass lint"
    yass lint
}
