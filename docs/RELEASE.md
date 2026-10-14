# CI and Releases

CI validates the application and release artifacts across supported platforms.

It checks:

- frontend build, types and tests;
- Go tests and Staticcheck;
- generated bindings and layout;
- `go mod tidy -diff`;
- workflow linting.

Go comes from `go.mod`, Node from `.node-version`. Build tools and GitHub Actions are pinned.

## Releases

Releases are created from SemVer tags on commits already merged into `main`.

```sh
git tag -a v0.1.0-beta.1
git push origin v0.1.0-beta.1
```

The first command opens an editor for the release notes. Use bullet points:

```markdown
- Improved tab switching
- Fixed proxy settings
```

The workflow copies the tag annotation into the GitHub release and signed updater manifests. The application's update window displays lines starting with `- ` or `* `. Editing the GitHub release after publication does not change the notes already included in signed manifests.

Stable releases use tags without a prerelease suffix:

```text
v0.1.0
```

The release workflow builds:

- macOS universal;
- Windows amd64;
- Linux amd64.

The production frontend is built once and reused by native builds.

## Updater Signing

Before the first release, configure:

```text
UPDATER_KEY
```

It must match:

```text
internal/transport/updater/updater.key.pub
```

Generate the initial key with:

```sh
task updater:genkey
```

The command works on Windows, macOS and Linux. It writes the public key into the repository and prints the private key's temporary location.

Save the private key in a password manager or offline backup. In GitHub, open **Settings → Secrets and variables → Actions**, then create `UPDATER_KEY` with the complete contents of that file. Delete the temporary private key after backing it up and configuring the secret.

The command does not require GitHub CLI or upload secrets automatically. It skips generation when the public key already exists.

Do not replace the signing key after clients have been released. Installed clients trust the embedded public key.

## Publishing

The release workflow:

1. validates the tag and application;
2. builds native packages;
3. runs integration tests;
4. creates checksums;
5. signs and verifies updater manifests;
6. publishes the GitHub release;
7. advances update channels.

Stable releases update `stable` and may also advance `beta`.

Prereleases update only `beta`.

Older versions cannot roll an update channel backwards.

## Recovery

Rerun failed jobs for transient failures.

Before publication, draft assets may be replaced.

After publication, release artifacts are treated as immutable.

A release published through GitHub while creating a new tag may exist before the workflow finishes. Its published status does not mean that its packages are ready. The workflow uploads packages only after signing and verification succeed.

The workflow always uses the current build artifacts. It may replace draft assets, but refuses to overwrite published files. Once files are published, use a new version for another build.

Never move an existing release tag. Publish fixes under a new version.

## Repository Setup

Protect:

- `main`;
- release tags matching `v*`.

Require CI before merging.

Publishing requires:

```text
contents: write
```

Build jobs should remain read-only, and release secrets should only be available to the publishing job.

## Updater

Schemo uses the Wails updater for downloading and installing updates.

Update manifests select the platform artifact and require a valid Schemo signature.

Stable and beta channels are supported.

Updates are refused when the installation cannot be safely replaced, such as read-only locations.

## Signing Limitations

Updater artifacts are signed, but native packages are not currently signed with platform certificates.

Current limitations:

- macOS uses ad hoc signing and is not notarized;
- Windows has no Authenticode signature;
- operating-system trust warnings may appear.

Updater signing protects the application update path but does not replace native code signing.

## Platform Notes

Linux packages are built on Ubuntu 24.04 with GTK4 and WebKitGTK 6.0.

macOS 12 is the configured deployment target.
