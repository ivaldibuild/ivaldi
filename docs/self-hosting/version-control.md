# Connecting GitHub

Ivaldi reads repositories and commit history from GitHub through a GitHub App that you register. Organizations connect their GitHub accounts by installing the App. Before Ivaldi records an installation, GitHub confirms that the person installing it can access it, so one Organization cannot connect another's account by guessing its installation ID.

Without this configuration the control plane still starts, but connecting GitHub and creating Sources fail with a clear error.

## Register the GitHub App

On github.com, or on your GitHub Enterprise Server, create a GitHub App with:

- **Callback URL**: the web app's page that completes an install (see [The install flow](#the-install-flow)).
- **Request user authorization (OAuth) during installation**: on. GitHub then sends the person back to the callback URL with an authorization code, which Ivaldi uses to check they can access the installation.
- **Repository permissions**: Metadata (read) and Contents (read).

Generate a private key and a client secret for the App, and note its App ID, client ID, and slug (the name in its URL, as in `https://github.com/apps/<slug>`).

## Configure the control plane

| Setting | Environment variable | |
|---|---|---|
| `version_control.github.app_id` | `IVALDI_VERSION_CONTROL_GITHUB_APP_ID` | The App ID. |
| `version_control.github.slug` | `IVALDI_VERSION_CONTROL_GITHUB_SLUG` | The App's slug. |
| `version_control.github.client_id` | `IVALDI_VERSION_CONTROL_GITHUB_CLIENT_ID` | The App's client ID. |
| `version_control.github.client_secret_file` | `IVALDI_VERSION_CONTROL_GITHUB_CLIENT_SECRET_FILE` | A file holding the client secret. `IVALDI_VERSION_CONTROL_GITHUB_CLIENT_SECRET` sets it directly; setting both is an error. |
| `version_control.github.private_key_file` | `IVALDI_VERSION_CONTROL_GITHUB_PRIVATE_KEY_FILE` | A file holding the App's PEM private key. |
| `version_control.github.web_url` | `IVALDI_VERSION_CONTROL_GITHUB_WEB_URL` | GitHub Enterprise Server only, such as `https://ghe.example.com`. Leave it empty for github.com. |
| `version_control.github.api_url` | `IVALDI_VERSION_CONTROL_GITHUB_API_URL` | Overrides the API address, which is otherwise `https://api.github.com`, or `<web_url>/api/v3`. |

If any of these is set, the control plane refuses to start until the required ones are all set. Secrets are never read from the YAML file. Ivaldi contacts only this one GitHub host: a Connection that names any other host cannot be used.

Install state is sealed with keys derived from `auth.signing_key_files`, so every replica must have the same signing keys.

## The install flow

1. The web app calls `POST /v1/connections/installations` with `{"provider": "github"}`. The caller needs `connection.manage` on the Organization. The response gives a `url` and an `expires_at` 30 minutes away.
2. The web app sends the person to `url`, where they install the App and authorize it.
3. GitHub sends them to the callback URL with `code`, `installation_id`, and `state`.
4. The callback page calls `POST /v1/connections/installations/complete` with `provider`, `state`, `code`, and `installation_id`, as the same signed-in person. Ivaldi checks the state was issued to them for their Organization, exchanges the code for their GitHub user token, and creates the Connection only if the installation is one of the App's installations they can access. It does not keep the user token.

An installation can be connected to one Organization at a time. Archive its Connection to connect it elsewhere.
