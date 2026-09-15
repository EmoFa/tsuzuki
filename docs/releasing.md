# Releasing

Pushing a version tag runs `.github/workflows/release.yml`, which uses goreleaser to:

1. build every platform and publish a GitHub release with archives, `.deb`, `.rpm`
   and Arch packages;
2. commit the updated cask to [EmoFa/homebrew-tap](https://github.com/EmoFa/homebrew-tap)
   (`brew install EmoFa/tap/tsuzuki`);
3. push the updated package to the AUR as `tsuzuki-bin`, once the `AUR_SSH_KEY`
   secret exists (without it this step is skipped);
4. commit winget manifests to the EmoFa/winget-pkgs fork and open a pull request to
   [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs)
   (`winget install EmoFa.tsuzuki` once it's merged).

Tags with a suffix, such as `v1.2.0-rc1`, make a GitHub prerelease and skip the
package managers.

## One-time setup

| What | How |
|---|---|
| Homebrew tap | A public, empty repo named `EmoFa/homebrew-tap`. |
| winget fork | Fork [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) to EmoFa (master branch only). No need to clone it or keep it in sync. |
| `PACKAGES_GITHUB_TOKEN` secret | A classic token (<https://github.com/settings/tokens/new>) with only the `public_repo` scope, saved in this repo under Settings → Secrets and variables → Actions. |
| AUR account | Register at <https://aur.archlinux.org/register>. |
| `AUR_SSH_KEY` secret | Create a key just for this: `ssh-keygen -t ed25519 -f ~/.ssh/aur -N "" -C "tsuzuki AUR"`. Paste `~/.ssh/aur.pub` into your AUR account (My Account → SSH Public Key), and save the contents of `~/.ssh/aur` (the private key) as this secret. |

The AUR package is created by the first release after the secret is added; nothing
needs creating there first. AUR registration is sometimes closed (it was in September
2026); until you have an account, releases simply skip the AUR. When it's published,
add `yay -S tsuzuki-bin` to the install table in the README.

## Making a release

```sh
make lint test
make snapshot     # optional: inspect the archives and package files in dist/
git tag v0.1.0
git push origin v0.1.0
```

Then watch the **Release** workflow under the repo's Actions tab.

### The first winget pull request

- A Microsoft bot asks you to accept their contributor license agreement: reply on
  the pull request with the comment it gives (`@microsoft-github-policy-service agree`).
- Automated validation downloads, scans and test-installs the package, then a
  moderator reviews it. Requests for changes arrive as PR comments with a
  `Needs-Author-Feedback` label.
- Once merged, `winget install EmoFa.tsuzuki` works within a few hours. Later
  versions go through the same pull request flow, usually faster.

### If a step fails

The GitHub release is published before the package managers are updated, so a
failure there (an expired token, a missing secret) leaves the release in place.
Fix the cause, then either:

- delete the GitHub release and the tag (`git push --delete origin v0.1.0`,
  `git tag -d v0.1.0`) and tag again, or
- release the fix as the next version.

Tokens expire: when `PACKAGES_GITHUB_TOKEN` does, create a new one and replace the
secret.
