package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lobojoel2003/accountedfor/internal/randid"
	"github.com/lobojoel2003/accountedfor/internal/textutil"
)

// ErrTombstoned is returned when an import tries to recreate a record that
// was deleted on this server.
var ErrTombstoned = errors.New("record was deleted")

const personCols = `id, public_id, record_id, full_name, given_name, family_name, alternate_names,
	description, sex, date_of_birth, age, home_street, home_neighborhood, home_city, home_state,
	home_postal_code, home_country, photo_url, profile_urls, author_name, author_email, author_phone,
	source_name, source_url, source_date, entry_date, expiry_date, pfif_status, status, visibility,
	origin, hidden, created_at, updated_at`

const noteCols = `id, record_id, person_id, linked_person_record_id, status, author_made_contact,
	author_name, author_email, author_phone, email_of_found_person, phone_of_found_person,
	last_known_location, text, photo_url, private, hidden, origin, source_date, entry_date`

// Domain is the PFIF domain used for record IDs created by this server.
// Callers set it once at startup.
var Domain = "localhost"

// NewPersonRecordID returns a fresh public ID and matching PFIF record ID.
func NewPersonRecordID() (publicID, recordID string) {
	publicID = randid.PublicID()
	return publicID, Domain + "/person." + publicID
}

// NewNoteRecordID returns a fresh PFIF note record ID.
func NewNoteRecordID() string { return Domain + "/note." + randid.Short(12) }

// IsLocalRecord reports whether a PFIF record ID was minted by this server.
func IsLocalRecord(recordID string) bool { return strings.HasPrefix(recordID, Domain+"/") }

func searchKey(p *Person) string {
	return textutil.NormalizeName(p.FullName + " " + p.AlternateNames)
}

