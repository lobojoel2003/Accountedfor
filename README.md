<p align="center">
  <img src="docs/images/hero.png" alt="Accountedfor: an “I'm safe” board for disasters, shown on three phones" width="100%">
</p>

<h1 align="center">
  <img src="docs/images/logo.svg" alt="" width="36" height="36"> Accountedfor
</h1>

<p align="center">
  <strong>An open source “I'm safe” board for floods, fires and earthquakes.</strong><br>
  One big button for people in a crisis. A clear list of who is safe and who is missing for responders.
</p>

<p align="center">
  <a href="https://github.com/lobojoel2003/Accountedfor/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/lobojoel2003/Accountedfor/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/lobojoel2003/Accountedfor/actions/workflows/release.yml"><img alt="Release workflow" src="https://github.com/lobojoel2003/Accountedfor/actions/workflows/release.yml/badge.svg"></a>
  <a href="https://github.com/lobojoel2003/Accountedfor/actions/workflows/codeql.yml"><img alt="CodeQL" src="https://github.com/lobojoel2003/Accountedfor/actions/workflows/codeql.yml/badge.svg"></a>
  <a href="https://github.com/lobojoel2003/Accountedfor/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/lobojoel2003/Accountedfor?sort=semver&color=0b6e4f"></a>
  <a href="https://github.com/lobojoel2003/Accountedfor/pkgs/container/accountedfor"><img alt="Container image" src="https://img.shields.io/badge/ghcr.io-accountedfor-0b6e4f?logo=docker&logoColor=white"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/lobojoel2003/Accountedfor"></a>
  <a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#screenshots">Screenshots</a> ·
  <a href="#privacy-model">Privacy</a> ·
  <a href="#sms-for-people-without-data">SMS</a> ·
  <a href="#pfif-import-and-export">PFIF</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#releases-versions-and-build-numbers">Releases</a>
</p>

---

## Why

After a flood, fire or earthquake, families can't reach each other and fill
social media with “has anyone seen…” posts. Responders need a single list of
who is safe and who is missing.

