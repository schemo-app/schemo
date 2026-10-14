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
git tag -a v0.1.0-beta.1 -m "First beta"
git push origin v0.1.0-beta.1
```

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

Keep an offline backup of the private key.

Do not replace the signing key after clients have been released. Installed clients trust the embedded public key.

## Publishing

The release workflow:

1. validates the tag and application;
2. builds native packages;
3. runs integration tests;
4. creates checksums;
5. signs updater manifests;
6. publishes the GitHub release;
7. advances update channels.

Stable releases update `stable` and may also advance `beta`.

Prereleases update only `beta`.

Older versions cannot roll an update channel backwards.

## Recovery

Rerun failed jobs for transient failures.

Before publication, draft assets may be replaced.

After publication, release artifacts are treated as immutable.

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