// CreatePerson inserts a person together with its first notes. Missing
// identifiers and dates are filled in. When editToken is not empty its hash
// is stored so the author can later change or remove the post.
func (s *Store) CreatePerson(ctx context.Context, p *Person, notes []Note, editToken string) error {
	if p.PublicID == "" {
		p.PublicID, p.RecordID = NewPersonRecordID()
	}
	if p.RecordID == "" {
		p.RecordID = Domain + "/person." + p.PublicID
	}
	if p.EntryDate.IsZero() {
		p.EntryDate = time.Now().UTC()
	}
	if !p.Visibility.Valid() {
		return fmt.Errorf("invalid visibility %q", p.Visibility)
	}
	var tokenHash []byte
	if editToken != "" {
		tokenHash = randid.Hash(editToken)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO persons (
			public_id, record_id, full_name, given_name, family_name, alternate_names, search_name,
			description, sex, date_of_birth, age, home_street, home_neighborhood, home_city, home_state,
			home_postal_code, home_country, photo_url, profile_urls, author_name, author_email,
			author_phone, source_name, source_url, source_date, entry_date, expiry_date, visibility,
			origin, hidden, edit_token_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,
			$24,$25,$26,$27,$28,$29,$30,$31)
		RETURNING id, created_at, updated_at`,
			p.PublicID, p.RecordID, p.FullName, p.GivenName, p.FamilyName, p.AlternateNames, searchKey(p),
			p.Description, p.Sex, p.DateOfBirth, p.Age, p.HomeStreet, p.HomeNeighborhood, p.HomeCity,
			p.HomeState, p.HomePostalCode, p.HomeCountry, p.PhotoURL, p.ProfileURLs, p.AuthorName,
			p.AuthorEmail, p.AuthorPhone, p.SourceName, p.SourceURL, p.SourceDate, p.EntryDate,
			p.ExpiryDate, p.Visibility, p.Origin, p.Hidden, tokenHash,
		).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert person: %w", err)
		}
		for i := range notes {
			notes[i].PersonID = p.ID
			if _, err := insertNote(ctx, tx, &notes[i], false); err != nil {
				return err
			}
		}
		p.Notes = notes
		return recomputeStatus(ctx, tx, p.ID, &p.PFIFStatus)
	})
}

// insertNote stores n and reports whether a row was written (false when
// ignoreDuplicate skipped an existing record ID).
func insertNote(ctx context.Context, tx pgx.Tx, n *Note, ignoreDuplicate bool) (bool, error) {
	if n.RecordID == "" {
		n.RecordID = NewNoteRecordID()
	}
	if n.EntryDate.IsZero() {
		n.EntryDate = time.Now().UTC()
	}
	if !ValidNoteStatus(n.Status) {
		n.Status = StatusNone
	}
	conflict := ""
	if ignoreDuplicate {
		conflict = " ON CONFLICT (record_id) DO NOTHING"
	}
	err := tx.QueryRow(ctx, `INSERT INTO notes (
		record_id, person_id, linked_person_record_id, status, author_made_contact, author_name,
		author_email, author_phone, email_of_found_person, phone_of_found_person,
		last_known_location, text, photo_url, private, hidden, origin, source_date, entry_date)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`+conflict+` RETURNING id`,
		n.RecordID, n.PersonID, n.LinkedPersonRecordID, n.Status, n.AuthorMadeContact, n.AuthorName,
		n.AuthorEmail, n.AuthorPhone, n.EmailOfFoundPerson, n.PhoneOfFoundPerson,
		n.LastKnownLocation, n.Text, n.PhotoURL, n.Private, n.Hidden, n.Origin, n.SourceDate, n.EntryDate,
	).Scan(&n.ID)
	if ignoreDuplicate && errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert note: %w", err)
	}
	return true, nil
}

func recomputeStatus(ctx context.Context, tx pgx.Tx, personID int64, out *string) error {
	var status string
	err := tx.QueryRow(ctx, `UPDATE persons SET pfif_status = COALESCE((
			SELECT status FROM notes
			WHERE person_id = $1 AND NOT hidden AND status <> ''
			ORDER BY COALESCE(source_date, entry_date) DESC, id DESC LIMIT 1), ''),
		updated_at = now()
		WHERE id = $1 RETURNING pfif_status`, personID).Scan(&status)
	if err != nil {
		return notFound(err)
	}
	if out != nil {
		*out = status
	}
	return nil
}

func (s *Store) getPerson(ctx context.Context, where string, arg any) (*Person, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+personCols+" FROM persons WHERE "+where, arg)
	if err != nil {
		return nil, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByNameLax[Person])
	return p, notFound(err)
}

// GetPerson returns a person by public ID.
func (s *Store) GetPerson(ctx context.Context, publicID string) (*Person, error) {
	return s.getPerson(ctx, "public_id = $1", publicID)
}

// GetPersonByID returns a person by database ID.
func (s *Store) GetPersonByID(ctx context.Context, id int64) (*Person, error) {
	return s.getPerson(ctx, "id = $1", id)
}

// GetPersonByRecordID returns a person by PFIF record ID.
func (s *Store) GetPersonByRecordID(ctx context.Context, recordID string) (*Person, error) {
	return s.getPerson(ctx, "record_id = $1", recordID)
}

// PersonsByRecordIDs returns the people with the given PFIF record IDs keyed
// by record ID. Unknown IDs are skipped.
func (s *Store) PersonsByRecordIDs(ctx context.Context, ids []string) (map[string]*Person, error) {
	out := map[string]*Person{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT "+personCols+" FROM persons WHERE record_id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	people, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByNameLax[Person])
	if err != nil {
		return nil, err
	}
	for _, p := range people {
		out[p.RecordID] = p
	}
	return out, nil
}

func (s *Store) personsByIDs(ctx context.Context, ids []int64) (map[int64]*Person, error) {
	out := map[int64]*Person{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT "+personCols+" FROM persons WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	people, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByNameLax[Person])
	if err != nil {
		return nil, err
	}
	for _, p := range people {
		out[p.ID] = p
	}
	return out, nil
}

// VerifyEditToken returns the person when token is the record's edit token.
func (s *Store) VerifyEditToken(ctx context.Context, publicID, token string) (*Person, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var hash []byte
	err := s.pool.QueryRow(ctx, "SELECT edit_token_hash FROM persons WHERE public_id = $1", publicID).Scan(&hash)
	if err != nil {
		return nil, notFound(err)
	}
	if len(hash) == 0 || subtle.ConstantTimeCompare(hash, randid.Hash(token)) != 1 {
		return nil, ErrNotFound
	}
	return s.GetPerson(ctx, publicID)
}

// LoadNotes fills p.Notes in chronological order.
func (s *Store) LoadNotes(ctx context.Context, p *Person, includeHidden bool) error {
	notes, err := s.notesFor(ctx, []int64{p.ID}, includeHidden)
	if err != nil {
		return err
	}
	p.Notes = notes[p.ID]
	return nil
}

func (s *Store) notesFor(ctx context.Context, personIDs []int64, includeHidden bool) (map[int64][]Note, error) {
	q := "SELECT " + noteCols + " FROM notes WHERE person_id = ANY($1)"
	if !includeHidden {
		q += " AND NOT hidden"
	}
	q += " ORDER BY COALESCE(source_date, entry_date), id"
	rows, err := s.pool.Query(ctx, q, personIDs)
	if err != nil {
		return nil, err
	}
	notes, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[Note])
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]Note, len(personIDs))
	for _, n := range notes {
		out[n.PersonID] = append(out[n.PersonID], n)
	}
	return out, nil
}

// LatestMessages returns, per person, the newest note that is visible to
// the given audience. It is used to show a one-line message in lists.
func (s *Store) LatestMessages(ctx context.Context, personIDs []int64, includePrivate bool) (map[int64]Note, error) {
	out := map[int64]Note{}
	if len(personIDs) == 0 {
		return out, nil
	}
	q := `SELECT DISTINCT ON (person_id) ` + noteCols + ` FROM notes
		WHERE person_id = ANY($1) AND NOT hidden AND (text <> '' OR last_known_location <> '')`
	if !includePrivate {
		q += " AND NOT private"
	}
	q += " ORDER BY person_id, COALESCE(source_date, entry_date) DESC, id DESC"
	rows, err := s.pool.Query(ctx, q, personIDs)
	if err != nil {
		return nil, err
	}
	notes, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[Note])
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		out[n.PersonID] = n
	}
	return out, nil
}

// AddNote appends a note to a person and updates the person's status.
func (s *Store) AddNote(ctx context.Context, personID int64, n *Note) error {
	n.PersonID = personID
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := insertNote(ctx, tx, n, false); err != nil {
			return err
		}
		return recomputeStatus(ctx, tx, personID, nil)
	})
}

// UpdatePerson changes the name and visibility of a record.
func (s *Store) UpdatePerson(ctx context.Context, p *Person) error {
	if !p.Visibility.Valid() {
		return fmt.Errorf("invalid visibility %q", p.Visibility)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE persons SET full_name = $2, search_name = $3,
		visibility = $4, description = $5, updated_at = now() WHERE id = $1`,
		p.ID, p.FullName, searchKey(p), p.Visibility, p.Description)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPersonHidden hides or shows a record (moderation).
func (s *Store) SetPersonHidden(ctx context.Context, id int64, hidden bool) error {
	tag, err := s.pool.Exec(ctx, "UPDATE persons SET hidden = $2, updated_at = now() WHERE id = $1", id, hidden)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNoteHidden hides or shows a note and returns its person's ID.
func (s *Store) SetNoteHidden(ctx context.Context, noteID int64, hidden bool) (int64, error) {
	var personID int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "UPDATE notes SET hidden = $2 WHERE id = $1 RETURNING person_id",
			noteID, hidden).Scan(&personID); err != nil {
			return notFound(err)
		}
		return recomputeStatus(ctx, tx, personID, nil)
	})
	return personID, err
}

