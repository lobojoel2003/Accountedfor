# Changelog

All notable changes to Accountedfor are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each release's binaries and container image also carry a **build number**
(the CI run that produced them), shown in the page footer, at `/version` and
by `accountedfor version`.

## [Unreleased]

## [1.0.0] - 2026-11-12

First public release.

### Added

- **"I'm safe" in one tap.** A home page built around one large button, a
  form that takes under a minute, and the option to add family members who
  are safe together.
- **Privacy choices on every post**: *Everyone*, *Name only* (the default) or
  *Responders only*. Phone numbers and email addresses are never shown
  publicly.
- **Search** that ignores accents, case and small spelling differences
  (PostgreSQL trigram matching).
- **Missing-person reports** with updates from anyone ("I've heard from them",
  "I'm looking too", other information) and private notes for responders.
- **Reunions**: after posting "I'm safe", people are shown reports looking for
  a similar name and can answer them directly. Responders review other
  possible matches and link or dismiss them.
- **Private edit links** so authors can change, update or remove their post
  without an account.
- **SMS gateway** for people without data: `SAFE`, `FIND`, `MISSING`,
  `DELETE` and `HELP`, in English and Spanish, via Twilio (signature-verified)
  or any gateway that can call a webhook.
- **PFIF 1.4 export and PFIF 1.1–1.4 import**, the format used by Google Person
  Finder, from the dashboard, the command line and a token-protected API
  (`/api/v1/pfif`). Deletions are exported as expired records so copies are
  removed downstream. A public feed (`/pfif.xml`) carries only public fields.
- **Responder dashboard**: statistics, filters, CSV export (safe against
  spreadsheet formula injection), bulk entry from shelter lists, moderation
  (hide, change visibility, delete), accounts with admin and responder roles,
  API tokens and an audit log of sign-ins, exports, imports and moderation.
- **Pages built for weak signals**: server-rendered HTML with inlined CSS and
  no JavaScript, web fonts or images. The home page is about 8.6 KB, 3.3 KB
  gzipped, enforced by a test budget.
- **Security**: strict Content Security Policy, cross-origin form protection,
  CSRF tokens for responders, bcrypt passwords, hashed session, edit and API
  tokens, rate limits per IP and per phone number, honeypot spam trap,
  `noindex` on every page with a name.
- **Data retention**: records are deleted automatically after
  `RETENTION_DAYS` (180 by default).
- **English and Spanish** interface, chosen from the browser or the footer.
- **Operations**: single static binary, distroless non-root container image
  for `linux/amd64` and `linux/arm64`, Docker Compose setup, health check,
  `/version` endpoint, embedded database migrations, and a CLI for accounts,
  tokens, import, export and purging.

[Unreleased]: https://github.com/lobojoel2003/accountedfor/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/lobojoel2003/accountedfor/releases/tag/v1.0.0
