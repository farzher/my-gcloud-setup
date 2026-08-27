package main

func buildStatusScript() string {
	return `#!/bin/bash
set -u
URL=http://127.0.0.1:3000
FAILED=0

service_status() {
  if systemctl is-active --quiet "$1"; then
    printf '%s: active\n' "$1"
  else
    printf '%s: inactive\n' "$1"
    FAILED=1
  fi
}

printf '== services ==\n'
service_status web.service
service_status nginx.service
service_status postgresql.service

printf '\n== health ==\n'
if HEALTH="$(curl -fsS --max-time 2 "$URL/healthz" 2>/dev/null)"; then
  printf 'healthz: %s\n' "$(printf '%s' "$HEALTH" | tr '\n' ' ' | cut -c1-300)"
else
  printf 'healthz: failed\n'
  FAILED=1
fi

if sudo -n -u postgres psql -d web -tAc 'SELECT 1' 2>/dev/null | grep -qx 1; then
  printf 'database: ok\n'
else
  printf 'database: failed\n'
  FAILED=1
fi

printf '\n== resources ==\n'
df -hP / | awk 'NR==2 {printf "disk: %s used / %s total (%s), %s free\\n",$3,$2,$5,$4}'
free -h | awk '/^Mem:/ {printf "memory: %s used / %s total, %s available\\n",$3,$2,$7} /^Swap:/ {printf "swap: %s used / %s total\\n",$3,$2}'

if [ "$FAILED" -ne 0 ] || [ "${1:-}" = '--logs' ]; then
  printf '\n== recent web logs ==\n'
  journalctl -u web.service -n 25 --no-pager --output=short-iso 2>/dev/null || true
fi

exit "$FAILED"
`
}