// DeletePerson removes a record and its notes and leaves a tombstone so
// PFIF consumers learn about the deletion.
func (s *Store) DeletePerson(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var recordID string
		if err := tx.QueryRow(ctx, "DELETE FROM persons WHERE id = $1 RETURNING record_id", id).Scan(&recordID); err != nil {
			return notFound(err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO tombstones (record_id) VALUES ($1)
			ON CONFLICT (record_id) DO UPDATE SET deleted_at = now()`, recordID)
		return err
	})
}

// DeleteSMSPosts removes every record posted by SMS from phone and returns
// how many were removed.
func (s *Store) DeleteSMSPosts(ctx context.Context, phone string) (int, error) {
	if phone == "" {
		return 0, nil
	}
	var n int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `DELETE FROM persons WHERE origin = 'sms' AND author_phone = $1
			RETURNING record_id`, phone)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		n = len(ids)
		if n == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO tombstones (record_id) SELECT unnest($1::text[])
			ON CONFLICT (record_id) DO UPDATE SET deleted_at = now()`, ids)
		return err
	})
	return n, err
}

// SearchOptions narrows a name search.
type SearchOptions struct {
	// IncludePrivate also returns records hidden from the public. Only
	// responders may set it.
	IncludePrivate bool
	// IncludeHidden also returns moderated records.
	IncludeHidden bool
	// Status limits results to "safe", "missing", "deceased" or "unknown".
	Status string
	Limit  int
	Offset int
}

