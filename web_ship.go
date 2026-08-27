package main

func buildShipScript() string {
	return `#!/bin/bash
set -Eeuo pipefail
APP=/website/app
[ "$#" -gt 0 ] || { echo 'Usage: ship-web <commit message>' >&2; exit 2; }
cd "$APP"
git add -A
if git diff --cached --quiet; then
  echo 'No changes to ship.'
  exit 0
fi
git commit -m "$*"
git push
exec /usr/local/bin/deploy-web
`
}
