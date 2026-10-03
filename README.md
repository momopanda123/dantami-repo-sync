# Dantami Repo Sync

**English** · [한국어](README.ko.md)

Manage GitHub ↔ Gitea repository synchronization from a Synology DSM dashboard.

- Multiple repository pairs; bidirectional or one-way synchronization
- A dashboard for status, schedules, conflicts, history and connection management
- Independent app accounts with administrator and read-only viewer roles
- Korean / English language switching on the login page and dashboard
- No remote deletion, force push or automatic conflict merge

## Install

Download the `.spk` from [Releases](https://github.com/momopanda123/dantami-repo-sync/releases).
Supported target: **DSM 7, Intel/AMD x86_64**. ARM NAS models and DSM 6 are not supported.

1. Open **Package Center → Manual Install** and select the SPK. Upgrade an existing installation without uninstalling it.
2. On first installation, create an app administrator in the installation wizard. This account is separate from DSM.
3. Open the app over **HTTPS** and sign in. Existing app accounts are preserved on subsequent upgrades.
4. Use the language selector at the top right to choose **한국어** or **English**. The selection is saved in this browser. Switching language reloads the page and warns about unsaved password/token input.

Usernames accept 3–40 letters, numbers, dots, underscores or hyphens. Passwords must be 12–72 bytes and match the confirmation.
There is no public first-user registration endpoint. If no initial app administrator is configured, the service fails closed.

## Connect repositories

1. Open **Connections** and enter your Gitea HTTPS server root URL and Gitea username.
2. Create and enter your GitHub and Gitea access tokens. Keep tokens private and grant only required access.
   - GitHub: use a fine-grained token for selected repositories, with **Contents: Read and write**. Add **Workflows: Read and write** if you sync workflow files.
   - Gitea: repository read/write access is required. Ordinary personal tokens are based on the account's access. Use a dedicated limited-access Gitea account when repository-level isolation is needed.
3. Confirm private NAS storage and select **Save connection and load repositories**.
4. Select both repositories and the direction under **Connect repositories**.
5. Run **Check connection**, review the result, then enable automatic sync.

New installations start with an empty list. No personal server, username or repository is preconfigured.
Local Gitea discovery can identify servers on the NAS's loopback interface. Manually entered servers must use HTTPS and must not have a URL subpath.
Changing servers requires re-entering the Gitea token rather than silently reusing it for a new destination.

## Access checks

An API listing or account-level `permissions.push` value alone does not establish effective token access.
Candidate repositories are checked using authenticated, read-only Git upload-pack and receive-pack service advertisements. Only verified candidates are listed, and the selected pair is checked again before registration.

These probes send no POST, commit, ref update or pack data. Expired lists and failed refreshes cannot be used for new registration.
They do not introspect the provider's token-settings page or guarantee that every subsequent push will pass branch protection or workflow-specific policies.
The current candidate model requires read/write service access on both sides, including for a one-way connection; read-only source tokens are not supported by this filtering model.

## Accounts and management

**User accounts** supports password changes and administrator-managed creation, disabling, enabling, password reset and deletion.
Administrative account changes require the administrator's current password. Viewers can read the shared dashboard; repositories are not isolated into separate per-user workspaces.
An administrator cannot disable or delete their own account. Public signup and email-based password recovery are not provided.

Passwords are stored as bcrypt hashes (cost 12). Secure, HttpOnly, SameSite=Strict session cookies expire after eight hours. Logout, disabling an account and password changes invalidate applicable sessions. Service restarts require signing in again.
The app uses its own authentication, not DSM administrator sessions. DSM provides package installation, lifecycle and web routing.

Connections can be searched, paused, archived/restored or removed. **Remove connection** asks for confirmation and deletes only this app's connection settings, local cache and check history. Remote repository code is untouched.
Re-adding a removed connection starts without its previous deletion-detection history and requires a fresh check.
Legacy automatically seeded connections are removed from the active list during upgrade and backed up once in private NAS storage. User-added connections remain.

## Sync scope and limitations

Commit ancestry determines direction, not timestamps. New commits, branches and tags are copied when safe. Divergent branches and conflicting tags require manual review.
Up to two jobs run concurrently. Destination-only changes are preserved in one-way mode.
Git LFS data, issues, pull requests, release assets and submodule repositories are not synchronized. LFS pointers stop automatic updates rather than silently omitting their data.
A network failure can leave partially completed work; subsequent checks compare actual remote states. There is no cross-server transaction.

## Build and test

Requirements: Go 1.27.1+, Python 3 with Pillow, and Git CLI for integration tests.

```sh
mkdir -p .agents dist
python3 generate-locales.py
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o .agents/repo-sync .
go list -m -json all > .agents/modules.json
python3 build-package.py
```

The reviewed catalog is `web/i18n.json`. Run `generate-locales.py` after editing UI strings; missing translations fail generation. Generated English UI assets are checked in so ordinary Go builds can embed them.
The package includes dependency licenses. Runtime account data, tokens, caches, logs and development scratch files must not be committed.

## Verification

Local tests cover synchronization, account/session security, access filtering, upgrade behavior and localization. UI checks cover key DOM flows and style isolation.
Installation and synchronization have been reported working on an initial deployment. This is not comprehensive validation across DSM versions, NAS hardware, proxies, browser rendering or all remote protection policies.

## License

[MIT](LICENSE) · Copyright © 2026 momopanda123