// Search finds people whose name resembles query, best matches first.
func (s *Store) Search(ctx context.Context, query string, opt SearchOptions) ([]Person, error) {
	key := textutil.NormalizeName(query)
	if key == "" {
		return nil, nil
	}
	if opt.Limit <= 0 || opt.Limit > 200 {
		opt.Limit = 50
	}
	var where []string
	args := []any{key}
	where = append(where, "(search_name LIKE '%' || $1 || '%' OR $1 <% search_name)")
	if !opt.IncludePrivate {
		where = append(where, "visibility <> 'private'")
	}
	if !opt.IncludeHidden {
		where = append(where, "NOT hidden")
	}
	if opt.Status != "" {
		args = append(args, opt.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	args = append(args, opt.Limit, opt.Offset)
	q := "SELECT " + personCols + " FROM persons WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(` ORDER BY (search_name = $1) DESC, (search_name LIKE $1 || '%%') DESC,
			word_similarity($1, search_name) DESC, updated_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByNameLax[Person])
}

// Name similarity thresholds (pg_trgm similarity, 0–1). "maria lopez" and
// "maria lopes" score 0.71; "maria lopez" and "ana lopez" score 0.38.
const (
	// SelfMatchThreshold decides which missing reports someone who just
	// posted "I'm safe" is asked about.
	SelfMatchThreshold = 0.5
	// ReviewMatchThreshold decides which pairs responders review. It is
	// lower because a person checks each pair.
	ReviewMatchThreshold = 0.45
)

// SimilarMissing returns missing-person reports whose name resembles name.
// They are offered to someone who just marked themselves safe ("Is someone
// looking for you?"). Private records are never returned.
func (s *Store) SimilarMissing(ctx context.Context, name string, excludeID int64, limit int) ([]Person, error) {
	key := textutil.NormalizeName(name)
	if key == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT "+personCols+` FROM persons
		WHERE status = 'missing' AND NOT hidden AND visibility <> 'private' AND id <> $2
		  AND (search_name = $1 OR (search_name % $1 AND similarity(search_name, $1) >= $4))
		ORDER BY similarity(search_name, $1) DESC, updated_at DESC LIMIT $3`, key, excludeID, limit, SelfMatchThreshold)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByNameLax[Person])
}

// ListOptions filters the responder list.
type ListOptions struct {
	Query      string
	Status     string
	Origin     string
	Visibility string
	HiddenOnly bool
	Limit      int
	Offset     int
}

// List returns records for responders, newest first, and the total count.
func (s *Store) List(ctx context.Context, opt ListOptions) ([]Person, int, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 50
	}
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	order := "updated_at DESC, id DESC"
	if key := textutil.NormalizeName(opt.Query); key != "" {
		add("(search_name LIKE '%%' || $%[1]d || '%%' OR $%[1]d <%% search_name)", key)
		order = fmt.Sprintf("word_similarity($%d, search_name) DESC, updated_at DESC", len(args))
	}
	if opt.Status != "" {
		add("status = $%d", opt.Status)
	}
	if opt.Origin != "" {
		add("origin = $%d", opt.Origin)
	}
	if opt.Visibility != "" {
		add("visibility = $%d", opt.Visibility)
	}
	if opt.HiddenOnly {
		where = append(where, "hidden")
	}
	cond := "TRUE"
	if len(where) > 0 {
		cond = strings.Join(where, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM persons WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, opt.Limit, opt.Offset)
	q := fmt.Sprintf("SELECT %s FROM persons WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d",
		personCols, cond, order, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	people, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[Person])
	return people, total, err
}

// Stats counts records. Public statistics exclude private and hidden
// records.
func (s *Store) Stats(ctx context.Context, public bool) (Stats, error) {
	cond := "TRUE"
	if public {
		cond = "NOT hidden AND visibility <> 'private'"
	}
	var st Stats
	err := s.pool.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE NOT hidden),
			count(*) FILTER (WHERE status = 'safe' AND NOT hidden),
			count(*) FILTER (WHERE status = 'missing' AND NOT hidden),
			count(*) FILTER (WHERE status = 'deceased' AND NOT hidden),
			count(*) FILTER (WHERE status = 'unknown' AND NOT hidden),
			count(*) FILTER (WHERE created_at > now() - interval '24 hours' AND NOT hidden),
			count(*) FILTER (WHERE hidden),
			count(*) FILTER (WHERE origin = 'sms'),
			count(*) FILTER (WHERE origin = 'import')
		FROM persons WHERE `+cond).Scan(
		&st.Total, &st.Safe, &st.Missing, &st.Deceased, &st.Unknown, &st.Last24h, &st.Hidden, &st.BySMS, &st.Imported)
	return st, err
}

