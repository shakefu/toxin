# shellcheck shell=bash
# script/.setup/10-hooks.sh — install prek hooks and the yass spec tool
10-hooks() {
    info "installing prek hooks"
    prek install
    if command -v yass >/dev/null 2>&1; then
        info "yass $(yass --version | awk '{print $2}') found"
    else
        warn "yass not found; script/test will skip nothing — install it with:"
        warn "  curl -fsSL https://raw.githubusercontent.com/shakefu/yass/main/install.sh | sh"
    fi
}
