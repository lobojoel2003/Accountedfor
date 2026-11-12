package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/lobojoel2003/accountedfor/internal/randid"
)

// ErrExists is returned when a unique value is already taken.
var ErrExists = errors.New("already exists")

const userCols = "id, username, display_name, password_hash, role, disabled, created_at, last_login_at"

// NormalizeUsername lower-cases and trims a username.
func NormalizeUsername(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

// ValidUsername reports whether u is 2–40 characters of a-z, 0-9, ".", "-"
// or "_".
func ValidUsername(u string) bool {
	if len(u) < 2 || len(u) > 40 {
		return false
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// CreateUser adds a responder account.
func (s *Store) CreateUser(ctx context.Context, username, displayName, password, role string) (*User, error) {
	username = NormalizeUsername(username)
	if !ValidUsername(username) {
		return nil, fmt.Errorf("username must be 2-40 characters of a-z, 0-9, '.', '-' or '_'")
	}
	if role != RoleAdmin && role != RoleResponder {
		return nil, fmt.Errorf("role must be %q or %q", RoleAdmin, RoleResponder)
	}
	if len(password) < 12 {
		return nil, fmt.Errorf("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $2, $3, $4) RETURNING `+userCols, username, displayName, string(hash), role)
	if err != nil {
		return nil, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[User])
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrExists
	}
	return u, err
}

// CountUsers returns the number of accounts.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&n)
	return n, err
}

// ListUsers returns all accounts ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+userCols+" FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[User])
}

// GetUser returns an account by username.
func (s *Store) GetUser(ctx context.Context, username string) (*User, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+userCols+" FROM users WHERE username = $1", NormalizeUsername(username))
	if err != nil {
		return nil, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[User])
	return u, notFound(err)
}

// dummyHash makes failed logins for unknown users take as long as for
// known ones.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("accountedfor-timing-equalizer"), bcrypt.DefaultCost)

// Authenticate checks a username and password and returns the account.
func (s *Store) Authenticate(ctx context.Context, username, password string) (*User, error) {
	u, err := s.GetUser(ctx, username)
	if errors.Is(err, ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || u.Disabled {
		return nil, ErrNotFound
	}
	_, _ = s.pool.Exec(ctx, "UPDATE users SET last_login_at = now() WHERE id = $1", u.ID)
	return u, nil
}

// SetPassword replaces an account's password and signs it out everywhere.
func (s *Store) SetPassword(ctx context.Context, username, password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, "UPDATE users SET password_hash = $2 WHERE username = $1 RETURNING id",
			NormalizeUsername(username), string(hash)).Scan(&id); err != nil {
			return notFound(err)
		}
		_, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", id)
		return err
	})
}

// SetUserDisabled enables or disables an account. Disabling signs it out.
func (s *Store) SetUserDisabled(ctx context.Context, username string, disabled bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, "UPDATE users SET disabled = $2 WHERE username = $1 RETURNING id",
			NormalizeUsername(username), disabled).Scan(&id); err != nil {
			return notFound(err)
		}
		if disabled {
			if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", id); err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateSession signs a user in and returns the secret session token.
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (token string, err error) {
	token = randid.Token()
	_, err = s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at)
		VALUES ($1, $2, $3, $4)`, randid.Hash(token), userID, randid.Token(), time.Now().Add(ttl))
	return token, err
}

// GetSession returns the session for a token if it is valid.
func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var sess Session
	u := &sess.User
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.username, u.display_name, u.role, u.disabled,
			u.created_at, u.last_login_at, s.csrf_token, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND NOT u.disabled`, randid.Hash(token)).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt, &u.LastLoginAt,
		&sess.CSRFToken, &sess.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &sess, nil
}

// DeleteSession signs a session out.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", randid.Hash(token))
	return err
}

// CreateAPIToken creates a machine token and returns its secret value,
// which is shown once.
func (s *Store) CreateAPIToken(ctx context.Context, name, scope string) (*APIToken, string, error) {
	if scope != "read" && scope != "readwrite" {
		return nil, "", fmt.Errorf("scope must be read or readwrite")
	}
	if strings.TrimSpace(name) == "" {
		return nil, "", fmt.Errorf("token name is required")
	}
	secret := "af_" + randid.Token()
	rows, err := s.pool.Query(ctx, `INSERT INTO api_tokens (name, token_hash, scope) VALUES ($1, $2, $3)
		RETURNING id, name, scope, created_at, last_used_at, revoked_at`, strings.TrimSpace(name), randid.Hash(secret), scope)
	if err != nil {
		return nil, "", err
	}
	t, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[APIToken])
	return t, secret, err
}

// CheckAPIToken returns the token record for a secret if it is valid.
func (s *Store) CheckAPIToken(ctx context.Context, secret string) (*APIToken, error) {
	if secret == "" {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `UPDATE api_tokens SET last_used_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING id, name, scope, created_at, last_used_at, revoked_at`, randid.Hash(secret))
	if err != nil {
		return nil, err
	}
	t, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[APIToken])
	return t, notFound(err)
}

// ListAPITokens returns every token, newest first.
func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, scope, created_at, last_used_at, revoked_at
		FROM api_tokens ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[APIToken])
}

// RevokeAPIToken disables a token.
func (s *Store) RevokeAPIToken(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, "UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Audit appends to the audit log. Failures are returned but callers usually
// only log them.
func (s *Store) Audit(ctx context.Context, actor, action, detail string) error {
	_, err := s.pool.Exec(ctx, "INSERT INTO audit_log (actor, action, detail) VALUES ($1, $2, $3)",
		actor, action, detail)
	return err
}

// ListAudit returns the newest audit entries.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, "SELECT id, at, actor, action, detail FROM audit_log ORDER BY at DESC, id DESC LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[AuditEntry])
}
