# Notifications

Ivaldi tells people about work that waits for them. So far that is approval requests: when a Deployment waits for a human, the people who could approve it are told, and when it is decided, the person who asked hears the outcome ([design](../design/notifications.md)).

Every notification appears in the web app's inbox. Email is optional and off until you configure an SMTP server.

## Who is told

- A waiting approval request goes to the owning Team's members who could decide it. If none of them could, it goes to the Organization's owners and admins who could. Nobody is told about a request they cannot decide, and the person who asked is never asked to approve it.
- An approval or a rejection goes to the person who asked, or the person whose agent Session asked.

Notifications run in `serve`, which reads the outbox stream with its own consumer group:

```yaml
notifications:
  consumer_group: notifications   # default
  concurrency: 10                 # default
  dead_letter_stream: ivaldi:control-plane:events:notifications:dead
```

## Email

```yaml
external_url: https://ivaldi.example.com
notifications:
  email:
    smtp_address: smtp.example.com:587
    username: ivaldi
    password_file: /etc/ivaldi/smtp-password
    from: Ivaldi <ivaldi@example.com>
    tls: starttls
```

| Key | Environment variable | Meaning |
|---|---|---|
| `smtp_address` | `IVALDI_NOTIFICATIONS_EMAIL_SMTP_ADDRESS` | `host:port` of the SMTP server. Email is off while it is empty. |
| `username` | `IVALDI_NOTIFICATIONS_EMAIL_USERNAME` | Account to authenticate as, if the server needs one. |
| `password_file` | `IVALDI_NOTIFICATIONS_EMAIL_PASSWORD_FILE` | File holding the account's password. Mount it from a Kubernetes Secret. |
| `from` | `IVALDI_NOTIFICATIONS_EMAIL_FROM` | The sender, such as `Ivaldi <ivaldi@example.com>`. Your server must be allowed to send as it. |
| `tls` | `IVALDI_NOTIFICATIONS_EMAIL_TLS` | `starttls` (the default, usually port 587), `tls` (implicit TLS, usually port 465), or `none`. |

- With `starttls`, Ivaldi refuses to send if the server does not offer STARTTLS, so the password never travels in plain text.
- `none` is only allowed when the server is on the same host (`localhost` or a loopback address), such as a local relay or a mail catcher like Mailpit during development.
- Links in emails are made from `external_url`, where the control plane serves the web app, so set it to the address people use.

An email carries the notification's title and text and a link to the request. It never carries the request's details or any secret.

### Delivery

- Each person gets an email only if their account has an email address. One without is skipped and still sees the notification in the inbox.
- If the server refuses or cannot be reached, the event is handled again later and only the emails not yet sent are retried. After the consumer's last attempt the event moves to the dead-letter stream and those emails are not sent; the notifications stay in the inbox.
- Rarely, an email can arrive twice: if `serve` stops after the server accepted a message but before Ivaldi recorded it. SMTP has no way to send a message idempotently, so a duplicate is preferred to a missed email.
- Every send failure is logged with the notification's ID.
