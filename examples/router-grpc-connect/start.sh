#!/usr/bin/env bash
set -euo pipefail

example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${example_dir}/../.." && pwd)"
service_pid=""

cleanup() {
  if [[ -n "${service_pid}" ]] && kill -0 "${service_pid}" 2>/dev/null; then
    kill "${service_pid}"
    wait "${service_pid}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

cd "${example_dir}"

if ! command -v wgc >/dev/null 2>&1; then
  npm install -g wgc@latest
fi

if [[ ! -x router ]]; then
  wgc router download-binary -o .
fi

(
  cd "${repo_root}/demo/pkg/subgraphs/projects"
  env -u GOROOT go run ./cmd/service
) &
service_pid=$!

ready=false
for _ in {1..60}; do
  if (echo > /dev/tcp/127.0.0.1/4011) >/dev/null 2>&1; then
    ready=true
    break
  fi
  if ! kill -0 "${service_pid}" 2>/dev/null; then
    wait "${service_pid}"
  fi
  sleep 1
done

if [[ "${ready}" != true ]]; then
  echo "projects service did not become ready on port 4011" >&2
  exit 1
fi

wgc router compose -i graph.yaml -o config.json
./router
