#!/usr/bin/env bash
# Check that a built repository really installs, by pointing apt at it in a
# Debian container:
#
#   test-apt-repo.sh <site-dir> [image]
#
# The site is served over HTTP the way Pages serves it, the keyring package is
# installed the way the instructions say, and then tsuzuki is installed and run.
set -euo pipefail

site=$(realpath "${1:?usage: test-apt-repo.sh <site-dir> [image]}")
image=${2:-debian:stable-slim}
port=${PORT:-8731}

python3 -m http.server "$port" --bind 127.0.0.1 --directory "$site" >/dev/null 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true' EXIT
sleep 1

# The script is fed in whole and the port passed as an argument, so nothing is
# expanded by the shell out here.
docker run --rm -i --network host -v "$site:/site:ro" -e DEBIAN_FRONTEND=noninteractive \
	"$image" bash -euo pipefail -s "$port" <<'INNER'
port=$1
apt-get update -qq

# The repository as a user would add it, with the site's own URL rewritten to
# the local server standing in for GitHub Pages.
apt-get install -y -qq /site/tsuzuki-archive-keyring.deb >/dev/null
sed -i "s|URIs: .*|URIs: http://127.0.0.1:$port/apt|" /etc/apt/sources.list.d/tsuzuki.sources
apt-get update -qq

apt-get install -y -qq --no-install-recommends tsuzuki >/dev/null
tsuzuki version
echo "--- what apt sees ---"
apt-cache policy tsuzuki
dpkg -s tsuzuki | grep -E "^(Version|Depends)"

# Every release in the pool stays installable, not only the newest.
older=$(apt-cache madison tsuzuki | tail -1 | awk '{print $3}')
apt-get install -y -qq --no-install-recommends --allow-downgrades "tsuzuki=$older" >/dev/null
echo "--- pinned to $older ---"
tsuzuki version
INNER
