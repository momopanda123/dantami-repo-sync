# Dantami Repo Sync

**English** · [한국어](README.ko.md) · [Download installer](https://github.com/momopanda123/dantami-repo-sync/releases/latest) · [All versions](https://github.com/momopanda123/dantami-repo-sync/releases)

Connect GitHub and Gitea, check their differences, then keep selected repository pairs in sync from your Synology NAS.

![Repository dashboard with status counters and individual sync controls](docs/images/00-dashboard.png)

> These are real Korean-language screenshots from v1.4.1, cropped for clarity. Account and repository identifiers were replaced with examples; no access tokens are shown. v1.5.0 adds a Korean/English selector that is not present in these older screenshots. The guide below uses current English button names, with Korean labels where useful.

## Before you start

- A **Synology NAS running DSM 7 on Intel/AMD x86_64**, accessible over HTTPS
- An existing GitHub repository and an existing Gitea repository to pair
- A GitHub token and a Gitea token for the intended repositories
- Decide whether to synchronize both ways or in one direction. Review differences before enabling automatic sync

**Three different credentials:** your **app account** signs you into Dantami Repo Sync; the **GitHub token** connects GitHub; the **Gitea token and username** connect Gitea. Your DSM password is not the app password.

## 1. Install and sign in

1. Download the `.spk` under **Assets** on the [latest release](https://github.com/momopanda123/dantami-repo-sync/releases/latest).
2. In DSM, select **Package Center → Manual Install** and choose the file. Upgrade an existing installation without uninstalling it.
3. When first prompted, create an **app administrator** in the installer. Use that app account to sign in over HTTPS.
4. On v1.6.2 or later, open **Settings**, choose **English** or **한국어**, then **Save settings**. On the login page, use the language selector. The choice is saved in this browser.

![App login form, separate from DSM authentication](docs/images/01-sign-in.png)

**Expected result:** the dashboard opens. A new installation starts with no connected repositories. Existing app accounts are preserved when upgrading.

## 2. Create the two access tokens

### GitHub

1. Open [Fine-grained personal access tokens](https://github.com/settings/personal-access-tokens/new).
2. Give the token a recognizable name, such as **Dantami Repo Sync**, and choose an expiry.
3. Select the correct **Resource owner**.
4. Under **Repository access**, choose **Only select repositories** and select the repositories to synchronize.
5. Grant **Contents → Read and write**. If you synchronize `.github/workflows` files, also grant **Workflows → Read and write**.
6. Select **Generate token**, copy it, and enter it only in the app's GitHub token field.

### Gitea

1. Sign in to **your own Gitea server** and open **Settings → Applications** (`/user/settings/applications`).
2. Choose **Generate New Token**, name it, and grant **repository → Read and Write** with access to private repositories if needed.
3. Copy the generated value into the app's Gitea token field. UI wording can vary by Gitea version.

> Ordinary Gitea personal tokens are based on the account's repository access, rather than GitHub's selected-repository model. For strict isolation, use a dedicated Gitea account granted write access only to the intended repositories. Hiding entries in this app does not reduce the token's actual permissions.

Never paste real tokens into issues, screenshots or README files. They may only be shown once when created.

## 3. Save the GitHub and Gitea connections

Open **Connections (계정 연결)** from the left menu.

![Connection form with an anonymized Gitea username and no exposed tokens](docs/images/02-connections.png)

1. Enter your **Gitea server URL**, such as `https://git.example.com`, and your **Gitea username**.
2. Enter the GitHub and Gitea tokens in their respective fields.
3. Select the checkbox consenting to private storage on the NAS.
4. Click **Save connection and load repositories (연결 저장 · 목록 불러오기)**.

**Expected result:** tokens show as saved and repository lists load after access checks.

**About the local address in the screenshot:** `127.0.0.1` means the NAS itself, not your PC. Use **Find Gitea on this NAS** to select a verified local endpoint, or enter your own HTTPS server URL. Do not blindly copy the example address. Once a token is saved, its field stays blank unless you are replacing it.

## 4. Pair the repositories

Click **Connect repositories (저장소 연결)** at the top right.

![Repository pairing dialog with anonymized example repository paths](docs/images/03-pair-repositories.png)

1. Refresh the lists if needed.
2. Select the GitHub repository on the left and the matching Gitea repository on the right. The two names do not have to be identical.
3. Choose a direction:
   - **Bidirectional:** new work on either side is considered for the other
   - **GitHub → NAS:** GitHub is the source
   - **NAS → GitHub:** Gitea on the NAS is the source
4. Click **Add connection (연결 추가)**.

**Expected result:** a new card appears on the dashboard, with automatic sync initially off.

The screenshot shows repositories that are **already connected**. For a new pair, select an unused repository; already-connected entries cannot be registered again. If only one repository is permitted by a GitHub token, only the verified candidate should appear. Several Gitea entries can be normal for an account-wide token.

## 5. Check first, then enable synchronization

On the new card, select **Check connection (연결 확인)**. This compares repositories without applying changes. Review the result in **Details (상세 보기)** before enabling automatic sync.

![Close-up of a healthy connection card and its management controls](docs/images/04-sync-controls.png)

This screenshot shows a connection whose check is already complete and automatic sync is enabled, so its buttons read **Sync now** and **Pause** rather than the initial **Check connection** and **Enable automatic sync**.

| Control / status | What it means |
| --- | --- |
| Check connection | Compare without changing remote repositories |
| Sync now | Request synchronization immediately |
| Enable automatic sync / Pause | Start or pause scheduled jobs |
| Details | Review branches, tags, conflicts, history, direction and interval |
| Remove connection | Confirm removal of app settings/cache/history; remote repositories remain |
| Healthy | The completed check found the pair in a healthy state |
| Needs attention / conflict | Review the affected references; do not force-overwrite them |

The NAS and package must remain running for scheduled jobs. Closing the browser does not stop the service. Branch divergence and conflicting tags require review; the app does not delete remote refs, force push or automatically merge conflicts.

## If something does not work

Open **Diagnostics (실행 진단)** to check the app's service and private storage.

![NAS diagnostic checks; these do not prove remote repository access](docs/images/05-diagnostics.png)

| Symptom | First check |
| --- | --- |
| Cannot sign in | Use the app account created during installation, in an HTTPS browser session |
| Repository missing | Check token access/expiry, then refresh the repository lists |
| Gitea shows many repositories | Check the Gitea account's effective access; this may be expected |
| Token saved but connection fails | Check server URL, certificate, NAS network access and token permissions |
| Conflict appears | Open Details and review the branches/tags; automatic merging is not performed |
| Diagnostics pass but sync fails | Local service health does not prove remote access; review token and repository errors |

For support, share the app version, failing step and error message. Hide tokens, usernames, private server URLs and private repository names in screenshots.

---

After a service restart or update, enabled pairs are checked first. Safe pending changes are synchronized immediately after that check, without waiting for the configured interval. Normal scheduling resumes after synchronization. Explicit **Check connection** actions remain read-only.

## Settings

From v1.6.2, choose **Settings** in the left menu (also visible in the compact/mobile menu).

- **Language:** 한국어 / English. Select a language and **Save settings**; the page reloads. The login page also retains its language selector.
- **Dashboard refresh:** 4 seconds (default), 10/30/60 seconds, or manual. Use **Refresh** in the header at any time. This only changes browser polling; NAS sync schedules continue independently.
- **New connection defaults:** sync direction and automatic check interval. These are preselected when connecting a new repository and can be overridden there. Existing pairs remain unchanged; new pairs still require a connection check and explicitly enabling automatic sync.
- **Load defaults:** fills the settings form; only **Save settings** applies it. Cancel, Close or Escape discards unsaved settings.
- **About:** shows the installed version. App accounts/passwords and service tokens remain in their existing account menus.

Preferences are stored only in this browser, not synchronized across devices. Language cookies last up to one year; browser privacy settings can clear them earlier. No tokens or passwords are stored with these preferences. If browser storage is blocked, the app reports that it could not save.

Older v1.4.1 screenshots below do not show this menu; update the SPK to v1.6.2 to use it.

<details>
<summary>Advanced behavior, security, build instructions and license</summary>

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

Requirements: Go 1.26.0+, Python 3 with Pillow, and Git CLI for integration tests.

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

</details>
