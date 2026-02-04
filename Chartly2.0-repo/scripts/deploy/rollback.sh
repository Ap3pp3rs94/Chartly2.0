#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  rollback.sh
# Safe Helm rollback orchestrator.

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

say() { printf '[rollback] %s\n' "$*"; }
die() { printf '[rollback] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./rollback.sh --env dev|staging|prod --yes [--revision <n>]

Safety:
  - --env is required (no default)
  - --yes is required for all environments
  - prod requires CHARTLY_ALLOW_PROD_DEPLOY=1

Env:
  CHARTLY_ALLOW_PROD_DEPLOY=1

  CHARTLY_KUBE_CONTEXT_DEV
  CHARTLY_KUBE_CONTEXT_STAGING
  CHARTLY_KUBE_CONTEXT_PROD

  CHARTLY_HELM_RELEASE       default: chartly
  CHARTLY_DEPLOY_NAMESPACE   default: chartly

EOF
}

env_name=""
yes="0"
revision=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env) env_name="${2:-}"; shift 2 ;;
    --yes) yes="1"; shift ;;
    --revision) revision="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

[[ -n "$env_name" ]] || { usage; die "Missing required --env"; }
[[ "$yes" == "1" ]] || { usage; die "Refusing to run without --yes"; }

case "$env_name" in
  dev|staging|prod) ;;
  *) die "--env must be one of: dev|staging|prod" ;;
esac

if [[ "$env_name" == "prod" ]]; then
  [[ "${CHARTLY_ALLOW_PROD_DEPLOY:-0}" == "1" ]] || die "Refusing prod rollback. Set CHARTLY_ALLOW_PROD_DEPLOY=1"
fi

command -v kubectl >/dev/null 2>&1 || die "kubectl not found"
command -v helm   >/dev/null 2>&1 || die "helm not found"

say "repo_root: $repo_root"
say "env:      $env_name"

# Pick kube context from env vars
ctx_var="CHARTLY_KUBE_CONTEXT_${env_name^^}"
kube_ctx="${!ctx_var:-}"

if [[ -n "$kube_ctx" ]]; then
  say "Using kube context ($ctx_var): $kube_ctx"
  kubectl config use-context "$kube_ctx" >/dev/null
else
  say "Kube context not set ($ctx_var). Using current context."
fi

release="${CHARTLY_HELM_RELEASE:-chartly}"
namespace="${CHARTLY_DEPLOY_NAMESPACE:-chartly}"

say "Release:   $release"
say "Namespace: $namespace"

say "Helm history:"
helm history "$release" --namespace "$namespace" || true

say "Rolling back..."
if [[ -n "$revision" ]]; then
  if ! echo "$revision" | grep -Eq '^[0-9]+$'; then
    die "--revision must be an integer"
  fi
  helm rollback "$release" "$revision" --namespace "$namespace"
else
  # Previous revision
  helm rollback "$release" --namespace "$namespace"
fi

say "Rollout status (best-effort)..."
deploys="$(kubectl -n "$namespace" get deploy -o name 2>/dev/null || true)"
if [[ -n "$deploys" ]]; then
  while IFS= read -r d; do
    [[ -n "$d" ]] || continue
    say "rollout: $d"
    kubectl -n "$namespace" rollout status "$d" --timeout=120s || true
  done <<< "$deploys"
else
  say "No deployments found in namespace (ok)"
fi

say "kubectl get pods:"
kubectl -n "$namespace" get pods -o wide || true

say "done"
