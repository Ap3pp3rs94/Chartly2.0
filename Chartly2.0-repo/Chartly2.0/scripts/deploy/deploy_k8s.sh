#!/usr/bin/env bash
set -euo pipefail

# Chartly 2.0  deploy_k8s.sh
# Safe Helm-based deploy orchestrator.

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

say() { printf '[deploy_k8s] %s\n' "$*"; }
die() { printf '[deploy_k8s] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./deploy_k8s.sh --env dev|staging|prod [--yes]

Safety:
  - --env is required (no default)
  - prod requires:
      --yes
      CHARTLY_ALLOW_PROD_DEPLOY=1

Env:
  CHARTLY_ALLOW_PROD_DEPLOY=1

  # kube contexts (picked by --env)
  CHARTLY_KUBE_CONTEXT_DEV
  CHARTLY_KUBE_CONTEXT_STAGING
  CHARTLY_KUBE_CONTEXT_PROD

  # helm config (optional overrides)
  CHARTLY_HELM_CHART_PATH      default: <repo_root>/deploy/helm/chartly
  CHARTLY_HELM_RELEASE         default: chartly
  CHARTLY_DEPLOY_NAMESPACE     default: chartly

  # smoke (optional)
  CHARTLY_BASE_URL             if set and curl exists, will call /health and /ready

EOF
}

env_name=""
yes="0"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env) env_name="${2:-}"; shift 2 ;;
    --yes) yes="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown arg: $1 (use --help)" ;;
  esac
done

[[ -n "$env_name" ]] || { usage; die "Missing required --env"; }

case "$env_name" in
  dev|staging|prod) ;;
  *) die "--env must be one of: dev|staging|prod" ;;
esac

if [[ "$env_name" == "prod" ]]; then
  [[ "$yes" == "1" ]] || die "Refusing prod deploy without --yes"
  [[ "${CHARTLY_ALLOW_PROD_DEPLOY:-0}" == "1" ]] || die "Refusing prod deploy. Set CHARTLY_ALLOW_PROD_DEPLOY=1"
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

chart_path="${CHARTLY_HELM_CHART_PATH:-$repo_root/deploy/helm/chartly}"
release="${CHARTLY_HELM_RELEASE:-chartly}"
namespace="${CHARTLY_DEPLOY_NAMESPACE:-chartly}"

values_file="$repo_root/deploy/helm/values.$env_name.yaml"
values_args=()
if [[ -f "$values_file" ]]; then
  say "Using values: $values_file"
  values_args+=( -f "$values_file" )
else
  say "No env values file found at: $values_file (ok)"
fi

[[ -d "$chart_path" ]] || die "Helm chart path not found: $chart_path"

say "Helm chart: $chart_path"
say "Release:    $release"
say "Namespace:  $namespace"

# Ensure namespace exists
kubectl get namespace "$namespace" >/dev/null 2>&1 || kubectl create namespace "$namespace" >/dev/null

say "Deploying (helm upgrade --install)..."
helm upgrade --install "$release" "$chart_path" \
  --namespace "$namespace" \
  "${values_args[@]}"

say "Rollout status (best-effort)..."
# Watch all deployments in namespace (if any)
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

# Optional smoke test
base_url="${CHARTLY_BASE_URL:-}"
if [[ -n "$base_url" && -x "$(command -v curl)" ]]; then
  base_url="${base_url%/}"
  for ep in /health /ready; do
    url="$base_url$ep"
    say "smoke GET $url"
    curl -fsS --max-time 10 "$url" | sed -e 's/^/[deploy_k8s]   /'
  done
  say "smoke: OK"
else
  say "Smoke skipped (set CHARTLY_BASE_URL and ensure curl is installed)"
fi

say "done"
