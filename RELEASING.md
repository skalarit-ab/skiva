# Releasing Skiva

Push a tag, and the release workflow does the rest. It tests Skiva on
Linux and Windows, builds both, signs the checksums, and publishes a
GitHub release. Installed copies find it there and update themselves.

## Version numbers

Desktop and Android share one version number, `vMAJOR.MINOR.PATCH`, as
`v0.3.0`. A beta adds `-beta.N`, as `v0.3.0-beta.1`.

The desktop can release far more often than Android. That is fine:
Android skips the numbers it does not ship. The desktop might go
v0.3.0, v0.3.1, v0.3.2 while Google Play goes from v0.3.0 to v0.3.2.
Play orders builds by a hidden version code, which `gunimapk` sets
from the time of the build, so it always goes up.

## A desktop release

1. Add a section to `CHANGELOG.md` headed `## v0.3.0`. Skiva's update
   window shows it as what's new. Without one, the release notes are
   the commit subjects since the last tag.
2. Tag and push:

   ```sh
   git tag v0.3.0
   git push origin v0.3.0
   ```

The release holds:

- `skiva_0.3.0_windows_amd64.exe`, the Windows program. It is its own
  installer: run from a download, it installs Skiva for the user, with
  no administrator, into `%LOCALAPPDATA%\Programs\skiva`. It offers to
  open MP3, FLAC, Ogg Vorbis and WAV files with Skiva.
- `skiva_0.3.0_linux_amd64.tar.gz`, the Linux program, which installs
  itself the same way into `~/.local/share`.
- `SHA256SUMS`, and `SHA256SUMS.sig`, its signature.

To try the build without publishing, run the workflow by hand. It keeps
what it built as an artifact of the run:

```sh
gh workflow run release.yml --ref main
```

## A beta

Tag `v0.3.0-beta.1`. A tag with a dash is published as a GitHub
pre-release. Only copies set to **Betas too** in Settings get it. A beta
build takes betas until the user picks **Releases**, so a beta tester
goes on to the next beta.

## Android

Build the App Bundle with the same version as the tag, and upload it to
Google Play. A beta goes to a testing track there.

```sh
go run . -write-icon /tmp/skiva-icon.png
go run github.com/marrasen/gunim/tools/gunimapk -keystore ~/keys/skalarit-upload.jks -key upload \
	-id se.skalarit.skiva -name Skiva -icon /tmp/skiva-icon.png -permissions music,playback \
	-version 0.3.0 -o /tmp/skiva.aab .
```

`gunimapk` does not yet stamp the version into Skiva's own code, so
the Android settings do not show the version yet.

## The signing key

An installed Skiva runs an update only if `SHA256SUMS` carries a
signature that the public key in `install.go`, `updateKey`, checks.

- The private key is in `~/keys/skiva-release.key` on the machine it
  was made on, and in the repository's `GUNIM_SIGN_KEY` Actions secret.
- Keep a copy somewhere safe, such as a password manager. Never commit
  it.
- If it is lost, installed copies take no more updates. Their users
  must download the next release by hand.
- To change it, publish one release signed with the old key that holds
  the new public key. Then sign every later release with the new key.

## Windows SmartScreen

The Windows program carries no Authenticode signature yet. Windows may
warn that it is from an unknown publisher the first times it is
downloaded. Signing it with a code-signing certificate stops that.