// Matches lists possible reunions: missing reports paired with safe reports
// that have a similar name and have not been dismissed.
func (s *Store) Matches(ctx context.Context, limit int) ([]Match, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT m.id, c.id, c.sim FROM persons m
		CROSS JOIN LATERAL (
			SELECT s.id, similarity(s.search_name, m.search_name) AS sim FROM persons s
			WHERE s.status = 'safe' AND NOT s.hidden AND s.id <> m.id
			  AND s.search_name % m.search_name
			  AND similarity(s.search_name, m.search_name) >= $2
			  AND NOT EXISTS (SELECT 1 FROM match_dismissals d WHERE d.missing_id = m.id AND d.safe_id = s.id)
			ORDER BY sim DESC LIMIT 3
		) c
		WHERE m.status = 'missing' AND NOT m.hidden
		ORDER BY c.sim DESC, m.updated_at DESC LIMIT $1`, limit, ReviewMatchThreshold)
	if err != nil {
		return nil, err
	}
	type pair struct {
		missing, safe int64
		sim           float64
	}
	var pairs []pair
	var ids []int64
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.missing, &p.safe, &p.sim); err != nil {
			rows.Close()
			return nil, err
		}
		pairs = append(pairs, p)
		ids = append(ids, p.missing, p.safe)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	people, err := s.personsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Match, 0, len(pairs))
	for _, p := range pairs {
		m, sf := people[p.missing], people[p.safe]
		if m == nil || sf == nil {
			continue
		}
		out = append(out, Match{Missing: *m, Safe: *sf, Similarity: p.sim})
	}
	return out, nil
}

// DismissMatch records that a responder decided two records are different
// people.
func (s *Store) DismissMatch(ctx context.Context, missingID, safeID int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO match_dismissals (missing_id, safe_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, missingID, safeID)
	return err
}

// PurgeExpired deletes records past their expiry date, old tombstones and
// expired sessions. It returns the number of records deleted.
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "DELETE FROM persons WHERE expiry_date < $1 RETURNING record_id", now)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		n = len(ids)
		if n > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO tombstones (record_id, deleted_at)
				SELECT unnest($1::text[]), $2 ON CONFLICT (record_id) DO UPDATE SET deleted_at = $2`, ids, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM tombstones WHERE deleted_at < $1", now.Add(-90*24*time.Hour)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM sessions WHERE expires_at < $1", now)
		return err
	})
	return n, err
}

