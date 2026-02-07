#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "ERROR: This script must be run on Linux." >&2
  exit 1
fi

DOMAIN=""
EXPECTED_IP=""
APPLY="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain)
      DOMAIN="$2"; shift 2;;
    --expected-ip)
      EXPECTED_IP="$2"; shift 2;;
    --apply)
      APPLY="true"; shift 1;;
    -h|--help)
      echo "Usage: $0 --domain app.chartly.lol --expected-ip 1.2.3.4 [--apply]"; exit 0;;
    *)
      echo "Unknown arg: $1" >&2; exit 2;;
  esac
done

if [[ -z "$DOMAIN" || -z "$EXPECTED_IP" ]]; then
  echo "ERROR: --domain and --expected-ip are required." >&2
  exit 2
fi

need_cmd(){
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: missing required command: $1" >&2
    exit 1
  }
}

need_cmd curl
need_cmd dig
need_cmd systemctl
need_cmd ss
need_cmd docker

bold(){ printf "\n== %s ==\n" "$1"; }

bold "Server Public IP"
SERVER_IP="$(curl -4s https://icanhazip.com | tr -d '[:space:]')"
echo "Server IPv4: ${SERVER_IP:-unknown}"

bold "DNS A Records"
DNS_A="$(dig +short "$DOMAIN" | tr -d '[:space:]' | tr '\n' ' ')"
echo "DNS A: ${DNS_A:-none}"
if [[ -n "$DNS_A" ]]; then
  for ip in $DNS_A; do
    if [[ "$ip" != "$EXPECTED_IP" ]]; then
      echo "WARNING: DNS points elsewhere: $ip (expected $EXPECTED_IP)"
      echo "Fix at DNS provider: set A $DOMAIN -> $EXPECTED_IP"
    fi
  done
fi

bold "Ports 80/443"
ss -lntp | grep -E ':(80|443)\b' || echo "No listeners on 80/443"
if command -v ufw >/dev/null 2>&1; then
  echo ""
  ufw status || true
fi

bold "Gateway Upstream Detection"
if ! docker compose -f docker-compose.control.yml ps >/dev/null 2>&1; then
  echo "docker compose control plane not available in current directory." >&2
  exit 1
fi

GW_CID="$(docker compose -f docker-compose.control.yml ps -q gateway || true)"
if [[ -z "$GW_CID" ]]; then
  echo "Gateway container not running."
  if [[ "$APPLY" == "true" ]]; then
    echo "Starting control plane..."
    docker compose -f docker-compose.control.yml up -d --build
    GW_CID="$(docker compose -f docker-compose.control.yml ps -q gateway || true)"
  else
    echo "Dry-run: docker compose -f docker-compose.control.yml up -d --build"
  fi
fi

if [[ -z "$GW_CID" ]]; then
  echo "ERROR: Gateway container still not running." >&2
  exit 1
fi

PORT_LINE="$(docker inspect "$GW_CID" --format '{{range $p, $conf := .NetworkSettings.Ports}}{{if $conf}}{{(index $conf 0).HostPort}}{{end}}{{end}}' | tr -d ' ' | head -n1)"
if [[ -z "$PORT_LINE" ]]; then
  echo "ERROR: Could not detect host port for gateway." >&2
  exit 1
fi

UPSTREAM_PORT="$PORT_LINE"
echo "Detected gateway host port: $UPSTREAM_PORT"

bold "Upstream HTTP Check"
if ! curl -fsI "http://127.0.0.1:${UPSTREAM_PORT}/" >/dev/null; then
  echo "ERROR: Upstream not responding on 127.0.0.1:${UPSTREAM_PORT}" >&2
  exit 1
fi
echo "Upstream OK"

bold "Caddy Config"
CADDYFILE_CONTENT="$DOMAIN {\n  reverse_proxy 127.0.0.1:${UPSTREAM_PORT}\n}"
if [[ "$APPLY" == "true" ]]; then
  echo "Writing /etc/caddy/Caddyfile"
  echo "$CADDYFILE_CONTENT" | sudo tee /etc/caddy/Caddyfile >/dev/null
  echo "Restarting caddy"
  sudo systemctl restart caddy
else
  echo "Dry-run. To apply:"
  echo "echo \"$CADDYFILE_CONTENT\" | sudo tee /etc/caddy/Caddyfile"
  echo "sudo systemctl restart caddy"
fi

bold "Caddy Status"
systemctl status caddy --no-pager || true

bold "Caddy Logs"
journalctl -u caddy -n 60 --no-pager || true

echo ""
echo "Next: once DNS points to $EXPECTED_IP, HTTPS issuance will succeed."
