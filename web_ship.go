package main

func buildShipScript() string {
	return `#!/bin/bash
set -Eeuo pipefail
APP=/website/app
[ "$#" -gt 0 ] || { echo 'Usage: ship-web <commit message>' >&2; exit 2; }
cd "$APP"
git add -A
if ! git diff --cached --quiet; then
  git commit -m "$*"
else
  echo 'No new commit; shipping current HEAD.'
fi
git push origin HEAD:main
exec /usr/local/bin/deploy-web
`
}
