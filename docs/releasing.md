# Releasing

Pushing a version tag runs `.github/workflows/release.yml`, which uses goreleaser to:

1. build every platform and publish a GitHub release with archives, `.deb`, `.rpm`
   and Arch packages;
2. commit the updated cask to [EmoFa/homebrew-tap](https://github.com/EmoFa/homebrew-tap)
   (`brew install EmoFa/tap/tsuzuki`);
3. push the updated package to the AUR as `tsuzuki-bin`;
4. commit winget manifests to a fork of
   [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) and open a pull
   request against it (`winget install EmoFa.tsuzuki` once merged).

Tags with a suffix, such as `v1.2.0-rc1`, make a GitHub prerelease and skip the
package managers.

`.github/workflows/packages.yml` then runs, covering the two repositories goreleaser
doesn't. It follows the Release workflow rather than the release it publishes, because
GitHub won't start a workflow from an event the Actions token created:

- **apt**, at <https://emofa.github.io/tsuzuki/apt>. The job rebuilds the repository
  from the `.deb` files of the last five releases, signs it, and deploys it to GitHub
  Pages along with the public key and `tsuzuki-archive-keyring.deb`, the package that
  sets a user's machine up. `packaging/build-apt-repo.sh` builds it and
  `packaging/test-apt-repo.sh` installs from it in a Debian container before it is
  published; both run locally too.
- **Fedora**, through [Copr](https://copr.fedorainfracloud.org/coprs/emofa/tsuzuki/).
  The job builds a source RPM from `packaging/tsuzuki.spec` and hands it to Copr, which
  builds it for each enabled Fedora release. Copr builds without network access, so the
  release carries a `tsuzuki-<version>-vendor.tar.gz` of the Go dependencies that the
  spec builds from.

`workflow_dispatch` runs it against the latest release: with the dry run left on it
signs with a throwaway key and publishes nothing, which is the way to test a change to
either, and with it off it republishes both repositories.

## Making a release

```sh
make lint test
make snapshot     # optional: inspect the archives and package files in dist/
git tag v0.1.0
git push origin v0.1.0
```

The **Release** workflow under the repository's Actions tab reports what happened.

## Repository setup

Steps 2–4 need these once; each is skipped when its secret is missing, so a release
still publishes without them.

| What | How |
|---|---|
| Homebrew tap | A public repository named `homebrew-tap` under the same owner. |
| winget fork | A fork of [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) (master branch only). It doesn't need cloning or keeping in sync. |
| `PACKAGES_GITHUB_TOKEN` secret | A classic token (<https://github.com/settings/tokens/new>) with only the `public_repo` scope, saved under Settings → Secrets and variables → Actions. It pushes to the tap and the winget fork, and expires like any token. |
| `AUR_SSH_KEY` secret | A dedicated key (`ssh-keygen -t ed25519 -f aur -N "" -C "tsuzuki AUR"`). The public half goes on an [AUR account](https://aur.archlinux.org) under My Account → SSH Public Key, the private half into the secret. The first release after that creates the package. |
| GitHub Pages | Settings → Pages → Source: **GitHub Actions**. The apt repository is published there. |
| `APT_GPG_PRIVATE_KEY` secret | The armoured private half of a passphrase-less signing key (`gpg --quick-gen-key 'tsuzuki repository <…>' rsa4096 sign never`). Users trust its public half, so replacing it means every machine reinstalls the keyring package. |
| `COPR_LOGIN`, `COPR_USERNAME`, `COPR_TOKEN` secrets | From the config block at <https://copr.fedorainfracloud.org/api/>, for the account owning the `tsuzuki` project. Tokens expire; the page issues new ones. |

Copr retires a chroot when its Fedora release goes end of life, so the project's chroot
list (Settings → Edit project) needs ticking forward about once a year. The spec needs a
Go toolchain at least as new as the `go` directive in `go.mod`, which is what decides
whether a given Fedora release can build it.

## Winget pull requests

- The first pull request from an account needs Microsoft's contributor licence
  agreement: their bot comments asking for a reply of
  `@microsoft-github-policy-service agree`.
- Automated validation downloads, scans and test-installs the package, then a moderator
  reviews it. Requested changes arrive as comments labelled `Needs-Author-Feedback`.
- Once merged, `winget install EmoFa.tsuzuki` works within a few hours. Later versions go
  through the same flow, usually faster.

## When a step fails

The GitHub release is published before the package managers are updated, so a failure
there (an expired token, a missing secret) leaves the release in place. Fix the cause,
then either release the fix as the next version, or delete the release and tag
(`git push --delete origin v0.1.0`, `git tag -d v0.1.0`) and tag again.