// Tombstone is a deleted record ID.
type Tombstone struct {
	RecordID  string
	DeletedAt time.Time
}

// Tombstones returns deletions at or after since.
func (s *Store) Tombstones(ctx context.Context, since time.Time) ([]Tombstone, error) {
	rows, err := s.pool.Query(ctx, `SELECT record_id, deleted_at FROM tombstones
		WHERE deleted_at >= $1 ORDER BY deleted_at, record_id`, since)
	if err != nil {
		return nil, err
	}
	var out []Tombstone
	for rows.Next() {
		var t Tombstone
		if err := rows.Scan(&t.RecordID, &t.DeletedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ExportFilter selects which records Each visits.
type ExportFilter struct {
	Since          time.Time
	IncludePrivate bool
	IncludeHidden  bool
}

// Each calls fn for every record changed at or after f.Since, oldest change
// first, with notes loaded. It pages through the table so memory use stays
// flat for large exports.
func (s *Store) Each(ctx context.Context, f ExportFilter, fn func(*Person) error) error {
	const page = 500
	filter := ""
	if !f.IncludePrivate {
		filter += " AND visibility <> 'private'"
	}
	if !f.IncludeHidden {
		filter += " AND NOT hidden"
	}
	cursorTime := f.Since
	var cursorID int64
	first := true
	for {
		// The first page includes records updated exactly at Since; later
		// pages continue strictly after the last row seen.
		cond := "(updated_at, id) > ($1, $2)"
		if first {
			cond = "updated_at >= $1 AND $2::bigint IS NOT NULL"
			first = false
		}
		q := "SELECT " + personCols + " FROM persons WHERE " + cond + filter + " ORDER BY updated_at, id LIMIT $3"
		args := []any{cursorTime, cursorID, page}
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		people, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[Person])
		if err != nil {
			return err
		}
		if len(people) == 0 {
			return nil
		}
		ids := make([]int64, len(people))
		for i := range people {
			ids[i] = people[i].ID
		}
		notes, err := s.notesFor(ctx, ids, f.IncludeHidden)
		if err != nil {
			return err
		}
		for i := range people {
			people[i].Notes = notes[people[i].ID]
			if err := fn(&people[i]); err != nil {
				return err
			}
		}
		last := people[len(people)-1]
		cursorTime, cursorID = last.UpdatedAt, last.ID
		if len(people) < page {
			return nil
		}
	}
}

// UpsertImported stores a record received from another system. New records
// get a local public ID; existing records have their PFIF fields refreshed
// and new notes appended. Visibility and moderation state of existing
// records are kept. It reports whether the record was created.
func (s *Store) UpsertImported(ctx context.Context, p *Person, notes []Note) (created bool, notesAdded int, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var deleted bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM tombstones WHERE record_id = $1)",
			p.RecordID).Scan(&deleted); err != nil {
			return err
		}
		if deleted {
			return ErrTombstoned
		}
		var id int64
		err := tx.QueryRow(ctx, "SELECT id FROM persons WHERE record_id = $1 FOR UPDATE", p.RecordID).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			created = true
			p.PublicID = randid.PublicID()
			if p.EntryDate.IsZero() {
				p.EntryDate = time.Now().UTC()
			}
			err = tx.QueryRow(ctx, `INSERT INTO persons (
				public_id, record_id, full_name, given_name, family_name, alternate_names, search_name,
				description, sex, date_of_birth, age, home_street, home_neighborhood, home_city,
				home_state, home_postal_code, home_country, photo_url, profile_urls, author_name,
				author_email, author_phone, source_name, source_url, source_date, entry_date,
				expiry_date, visibility, origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,
				$23,$24,$25,$26,$27,$28,'import') RETURNING id`,
				p.PublicID, p.RecordID, p.FullName, p.GivenName, p.FamilyName, p.AlternateNames,
				searchKey(p), p.Description, p.Sex, p.DateOfBirth, p.Age, p.HomeStreet,
				p.HomeNeighborhood, p.HomeCity, p.HomeState, p.HomePostalCode, p.HomeCountry,
				p.PhotoURL, p.ProfileURLs, p.AuthorName, p.AuthorEmail, p.AuthorPhone, p.SourceName,
				p.SourceURL, p.SourceDate, p.EntryDate, p.ExpiryDate, p.Visibility).Scan(&id)
			if err != nil {
				return fmt.Errorf("insert imported person: %w", err)
			}
		case err != nil:
			return err
		default:
			_, err = tx.Exec(ctx, `UPDATE persons SET full_name = $2, given_name = $3,
				family_name = $4, alternate_names = $5, search_name = $6, description = $7, sex = $8,
				date_of_birth = $9, age = $10, home_street = $11, home_neighborhood = $12,
				home_city = $13, home_state = $14, home_postal_code = $15, home_country = $16,
				photo_url = $17, profile_urls = $18, author_name = $19, author_email = $20,
				author_phone = $21, source_name = $22, source_url = $23, source_date = $24,
				expiry_date = $25, updated_at = now()
			WHERE id = $1`,
				id, p.FullName, p.GivenName, p.FamilyName, p.AlternateNames, searchKey(p),
				p.Description, p.Sex, p.DateOfBirth, p.Age, p.HomeStreet, p.HomeNeighborhood,
				p.HomeCity, p.HomeState, p.HomePostalCode, p.HomeCountry, p.PhotoURL, p.ProfileURLs,
				p.AuthorName, p.AuthorEmail, p.AuthorPhone, p.SourceName, p.SourceURL, p.SourceDate,
				p.ExpiryDate)
			if err != nil {
				return fmt.Errorf("update imported person: %w", err)
			}
		}
		p.ID = id
		for i := range notes {
			notes[i].PersonID = id
			added, err := insertNote(ctx, tx, &notes[i], true)
			if err != nil {
				return err
			}
			if added {
				notesAdded++
			}
		}
		return recomputeStatus(ctx, tx, id, &p.PFIFStatus)
	})
	return created, notesAdded, err
}

// DeleteByRecordID removes a record named by PFIF record ID, for example
// when an import says it has expired. It reports whether a record existed.
func (s *Store) DeleteByRecordID(ctx context.Context, recordID string) (bool, error) {
	p, err := s.GetPersonByRecordID(ctx, recordID)
	if errors.Is(err, ErrNotFound) {
		_, err = s.pool.Exec(ctx, `INSERT INTO tombstones (record_id) VALUES ($1)
			ON CONFLICT (record_id) DO NOTHING`, recordID)
		return false, err
	}
	if err != nil {
		return false, err
	}
	return true, s.DeletePerson(ctx, p.ID)
}

// AppendNotes adds notes to a person, skipping notes whose record ID is
// already stored, and returns how many were added.
func (s *Store) AppendNotes(ctx context.Context, personID int64, notes []Note) (int, error) {
	if len(notes) == 0 {
		return 0, nil
	}
	added := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i := range notes {
			notes[i].PersonID = personID
			ok, err := insertNote(ctx, tx, &notes[i], true)
			if err != nil {
				return err
			}
			if ok {
				added++
			}
		}
		return recomputeStatus(ctx, tx, personID, nil)
	})
	return added, err
}
