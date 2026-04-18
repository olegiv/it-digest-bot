# shellcheck shell=bash
#
# Common helpers sourced by the deploy/* scripts. Auto-loads host-specific
# settings from deploy/deploy.env if that file exists (gitignored; copy
# deploy.env.example to deploy.env and edit). Idempotent — safe to source
# multiple times.

_lib_sh_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -z ${_deploy_env_loaded:-} && -f "$_lib_sh_dir/deploy.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$_lib_sh_dir/deploy.env"
  set +a
  _deploy_env_loaded=1
fi

# resolve_host [arg]
#
# Prints the SSH target on stdout, prefixing with "root@" if the input
# has no user part. Precedence:
#   1. explicit argument (passed as $1)
#   2. HOST env var
#   3. DEPLOY_HOST env var (from deploy.env or shell)
#
# Returns nonzero with a hint-printing error if no source produces a host.
resolve_host() {
  local host="${1:-${HOST:-${DEPLOY_HOST:-}}}"
  if [[ -z $host ]]; then
    cat >&2 <<EOF
error: no target host set. Options:
  - copy deploy/deploy.env.example → deploy/deploy.env and set DEPLOY_HOST
  - pass via env: DEPLOY_HOST=server.example.com $0
  - pass via Makefile: make <target> HOST=server.example.com
EOF
    return 1
  fi
  [[ $host == *@* ]] || host="root@$host"
  printf '%s\n' "$host"
}
