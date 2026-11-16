# Connecting GitHub

Ivaldi reads repositories and commit history from GitHub through a GitHub App that you register. Organizations connect their GitHub accounts by installing the App. Before Ivaldi records an installation, GitHub confirms that the person installing it can access it, so one Organization cannot connect another's account by guessing its installation ID.

Without this configuration the control plane still starts, but connecting GitHub and creating Sources fail with a clear error.

## Register the GitHub App

On github.com, or on your GitHub Enterprise Server, create a GitHub App with:

- **Callback URL**: `<web app URL>/settings/connections/github/callback`, such as `https://ivaldi.example.com/settings/connections/github/callback`. This is the web app's address, not the control plane's. If the same App also signs people in (`auth.providers.github`), keep its sign-in callback too, in any order. GitHub returns from an install to the first callback URL in the list, and its settings sort the list themselves; when that is the sign-in callback, the control plane sends the install on to the web app's page. Leave **Setup URL** empty: GitHub disables it and ignores it while user authorization during installation is on.
- **Redirect on update** (optional): on, so someone can connect an account where the App is already installed by choosing it on GitHub's install page, rather than uninstalling and installing again.
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
| `version_control.github.install_callback_url` | `IVALDI_VERSION_CONTROL_GITHUB_INSTALL_CALLBACK_URL` | The web app's page that finishes an install. Leave it empty to use `/settings/connections/github/callback` on `auth.login_url`'s host. |
| `version_control.github.api_url` | `IVALDI_VERSION_CONTROL_GITHUB_API_URL` | Overrides the API address, which is otherwise `https://api.github.com`, or `<web_url>/api/v3`. |

If any of these is set, the control plane refuses to start until the required ones are all set. Secrets are never read from the YAML file. Ivaldi contacts only this one GitHub host: a Connection that names any other host cannot be used.

Install state is sealed with keys derived from `auth.signing_key_files`, so every replica must have the same signing keys.

## Receiving webhooks

GitHub tells Ivaldi when a repository is renamed, transferred, edited, archived, or unarchived, and when an installation is suspended, unsuspended, or uninstalled. The gateway (`ivaldi gateway`) receives these webhooks, checks their signature, resolves the installation to its Connection by asking the control plane as a platform workload ([ADR 0015](../adr/0015-platform-workloads.md)), and publishes them for the control plane to apply. Without webhooks, Sources change only when someone refreshes them.

In the App's settings on GitHub:

- **Webhook → Active**: on.
- **Webhook URL**: the gateway's public address and path, such as `https://ivaldi.example.com/webhooks/github`. GitHub must be able to reach it over HTTPS; terminate TLS in front of the gateway.
- **Webhook secret**: a long random value, such as `openssl rand -hex 32`. The gateway reads the same value from `gateway.webhooks.github.secret_file`.
- **Subscribe to events**: **Repository**. Installation events are sent to every App without subscribing.

Then give the gateway its identity and the control plane its public key:

1. Generate the gateway's key pair: `openssl genpkey -algorithm ed25519 -out gateway-key.pem` and `openssl pkey -in gateway-key.pem -pubout -out gateway-key.pub`.
2. On the control plane, list it as a platform workload:

   ```yaml
   platform:
     workloads:
       - name: gateway
         component: gateway
         public_key_files: [/etc/ivaldi/gateway-key.pub]
   ```

3. On the gateway, set `gateway.identity.private_key_file` to the private key, `gateway.control_plane_url` to an address where it reaches the control plane, and `gateway.webhooks.github.secret_file` to the webhook secret. Both read the same `redis` and `events.stream` settings.

To rotate the key, list the new public key beside the old one, restart the control plane, switch the gateway to the new private key, then remove the old public key.

After saving the App's webhook settings, GitHub sends a `ping`; **Advanced → Recent Deliveries** on the App's page should show it answered with `202`. A delivery for an installation no Organization has connected is answered with `404`.

### Locally

`make local_stack` runs the gateway on `http://localhost:8663`, with the key pair and webhook secret that `make local_secrets` generates in `.local/`. To let GitHub reach it, expose it with [Tailscale Funnel](https://tailscale.com/kb/1223/funnel):

```sh
tailscale funnel --bg 8663     # serves https://<machine>.<tailnet>.ts.net
tailscale funnel status        # shows the URL
tailscale funnel reset         # stops it
```

Funnel must be allowed for the machine in the tailnet's policy, and changing it needs root unless you once run `sudo tailscale set --operator=$USER`. Use `https://<machine>.<tailnet>.ts.net/webhooks/github` as the App's webhook URL and the contents of `.local/github-webhook-secret` as its secret.

## The install flow

Owners and admins start it from **Organisation settings → Connections → Connect GitHub**. Under the hood:

1. The web app calls `POST /v1/connections/installations` with `{"provider": "github"}`. The caller needs `connection.manage` on the Organization. The response gives a `url` and an `expires_at` 30 minutes away.
2. The web app sends the person to `url`, where they install the App and authorize it.
3. GitHub sends them to the callback URL with `code`, `installation_id`, and `state`.
4. The callback page (signing the person in again first if their session lapsed) calls `POST /v1/connections/installations/complete` with `provider`, `state`, `code`, and `installation_id`, as the same signed-in person. Ivaldi checks the state was issued to them for their Organization, exchanges the code for their GitHub user token, and creates the Connection only if the installation is one of the App's installations they can access. It does not keep the user token. The page then returns to Connections and removes the code from the address bar and history.

An installation can be connected to one Organization at a time. Archive its Connection to connect it elsewhere.

## Recording Changes

Release notes map commits to work through **Changes**. A Session, a CI job, or an automation records one with `POST /v1/sources/{source_id}/changes`:

```json
{
  "commit_shas": ["1a2b3c4d", "5e6f7a8b"],
  "session_id": "ses_...",
  "work_item_id": "wki_...",
  "title": "Add checkout",
  "external_ref": "https://github.com/acme/checkout/pull/42"
}
```

`commit_shas` must be the full SHAs as the provider reports them. `session_id`, `work_item_id`, and `external_ref` are optional. Recording needs `change.record` on the Source, and reading needs `change.read`. A commit belongs to one Change in its Source; recording it again returns `409`. The deploy sheet then shows the Change, Session, and Work Item next to the commit.

Until the Session capability records Changes itself, this endpoint is how CI or an automation supplies the link. From CI, `ivaldictl change record` calls it, taking the commit from the pipeline ([Creating Releases from CI](ci-releases.md#recording-which-work-produced-a-commit)).
