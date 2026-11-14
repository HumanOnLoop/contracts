#!/usr/bin/env bash
# Usage: with-port-forward.sh <service> <local-port> <remote-port> -- <command...>
#
# Runs <command> with <service> in the hotl namespace of the k3d-hotl cluster reachable on localhost:<local-port>.
# Reuses whatever already listens on that port (for example `make port-forward` in the platform repo); otherwise
# starts a port-forward for the duration of the command. Always names the k3d-hotl context explicitly.
set -euo pipefail

CONTEXT=k3d-hotl
NAMESPACE=hotl

[[ $# -ge 5 && $4 == -- ]] || { echo "usage: $0 <service> <local-port> <remote-port> -- <command...>" >&2; exit 2; }
svc=$1 lport=$2 rport=$3
shift 4

listening() { (exec 3<>"/dev/tcp/127.0.0.1/$lport") 2>/dev/null; }

if ! listening; then
	log=$(mktemp)
	kubectl --context "$CONTEXT" -n "$NAMESPACE" port-forward "$svc" "$lport:$rport" >"$log" 2>&1 &
	pf=$!
	trap 'kill $pf 2>/dev/null || true; rm -f "$log"' EXIT
	for _ in $(seq 1 50); do
		listening && break
		kill -0 "$pf" 2>/dev/null || { cat "$log" >&2; echo "port-forward to $svc failed; is the k3d-hotl stack up (make up in platform)?" >&2; exit 1; }
		sleep 0.2
	done
	listening || { cat "$log" >&2; echo "port-forward to $svc did not come up on localhost:$lport" >&2; exit 1; }
fi

"$@"
