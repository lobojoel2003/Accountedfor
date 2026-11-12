-- Accountedfor initial schema.
--
-- People and notes follow the PFIF 1.4 data model so that records can be
-- exchanged with other person-finder systems without loss. Contact details
-- (author_email, author_phone, *_of_found_person) are never shown publicly.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE persons (
    id                bigserial PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE,
    record_id         text        NOT NULL UNIQUE,
    full_name         text        NOT NULL,
    given_name        text        NOT NULL DEFAULT '',
    family_name       text        NOT NULL DEFAULT '',
    alternate_names   text        NOT NULL DEFAULT '',
    search_name       text        NOT NULL,
    description       text        NOT NULL DEFAULT '',
    sex               text        NOT NULL DEFAULT '',
    date_of_birth     text        NOT NULL DEFAULT '',
    age               text        NOT NULL DEFAULT '',
    home_street       text        NOT NULL DEFAULT '',
    home_neighborhood text        NOT NULL DEFAULT '',
    home_city         text        NOT NULL DEFAULT '',
    home_state        text        NOT NULL DEFAULT '',
    home_postal_code  text        NOT NULL DEFAULT '',
    home_country      text        NOT NULL DEFAULT '',
    photo_url         text        NOT NULL DEFAULT '',
    profile_urls      text        NOT NULL DEFAULT '',
    author_name       text        NOT NULL DEFAULT '',
    author_email      text        NOT NULL DEFAULT '',
    author_phone      text        NOT NULL DEFAULT '',
    source_name       text        NOT NULL DEFAULT '',
    source_url        text        NOT NULL DEFAULT '',
    source_date       timestamptz,
    entry_date        timestamptz NOT NULL DEFAULT now(),
    expiry_date       timestamptz,
    -- Latest PFIF note status, maintained by the application.
    pfif_status       text        NOT NULL DEFAULT '',
    status            text        GENERATED ALWAYS AS (
        CASE pfif_status
            WHEN 'is_note_author'     THEN 'safe'
            WHEN 'believed_alive'     THEN 'safe'
            WHEN 'information_sought' THEN 'missing'
            WHEN 'believed_missing'   THEN 'missing'
            WHEN 'believed_dead'      THEN 'deceased'
            ELSE 'unknown'
        END) STORED,
    visibility        text        NOT NULL CHECK (visibility IN ('public', 'limited', 'private')),
    origin            text        NOT NULL CHECK (origin IN ('web', 'sms', 'import', 'responder')),
    hidden            boolean     NOT NULL DEFAULT false,
    edit_token_hash   bytea,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX persons_search_name_trgm ON persons USING gin (search_name gin_trgm_ops);
CREATE INDEX persons_status_updated   ON persons (status, updated_at DESC);
CREATE INDEX persons_updated          ON persons (updated_at);
CREATE INDEX persons_expiry           ON persons (expiry_date) WHERE expiry_date IS NOT NULL;
CREATE INDEX persons_sms_phone        ON persons (author_phone) WHERE origin = 'sms';

CREATE TABLE notes (
    id                      bigserial PRIMARY KEY,
    record_id               text        NOT NULL UNIQUE,
    person_id               bigint      NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    linked_person_record_id text        NOT NULL DEFAULT '',
    status                  text        NOT NULL DEFAULT ''
        CHECK (status IN ('', 'information_sought', 'is_note_author', 'believed_alive', 'believed_missing', 'believed_dead')),
    author_made_contact     boolean     NOT NULL DEFAULT false,
    author_name             text        NOT NULL DEFAULT '',
    author_email            text        NOT NULL DEFAULT '',
    author_phone            text        NOT NULL DEFAULT '',
    email_of_found_person   text        NOT NULL DEFAULT '',
    phone_of_found_person   text        NOT NULL DEFAULT '',
    last_known_location     text        NOT NULL DEFAULT '',
    text                    text        NOT NULL DEFAULT '',
    photo_url               text        NOT NULL DEFAULT '',
    private                 boolean     NOT NULL DEFAULT false,
    hidden                  boolean     NOT NULL DEFAULT false,
    origin                  text        NOT NULL CHECK (origin IN ('web', 'sms', 'import', 'responder')),
    source_date             timestamptz,
    entry_date              timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notes_person ON notes (person_id, entry_date);

-- Deleted and expired records leave a tombstone so PFIF consumers that sync
-- from this server learn about the deletion.
CREATE TABLE tombstones (
    record_id  text PRIMARY KEY,
    deleted_at timestamptz NOT NULL DEFAULT now()
);

-- Possible reunions that a responder reviewed and rejected.
CREATE TABLE match_dismissals (
    missing_id bigint NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    safe_id    bigint NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    PRIMARY KEY (missing_id, safe_id)
);

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    username      text        NOT NULL UNIQUE,
    display_name  text        NOT NULL DEFAULT '',
    password_hash text        NOT NULL,
    role          text        NOT NULL CHECK (role IN ('admin', 'responder')),
    disabled      boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);

CREATE TABLE sessions (
    token_hash bytea       PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE api_tokens (
    id           bigserial PRIMARY KEY,
    name         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    scope        text        NOT NULL CHECK (scope IN ('read', 'readwrite')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

-- Who exported, imported, viewed contact details, hid or deleted what.
CREATE TABLE audit_log (
    id     bigserial PRIMARY KEY,
    at     timestamptz NOT NULL DEFAULT now(),
    actor  text        NOT NULL,
    action text        NOT NULL,
    detail text        NOT NULL DEFAULT ''
);

CREATE INDEX audit_log_at ON audit_log (at DESC);
