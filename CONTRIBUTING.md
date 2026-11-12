# Contributing to Accountedfor

Thank you for helping. Accountedfor is used when networks are failing and
people are frightened, so every change is judged by three questions:

1. **Does it keep pages tiny?** The home page has a size budget enforced by a
   test (12 KB raw, 5 KB gzipped). No JavaScript, web fonts or images.
2. **Does it respect each person's privacy choice?** Contact details are never
   public. New fields must go through `internal/web/views.go` and
   `internal/exchange` redaction.
3. **Is it plain?** Copy is short sentences in everyday words, in every
   supported language.

## Development setup

You need Go (see `go.mod`) and PostgreSQL 14 or newer.

```bash
# Start a database (or use your own)
docker run -d --name af-db -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine

# Run the server
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
export BOOTSTRAP_ADMIN_PASSWORD='choose-a-long-password'
make run

# Run every test, including database integration tests
make test-db
make lint
```

## Pull requests

- Keep each pull request focused; add or update tests.
- Add a line to the `[Unreleased]` section of `CHANGELOG.md`.
- New user-facing text needs a key in `internal/i18n/en.go` **and** every other
  catalog; `go test ./internal/i18n` fails otherwise.
- Database changes are new files in `internal/store/migrations/`
  (`002_description.sql`, ...). Never edit a migration that has been released.

## Adding a language

Copy `internal/i18n/es.go` to a new file, translate every value, register it in
`catalogs` and `Languages` in `internal/i18n/i18n.go`, and run the tests.
Translations by native speakers who know the local emergency vocabulary are
especially welcome.

## Releases

Maintainers release by moving the `[Unreleased]` notes in `CHANGELOG.md` to a
new version section, committing, and pushing a tag:

```bash
git tag -a v1.1.0 -m "Accountedfor v1.1.0"
git push origin v1.1.0
```

The release workflow tests the tag, builds binaries and the container image
(stamped with the version and the CI build number), attests their
provenance and publishes the GitHub Release with the changelog notes.
