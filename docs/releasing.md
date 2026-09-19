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
