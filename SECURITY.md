# Security policy

Accountedfor holds the names, locations and phone numbers of people in a
crisis. Please treat security problems seriously and report them privately.

## Reporting a vulnerability

Use GitHub's **private vulnerability reporting**: open the repository's
**Security** tab and choose **Report a vulnerability**. Please include steps to
reproduce and the version (`accountedfor version` or `/version`).

Do not open a public issue for a vulnerability, and do not test against a
board that is in use during a real emergency.

You can expect an acknowledgement within a few days. Fixes are released as
patch versions and noted in [CHANGELOG.md](CHANGELOG.md).

## Supported versions

Only the latest minor release receives security fixes.

## Design notes for reviewers

- Pages contain no JavaScript; the Content Security Policy is
  `default-src 'none'` plus a hash for the one inline stylesheet.
- Forms are protected by Go's cross-origin protection (`Sec-Fetch-Site` /
  `Origin`); responder forms also carry a per-session CSRF token.
- Passwords are hashed with bcrypt. Session tokens, edit links and API
  tokens are stored only as SHA-256 hashes.
- Twilio webhooks are verified with `X-Twilio-Signature`; other SMS gateways
  must send a shared secret.
- Contact details are never rendered on public pages or in the public PFIF
  feed. All exports, imports and moderation actions are written to the audit
  log.
