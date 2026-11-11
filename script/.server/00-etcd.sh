# shellcheck shell=bash
# script/.server/00-etcd.sh — run a local etcd on 127.0.0.1:2379
00-etcd() {
    info "starting etcd on 127.0.0.1:2379 (Ctrl-C to stop)"
    docker run --rm -p 127.0.0.1:2379:2379 \
        gcr.io/etcd-development/etcd:v3.5.14 \
        etcd \
        --advertise-client-urls http://127.0.0.1:2379 \
        --listen-client-urls http://0.0.0.0:2379
}
