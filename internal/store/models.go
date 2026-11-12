package store

import "time"

// Visibility controls who can see a person record.
type Visibility string

const (
	// Public records show the name, status and messages to anyone.
	Public Visibility = "public"
	// Limited records show only the name and status publicly; messages and
	// locations are for responders.
	Limited Visibility = "limited"
	// Private records are hidden from the public entirely.
	Private Visibility = "private"
)

// Valid reports whether v is a known visibility.
func (v Visibility) Valid() bool { return v == Public || v == Limited || v == Private }

// ParseVisibility returns v as a Visibility, or def when it is unknown.
func ParseVisibility(v string, def Visibility) Visibility {
	if vis := Visibility(v); vis.Valid() {
		return vis
	}
	return def
}

// Origin records how a record entered the system.
const (
	OriginWeb       = "web"
	OriginSMS       = "sms"
	OriginImport    = "import"
	OriginResponder = "responder"
)

// PFIF note statuses.
const (
	StatusNone              = ""
	StatusInformationSought = "information_sought"
	StatusIsNoteAuthor      = "is_note_author"
	StatusBelievedAlive     = "believed_alive"
	StatusBelievedMissing   = "believed_missing"
	StatusBelievedDead      = "believed_dead"
)

// ValidNoteStatus reports whether s is a PFIF 1.4 note status.
func ValidNoteStatus(s string) bool {
	switch s {
	case StatusNone, StatusInformationSought, StatusIsNoteAuthor, StatusBelievedAlive,
		StatusBelievedMissing, StatusBelievedDead:
		return true
	}
	return false
}

// Category groups PFIF statuses into what the board shows.
func Category(pfifStatus string) string {
	switch pfifStatus {
	case StatusIsNoteAuthor, StatusBelievedAlive:
		return "safe"
	case StatusInformationSought, StatusBelievedMissing:
		return "missing"
	case StatusBelievedDead:
		return "deceased"
	}
	return "unknown"
}

// Person is a PFIF person record plus Accountedfor's privacy settings.
type Person struct {
	ID               int64      `db:"id"`
	PublicID         string     `db:"public_id"`
	RecordID         string     `db:"record_id"`
	FullName         string     `db:"full_name"`
	GivenName        string     `db:"given_name"`
	FamilyName       string     `db:"family_name"`
	AlternateNames   string     `db:"alternate_names"`
	Description      string     `db:"description"`
	Sex              string     `db:"sex"`
	DateOfBirth      string     `db:"date_of_birth"`
	Age              string     `db:"age"`
	HomeStreet       string     `db:"home_street"`
	HomeNeighborhood string     `db:"home_neighborhood"`
	HomeCity         string     `db:"home_city"`
	HomeState        string     `db:"home_state"`
	HomePostalCode   string     `db:"home_postal_code"`
	HomeCountry      string     `db:"home_country"`
	PhotoURL         string     `db:"photo_url"`
	ProfileURLs      string     `db:"profile_urls"`
	AuthorName       string     `db:"author_name"`
	AuthorEmail      string     `db:"author_email"`
	AuthorPhone      string     `db:"author_phone"`
	SourceName       string     `db:"source_name"`
	SourceURL        string     `db:"source_url"`
	SourceDate       *time.Time `db:"source_date"`
	EntryDate        time.Time  `db:"entry_date"`
	ExpiryDate       *time.Time `db:"expiry_date"`
	PFIFStatus       string     `db:"pfif_status"`
	Status           string     `db:"status"`
	Visibility       Visibility `db:"visibility"`
	Origin           string     `db:"origin"`
	Hidden           bool       `db:"hidden"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`

	// Notes is filled by the methods that document doing so.
	Notes []Note `db:"-"`
}

// HomeArea returns a short "neighborhood, city, state" description.
func (p *Person) HomeArea() string {
	var parts []string
	for _, s := range []string{p.HomeNeighborhood, p.HomeCity, p.HomeState, p.HomeCountry} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return joinComma(parts)
}

// HasContact reports whether the record includes private contact details.
func (p *Person) HasContact() bool { return p.AuthorEmail != "" || p.AuthorPhone != "" }

// Note is a PFIF note: an update about a person.
type Note struct {
	ID                   int64      `db:"id"`
	RecordID             string     `db:"record_id"`
	PersonID             int64      `db:"person_id"`
	LinkedPersonRecordID string     `db:"linked_person_record_id"`
	Status               string     `db:"status"`
	AuthorMadeContact    bool       `db:"author_made_contact"`
	AuthorName           string     `db:"author_name"`
	AuthorEmail          string     `db:"author_email"`
	AuthorPhone          string     `db:"author_phone"`
	EmailOfFoundPerson   string     `db:"email_of_found_person"`
	PhoneOfFoundPerson   string     `db:"phone_of_found_person"`
	LastKnownLocation    string     `db:"last_known_location"`
	Text                 string     `db:"text"`
	PhotoURL             string     `db:"photo_url"`
	Private              bool       `db:"private"`
	Hidden               bool       `db:"hidden"`
	Origin               string     `db:"origin"`
	SourceDate           *time.Time `db:"source_date"`
	EntryDate            time.Time  `db:"entry_date"`
}

// Date returns when the note was originally written.
func (n *Note) Date() time.Time {
	if n.SourceDate != nil && !n.SourceDate.IsZero() {
		return *n.SourceDate
	}
	return n.EntryDate
}

// HasContact reports whether the note includes private contact details.
func (n *Note) HasContact() bool {
	return n.AuthorEmail != "" || n.AuthorPhone != "" || n.EmailOfFoundPerson != "" || n.PhoneOfFoundPerson != ""
}

// Roles of responder accounts.
const (
	RoleAdmin     = "admin"
	RoleResponder = "responder"
)

// User is a responder or administrator account.
type User struct {
	ID           int64      `db:"id"`
	Username     string     `db:"username"`
	DisplayName  string     `db:"display_name"`
	PasswordHash string     `db:"password_hash"`
	Role         string     `db:"role"`
	Disabled     bool       `db:"disabled"`
	CreatedAt    time.Time  `db:"created_at"`
	LastLoginAt  *time.Time `db:"last_login_at"`
}

// IsAdmin reports whether the user may manage accounts and import data.
func (u *User) IsAdmin() bool { return u != nil && u.Role == RoleAdmin }

// Name returns the display name or the username.
func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// Session is a signed-in responder.
type Session struct {
	User      User
	CSRFToken string
	ExpiresAt time.Time
}

// APIToken grants machine access to the PFIF API.
type APIToken struct {
	ID         int64      `db:"id"`
	Name       string     `db:"name"`
	Scope      string     `db:"scope"`
	CreatedAt  time.Time  `db:"created_at"`
	LastUsedAt *time.Time `db:"last_used_at"`
	RevokedAt  *time.Time `db:"revoked_at"`
}

// CanWrite reports whether the token may import records.
func (t *APIToken) CanWrite() bool { return t.Scope == "readwrite" }

// AuditEntry is one line of the audit log.
type AuditEntry struct {
	ID     int64     `db:"id"`
	At     time.Time `db:"at"`
	Actor  string    `db:"actor"`
	Action string    `db:"action"`
	Detail string    `db:"detail"`
}

// Stats summarizes the board.
type Stats struct {
	Total    int
	Safe     int
	Missing  int
	Deceased int
	Unknown  int
	Last24h  int
	Hidden   int
	BySMS    int
	Imported int
}

// Match is a possible reunion: a missing report and a safe report with a
// similar name.
type Match struct {
	Missing    Person
	Safe       Person
	Similarity float64
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
