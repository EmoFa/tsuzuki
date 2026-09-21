#!/usr/bin/env bash
# Build the package site published at https://emofa.github.io/tsuzuki/ from a
# directory of .deb packages: a signed apt repository under apt/, the public key
# to verify it, and a keyring package that sets both up on a user's machine.
#
#   build-apt-repo.sh <packages-dir> <output-dir>
#
# Signing uses the secret key already in the gpg keyring, named by
# APT_GPG_KEY_ID. Needs dpkg-dev, apt-utils and gnupg.
set -euo pipefail

packages=${1:?usage: build-apt-repo.sh <packages-dir> <output-dir>}
out=${2:?usage: build-apt-repo.sh <packages-dir> <output-dir>}
key=${APT_GPG_KEY_ID:?set APT_GPG_KEY_ID to the signing key fingerprint}
site=${APT_SITE_URL:-https://emofa.github.io/tsuzuki}
suite=${APT_SUITE:-stable}
component=${APT_COMPONENT:-main}
# Debian's architecture names, not Go's.
architectures=${APT_ARCHITECTURES:-amd64 arm64}
# Bumped by hand when the signing key or the source list changes.
keyring_version=${KEYRING_VERSION:-1.0}

keyring_package() {
	local build="$out/.keyring"
	mkdir -p "$build/DEBIAN" "$build/usr/share/keyrings" "$build/etc/apt/sources.list.d"
	gpg --export "$key" > "$build/usr/share/keyrings/tsuzuki-archive-keyring.gpg"
	cat > "$build/etc/apt/sources.list.d/tsuzuki.sources" <<-SOURCES
		Types: deb
		URIs: $site/apt
		Suites: $suite
		Components: $component
		Architectures: $architectures
		Signed-By: /usr/share/keyrings/tsuzuki-archive-keyring.gpg
	SOURCES
	cat > "$build/DEBIAN/control" <<-CONTROL
		Package: tsuzuki-archive-keyring
		Version: $keyring_version
		Architecture: all
		Maintainer: EmoFa <https://github.com/EmoFa>
		Section: misc
		Priority: optional
		Description: tsuzuki repository key and source list
		 Installs the key that signs the tsuzuki apt repository, and the source
		 list that points apt at it, so that tsuzuki can be installed and kept up
		 to date with apt.
	CONTROL
	dpkg-deb --build --root-owner-group "$build" "$out/tsuzuki-archive-keyring.deb" >/dev/null
	rm -rf "$build"
}

# release_file writes the index apt verifies: the repository's description and
# a digest of every file under it. Written here rather than with apt-ftparchive
# so the only tools needed are dpkg-dev and gnupg.
release_file() {
	local dist=$1
	cat <<-HEADER
		Origin: tsuzuki
		Label: tsuzuki
		Suite: $suite
		Codename: $suite
		Architectures: $architectures
		Components: $component
		Description: tsuzuki releases
		Date: $(date -u "+%a, %d %b %Y %H:%M:%S UTC")
	HEADER
	local algorithm
	for algorithm in MD5Sum:md5sum SHA256:sha256sum; do
		echo "${algorithm%%:*}:"
		local sum file
		while read -r file; do
			sum=$(${algorithm##*:} "$dist/$file" | cut -d' ' -f1)
			printf ' %s %16d %s\n' "$sum" "$(stat -c%s "$dist/$file")" "$file"
		done < <(cd "$dist" && find . -type f ! -name 'Release*' ! -name 'InRelease' -printf '%P\n' | sort)
	done
}

index_page() {
	cat > "$out/index.html" <<-HTML
		<!doctype html>
		<html lang="en">
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>tsuzuki packages</title>
		<style>
		  body { margin: 0 auto; padding: 2rem 1rem; max-width: 46rem; line-height: 1.6;
		         font-family: system-ui, sans-serif; color: #1c1917; background: #fafaf9; }
		  h1 { margin-bottom: 0; } h2 { margin-top: 2.5rem; }
		  p.lede { margin-top: .25rem; color: #57534e; }
		  pre { padding: .9rem 1rem; overflow-x: auto; background: #f5f5f4;
		        border: 1px solid #e7e5e4; border-radius: 6px; }
		  code { font-family: ui-monospace, monospace; font-size: .95em; }
		  a { color: #0369a1; }
		  @media (prefers-color-scheme: dark) {
		    body { color: #e7e5e4; background: #1c1917; }
		    p.lede { color: #a8a29e; }
		    pre { background: #292524; border-color: #44403c; }
		    a { color: #7dd3fc; }
		  }
		</style>
		<h1>tsuzuki</h1>
		<p class="lede">Watch anime from your terminal. <a href="https://github.com/EmoFa/tsuzuki">Source and documentation</a>.</p>

		<h2>Debian, Ubuntu and derivatives</h2>
		<p>Install the keyring package once, then tsuzuki upgrades with the rest of your system.</p>
		<pre><code>curl -fsSLO $site/tsuzuki-archive-keyring.deb
		sudo apt install ./tsuzuki-archive-keyring.deb
		sudo apt update &amp;&amp; sudo apt install tsuzuki</code></pre>

		<h2>Fedora</h2>
		<pre><code>sudo dnf copr enable emofa/tsuzuki
		sudo dnf install tsuzuki</code></pre>

		<h2>Elsewhere</h2>
		<p>Homebrew, winget, the AUR, Go and plain archives are covered in the
		<a href="https://github.com/EmoFa/tsuzuki#install">README</a>.</p>
		HTML
}

rm -rf "$out"
mkdir -p "$out/apt/pool/$component/t/tsuzuki"
cp "$packages"/*.deb "$out/apt/pool/$component/t/tsuzuki/"

gpg --export "$key" > "$out/tsuzuki.gpg"
keyring_package
index_page

cd "$out/apt"
for arch in $architectures; do
	dir="dists/$suite/$component/binary-$arch"
	mkdir -p "$dir"
	# --multiversion keeps every release in the pool listed, so an older one
	# can still be installed or pinned; without it only the newest is offered.
	dpkg-scanpackages --multiversion --arch "$arch" pool 2>/dev/null > "$dir/Packages"
	gzip -9fk "$dir/Packages"
	cat > "$dir/Release" <<-RELEASE
		Archive: $suite
		Component: $component
		Origin: tsuzuki
		Label: tsuzuki
		Architecture: $arch
	RELEASE
done

release_file "dists/$suite" > "dists/$suite/Release"

# Both signatures: InRelease for apt 1.1 and newer, Release.gpg for older.
gpg --batch --yes --default-key "$key" --clearsign -o "dists/$suite/InRelease" "dists/$suite/Release"
gpg --batch --yes --default-key "$key" -abs -o "dists/$suite/Release.gpg" "dists/$suite/Release"