- **Google Person Finder**, the best-known tool, is open source, but its
  repository was [archived on September 17, 2025](https://github.com/google/personfinder)
  and is read-only, with 114 open issues.
- Its [help center](https://support.google.com/personfinder) says each
  crisis's records are taken down months after the crisis.
- NIH's [People Locator](https://irp.nih.gov/accomplishments/disaster-response-tools-that-find-missing-people-and-save-lives)
  was a research system that shared records with Person Finder.

Since the archive there has been no maintained open source tool that a county,
NGO or neighborhood can launch on its own. **Accountedfor** is that tool: a
single binary plus PostgreSQL, running in minutes with Docker, speaking the same
**PFIF** format Person Finder used so records can move between systems.

## Features

| | |
|---|---|
| ✅ **One tap “I'm safe”** | A home page built around one big button. Posting takes under a minute, needs no account, and can include family members who are safe together. |
| 🔒 **Privacy on every post** | Each person chooses *Everyone*, *Name only* or *Responders only*. Phone numbers and emails are never public. Pages are hidden from search engines. Records are deleted automatically after 180 days. |
| 📶 **Built for weak signals** | Server-rendered HTML, CSS inlined, **no JavaScript, web fonts or images**. The home page is **3.4 KB** over the wire, enforced by a test. Nothing to download but the page itself. |
| 💬 **SMS for people without data** | `SAFE Maria Lopez, at the school shelter` · `FIND Maria` · `MISSING Juan` · `DELETE` · `HELP`, in English and Spanish. Twilio or any webhook gateway. |
| 🔁 **PFIF 1.4 import & export** | Exchange records with Person Finder–compatible systems: dashboard upload, CLI, token API, and a public feed. Deletions propagate. |
| 🤝 **Reunions** | Someone who posts “I'm safe” is shown reports looking for a similar name and can answer with one tap. Responders review other possible matches side by side. |
| 🧑‍🚒 **Responder dashboard** | Counts, filters, contact details, bulk entry from shelter lists, CSV export, moderation, accounts and roles, API tokens, and an audit log. |
| 🌍 **English & Español** | Chosen from the browser or the footer; adding a language is one file. |
| 🐳 **Fast setup** | `docker compose up -d`. Distroless multi-arch image (amd64, arm64), embedded migrations, health checks, binaries for Linux, macOS, Windows and Raspberry Pi. |

## Screenshots

<table>
  <tr>
    <td align="center" width="33%"><img src="docs/images/home.png" alt="Home page with a large I'm safe button" width="260"><br><sub><b>Home</b>: one big button, search, SMS instructions</sub></td>
    <td align="center" width="33%"><img src="docs/images/safe-form.png" alt="I'm safe form with three privacy choices" width="260"><br><sub><b>Post</b>: name, message, and who can see it</sub></td>
    <td align="center" width="33%"><img src="docs/images/posted.png" alt="Confirmation page offering a matching missing report" width="260"><br><sub><b>Reunite</b>: “Is someone looking for you?”</sub></td>
  </tr>
  <tr>
    <td align="center"><img src="docs/images/search.png" alt="Search results with safe and missing badges" width="260"><br><sub><b>Search</b>: accent- and typo-tolerant</sub></td>
    <td align="center"><img src="docs/images/person.png" alt="Missing person page with updates" width="260"><br><sub><b>Report</b>: updates from family, teachers, volunteers</sub></td>
    <td align="center"><img src="docs/images/home-es-dark.png" alt="Spanish home page in dark mode" width="260"><br><sub><b>Español</b> and dark mode</sub></td>
  </tr>
</table>

**For responders**

<p align="center"><img src="docs/images/dashboard.png" alt="Responder dashboard with counts, filters and a table of records" width="100%"></p>
<p align="center"><img src="docs/images/matches.png" alt="Possible reunions: a missing report next to a safe post with a similar name" width="100%"></p>

**By text message**

<p align="center"><img src="docs/images/sms.png" alt="SMS conversation: SAFE, SEGURO PRIVADO and FIND commands with replies" width="80%"></p>

> All names, numbers and places in the screenshots are fictional.

## Quick start

You need Docker with Compose v2.

```bash
git clone https://github.com/lobojoel2003/Accountedfor.git
cd Accountedfor
cp .env.example .env
```

Edit `.env`: set `BASE_URL` to the address people will use, a strong
`POSTGRES_PASSWORD`, the `EVENT_NAME`, and a `BOOTSTRAP_ADMIN_PASSWORD`
(12+ characters) for the first responder account. Then:

```bash
docker compose up -d
```

Open <http://localhost:8080>. Responders sign in at `/responders/login` as
`admin` with the bootstrap password. Remove `BOOTSTRAP_ADMIN_PASSWORD` from
`.env` afterwards and create personal accounts in **Accounts**, or with:

```bash
docker compose exec app accountedfor user add alice --admin
```

Load example data (fictional records in Person Finder's PFIF 1.2 format):

```bash
docker compose exec -T app accountedfor import - < examples/person-finder-1.2.xml
```

### Without Docker

Download a binary from the [latest release](https://github.com/lobojoel2003/Accountedfor/releases/latest)
(or `go install github.com/lobojoel2003/accountedfor/cmd/accountedfor@latest`),
point it at PostgreSQL 14+, and run it. Migrations apply automatically.

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/accountedfor?sslmode=disable'
export BASE_URL='https://safe.example.org'
accountedfor serve
```

### In production

Put it behind a TLS-terminating reverse proxy and set `TRUST_PROXY=true`.
With [Caddy](https://caddyserver.com) that is a two-line `Caddyfile`:

```
safe.example.org {
	reverse_proxy localhost:8080
}
```

Back up the database regularly, for example
`docker compose exec db pg_dump -U accountedfor accountedfor > backup.sql`.
The server is light: in local measurements it used about 25 MB of memory and
rendered pages in about a millisecond, so a small virtual server is a
reasonable starting point.

## Privacy model

People choose who sees each post. The choice applies to the web pages, the SMS
`FIND` command and the public PFIF feed alike.

| | Everyone | Name only *(default)* | Responders only |
|---|:---:|:---:|:---:|
| Name and status (“Safe”, “Being looked for”) | Anyone | Anyone | Responders |
| Message, location, age, description | Anyone | Responders | Responders |
| Phone number and email | Responders | Responders | Responders |

More safeguards:

- **No indexing.** Every page with a name is `noindex`, and `robots.txt`
  blocks person pages and search.
- **Retention.** Records are deleted after `RETENTION_DAYS` (default 180). The
  deletion is passed on to PFIF consumers.
- **No public death notices.** Responders can record “believed dead”; the
  public only sees “please contact local responders”.
- **Authors stay in control.** Every post gets a private edit link to update
  or remove it. SMS users text `DELETE`.
- **Accountability.** Sign-ins, exports, imports, links and moderation actions
  go to an audit log that administrators can read.
- **No tracking.** No cookies for the public except an optional language
  choice; no analytics, no third-party requests.

## SMS for people without data

Configure an SMS number and the home page shows “Text SAFE and your name to …”.

| Message | What happens |
|---|---|
| `SAFE Maria Lopez` | Marks Maria safe. Add a message after a comma: `SAFE Maria Lopez, at the school shelter`. Several people: `SAFE Maria Lopez & Juan Lopez`. |
| `SAFE PRIVATE Maria Lopez` | Same, visible to responders only (`PUBLIC` makes it visible to everyone). |
| `FIND Maria Lopez` | Replies with up to three matches and their status. |
| `MISSING Juan Lopez, last seen at the bridge` | Reports someone missing. Responders can call back the sender. |
| `DELETE` | Removes every post sent from that phone. |
| `HELP` (or anything else) | Instructions. |

Spanish keywords work too (`SEGURO`, `ESTOY A SALVO`, `BUSCAR`,
`DESAPARECIDO`, `BORRAR`, `AYUDA`) and the reply comes back in Spanish.
Replies fit in three SMS segments. Senders are rate limited per number.

**Twilio.** Set `TWILIO_AUTH_TOKEN` and point the number's *A message comes in*
webhook to `POST https://your-board/sms/twilio`. Every request's
`X-Twilio-Signature` is verified; replies are returned as TwiML.

**Any other gateway** (Android SMS gateway apps, Vonage, Africa's Talking,
RapidPro…). Set `SMS_WEBHOOK_SECRET` and have the gateway post form or JSON
fields `from` and `body`:

```bash
curl -X POST https://your-board/sms/inbound \
  -H "Authorization: Bearer $SMS_WEBHOOK_SECRET" \
  -d from=+15550100120 -d "body=SAFE Tomás Ruiz, at the stadium shelter"
# {"reply": "Accountedfor: Marked safe: Tomás Ruiz. Visible to: Name only. ..."}
```

Send `Accept: text/plain` to get the reply as plain text.

## PFIF import and export

[PFIF](https://web.archive.org/web/2023/https://zesty.ca/pfif/1.4/) (People
Finder Interchange Format) is the XML format Google Person Finder and other
person-finder systems use. Accountedfor **exports PFIF 1.4** and **imports
PFIF 1.1 to 1.4**, including the older field names. Re-importing updates
records instead of duplicating them, and expired records arriving from another
system delete the local copy.

| Where | Export | Import |
|---|---|---|
| Responder dashboard | **Import & export** → CSV or PFIF | Upload a PFIF file (admins) |
| Command line | `accountedfor export [--since T] [--public] > out.xml` | `accountedfor import file.xml [--visibility limited]` |
| API (token) | `GET /api/v1/pfif?since=2026-11-01T00:00:00Z` | `POST /api/v1/pfif` with a `readwrite` token |
| Public feed | `GET /pfif.xml`, public information only | — |

Create API tokens in **Accounts** or with
`accountedfor token create "Red Cross sync" [--write]`, then:

```bash
curl -H "Authorization: Bearer af_..." "https://your-board/api/v1/pfif?since=2026-11-12T00:00:00Z" > changes.xml
curl -H "Authorization: Bearer af_..." --data-binary @records.xml "https://your-board/api/v1/pfif?visibility=limited"
```

## Configuration

Everything is configured with environment variables; see
[`.env.example`](.env.example) for the full, commented list.

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | — (required) | PostgreSQL connection string |
| `BASE_URL` | `http://localhost:8080` | Public address, used in links, SMS replies and cookies |
| `SITE_NAME` / `EVENT_NAME` | `Accountedfor` / — | Shown on every page |
| `OPERATOR_NAME` / `OPERATOR_CONTACT` | — | Who runs the board (footer, privacy page) |
| `NOTICE` | — | Banner on every page |
| `DEFAULT_LANGUAGE` / `TIMEZONE` | `en` / `UTC` | Interface language and time zone for dates |
| `RETENTION_DAYS` | `180` | Delete records after this many days (`0` = never) |
| `BOOTSTRAP_ADMIN_PASSWORD` | — | Creates the first admin when there are no accounts |
| `SMS_NUMBER` | — | Number shown on the home page |
| `TWILIO_AUTH_TOKEN` / `SMS_WEBHOOK_SECRET` | — | Enable the SMS webhooks |
| `SMS_DEFAULT_VISIBILITY` | `limited` | Visibility of posts made by SMS |
| `PUBLIC_FEED` | `true` | Serve `/pfif.xml` |
| `PFIF_DOMAIN` | host of `BASE_URL` | Domain in PFIF record IDs (do not change later) |
| `TRUST_PROXY` | `false` | Use `X-Forwarded-For` behind a reverse proxy |
| `RATE_LIMIT_POSTS_PER_HOUR` | `120` | Posts per IP per hour (shelters share IPs; keep it generous) |
| `LOG_FORMAT` / `LOG_LEVEL` | `text` / `info` | Logging |

## Command line

```text
accountedfor serve                      Run the web server (default)
accountedfor migrate                    Apply database migrations
accountedfor user add NAME [--admin]    Create an account and print its password
accountedfor user list|passwd|disable|enable NAME
accountedfor token create NAME [--write] / token list / token revoke ID
accountedfor import FILE|- [--visibility public|limited|private]
accountedfor export [--since RFC3339] [--public]
accountedfor purge                      Delete expired records now
accountedfor healthcheck [URL]          Exit 0 if /healthz answers (used by Docker)
accountedfor version                    Version, build number and commit
```

## How it works

```mermaid
flowchart LR
  P["📱 People<br/>(web, no JavaScript)"] -->|HTTPS| S
  T["📟 People without data<br/>(SMS)"] --> G["SMS gateway<br/>Twilio / webhook"] --> S
  R["🧑‍🚒 Responders<br/>(dashboard)"] -->|HTTPS + session| S
  O["🌐 Other systems<br/>(Person Finder, NGOs)"] <-->|"PFIF 1.4 API / feed"| S
  S["Accountedfor<br/>single Go binary"] --> D[("PostgreSQL<br/>pg_trgm search")]
```

- **Go** standard library HTTP server, `html/template`, embedded templates,
  CSS and SQL migrations: one static binary with no runtime dependencies.
- **PostgreSQL** stores people and notes in the PFIF data model. Names are
  normalized (accents, case, punctuation) and searched with trigram
  similarity, which also powers reunion matching.
- Each page view is one HTML response of about 3–4 KB gzipped. The CSS is
  inlined and allowed by a hash in a strict Content Security Policy.

```text
cmd/accountedfor/    CLI and server entry point
internal/web/        HTTP handlers, templates, CSS, middleware, privacy views
internal/store/      PostgreSQL repository and migrations
internal/pfif/       PFIF reader (1.1–1.4) and writer (1.4)
internal/exchange/   Import/export between the store and PFIF, with redaction
internal/sms/        SMS command parser, replies, Twilio signatures
internal/i18n/       English and Spanish catalogs
examples/            Sample PFIF file
```

### Page weight

Measured with `curl` against the server (the test suite fails if the home page
grows past 12 KB, or 5 KB gzipped):

| Page | HTML | Over the wire (gzip) | Requests |
|---|---:|---:|---:|
| Home | 8.8 KB | 3.4 KB | 1 |
| “I'm safe” form | 9.5 KB | 3.5 KB | 1 |
| Search results | 8.5 KB | 3.3 KB | 1 |
| Person with updates | 10.6 KB | 4.0 KB | 1 |

## Security

Strict CSP (`default-src 'none'`), cross-origin form protection plus CSRF
tokens for responders, bcrypt passwords, hashed session/edit/API tokens,
verified Twilio webhooks, rate limits per IP and per phone number, a honeypot
for bots, `no-store` caching for shared shelter devices, CSV export hardened
against formula injection, and a non-root distroless container. CodeQL,
`govulncheck` and `staticcheck` run in CI. Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).

## Releases, versions and build numbers

Accountedfor follows [Semantic Versioning](https://semver.org). Every release
is a git tag `vMAJOR.MINOR.PATCH`, documented in [CHANGELOG.md](CHANGELOG.md).

Each binary and image is stamped with its **version**, **build number** (the
GitHub Actions run that built it), **commit** and **build date**. You can see
them in the page footer, at `/version`, and with `accountedfor version`:

```text
$ accountedfor version
accountedfor v1.0.0 (build 12, 3f1c2ab, 2026-11-12)   # example output
```

| Container tag | Meaning |
|---|---|
| `1.0.0`, `1.0`, `1`, `latest` | Releases (multi-arch: `linux/amd64`, `linux/arm64`) |
| `build-<n>` | The exact CI build `<n>` |
| `edge`, `sha-<commit>` | Latest commit on `main` (continuous delivery) |

### CI/CD pipeline

```mermaid
flowchart LR
  A[push / pull request] --> L[Lint<br/>gofmt · vet · staticcheck · shellcheck]
  A --> T[Tests on Go stable + oldstable<br/>with PostgreSQL, race detector,<br/>coverage, page-weight budget]
  A --> V[govulncheck]
  A --> Q[CodeQL]
  L & T --> B[Cross-compile 7 platforms]
  L & T --> D[Docker build +<br/>Compose smoke test]
  B & D & V -->|main only| E[Publish ghcr.io …:edge]
  R[push tag v* or<br/>Run workflow] --> RV[Verify tag + changelog + tests] --> RB[Binaries + SHA256SUMS<br/>+ provenance] & RI[Multi-arch image<br/>+ provenance] --> GR[GitHub Release<br/>notes from CHANGELOG]
```

To cut a release, move the `[Unreleased]` notes in `CHANGELOG.md` under a new
version heading and commit to `main`. Then either push a tag:

```bash
git tag -a v1.1.0 -m "Accountedfor v1.1.0"
git push origin v1.1.0
```

or open **Actions → Release → Run workflow** on `main` and enter `v1.1.0`; the
workflow creates the tag on the commit it built and tested.

Verify a downloaded artifact with `sha256sum -c SHA256SUMS` or
`gh attestation verify <file> --repo lobojoel2003/Accountedfor`.

## Development

```bash
make help        # list targets
make run         # build and run (needs DATABASE_URL)
make test        # unit tests
make test-db     # all tests incl. PostgreSQL integration (TEST_DATABASE_URL=...)
make lint        # gofmt, vet, staticcheck, go mod tidy
make dist        # cross-compile release archives into dist/
make docker      # build the container image
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the project's principles, how to
add a language and how releases work.

## License

[Apache License 2.0](LICENSE).

PFIF was designed by Ka-Ping Yee and the Person Finder community. Accountedfor
is an independent project and is not affiliated with Google or the NIH.
