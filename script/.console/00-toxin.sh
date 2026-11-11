# shellcheck shell=bash
# script/.console/00-toxin.sh — run the toxin CLI against a local etcd
00-toxin() {
    info "running toxin against ${TOXIN_ETCD:-127.0.0.1:2379} (script/server starts one)"
    TOXIN_ETCD="${TOXIN_ETCD:-127.0.0.1:2379}" go run ./cmd/toxin "$@"
}
