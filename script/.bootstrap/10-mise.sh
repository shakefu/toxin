# script/.bootstrap/10-mise.sh — mise integration for the bootstrap phase.
# Requires mise to already be installed; never installs it. Installs the
# repo's declared tools and puts mise's shims on PATH for the rest of the
# phase. Drop-in contract: scripts-to-rule-them-all README (one function
# named after the stem, no exit, stdlib for logging and outcomes;
# bootstrap is hard severity by default, so a failure stops the phase).
10-mise() {
  if ! command -v mise >/dev/null 2>&1; then
    warn "mise is not installed — install it from https://mise.jdx.dev/getting-started.html and re-run script/bootstrap"
    return $(fail "mise not found on PATH")
  fi

  if [ -f "$REPO_ROOT/mise.lock" ]; then
    info "installing tools (mise install --locked)"
    mise install --locked || return $(fail "mise install --locked failed")
  else
    info "installing tools (mise install)"
    mise install || return $(fail "mise install failed")
  fi

  # Make mise's shims resolvable for later drop-ins in this phase. Resolve
  # the directory from mise itself so a custom data dir (MISE_DATA_DIR)
  # works. Shims are just a directory on PATH — the non-interactive
  # mechanism from https://mise.jdx.dev/dev-tools/shims.html — and exports
  # are the sharing channel between drop-ins.
  local shims
  shims="$(mise data-dir 2>/dev/null || echo "$HOME/.local/share/mise")/shims"
  case ":$PATH:" in
    *":$shims:"*) ;;
    *) export PATH="$shims:$PATH" ;;
  esac
}
