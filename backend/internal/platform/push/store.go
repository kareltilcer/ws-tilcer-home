package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appdb "github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/db"
	"github.com/kareltilcer/ws-tilcer-home/backend/internal/platform/idgen"
)

// tsFormat is RFC 3339 with fixed-width nanoseconds — the same shape the session
// store uses, so string comparison stays chronological.
const tsFormat = time.RFC3339Nano

// Store is the platform-owned subscription + preference store.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Subscription is one browser endpoint belonging to one member.
type Subscription struct {
	ID           string     `json:"id"`
	UserID       string     `json:"-"`
	Endpoint     string     `json:"endpoint"`
	P256dh       string     `json:"-"`
	Auth         string     `json:"-"`
	UserAgent    *string    `json:"user_agent"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	FailingSince *time.Time `json:"-"`
}

// Preferences are a member's mute switches (D53a). The zero value is NOT the
// default — an absent row means everything is on, which DefaultPreferences returns.
type Preferences struct {
	Enabled    bool       `json:"enabled"`
	Categories Categories `json:"categories"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

// Categories are the three independently mutable buckets.
type Categories struct {
	Broadcast bool `json:"broadcast"`
	Triggers  bool `json:"triggers"`
	Summaries bool `json:"summaries"`
	// Chat is v10's fourth bucket (D248). Defaults ON like the other three: a
	// missing preferences row means all-on, so a member who has never opened the
	// panel still hears about a message addressed to them.
	Chat bool `json:"chat"`
}

// DefaultPreferences is what a member who has never opened the panel gets:
// everything on. Consent is the browser permission; these are only mutes.
func DefaultPreferences() Preferences {
	return Preferences{Enabled: true, Categories: Categories{Broadcast: true, Triggers: true, Summaries: true, Chat: true}}
}

// PreferencesPatch is a partial update; nil fields are left alone.
type PreferencesPatch struct {
	Enabled   *bool `json:"enabled"`
	Broadcast *bool `json:"-"`
	Triggers  *bool `json:"-"`
	Summaries *bool `json:"-"`
	Chat      *bool `json:"-"`
}

// ---- subscriptions ----

// UpsertResult describes what an Upsert actually did. The caller needs the
// difference to decide what is worth auditing: a page-load refresh of one's own
// endpoint is noise, but a first subscribe and a change of owner are both
// consent decisions and must be visible in the log.
type UpsertResult struct {
	// Created is true when the endpoint had no row at all.
	Created bool
	// PreviousUserID is the member the endpoint belonged to before, set only when
	// this upsert moved it to a DIFFERENT member.
	PreviousUserID string
}

// Transferred reports whether the endpoint changed hands.
func (r UpsertResult) Transferred() bool { return r.PreviousUserID != "" }

// Upsert registers (or refreshes) a device's subscription, keyed on the endpoint.
// A re-subscribe with the same endpoint updates the keys and clears any failure
// state rather than creating a second row — the browser re-issues the same
// endpoint across page loads, and duplicates would multiply every notification.
//
// The endpoint is globally unique, so a device that changed hands moves to the
// new owner rather than notifying the old one. That case is reported back
// through UpsertResult: on a shared household browser the endpoint outlives the
// session, so the move is how the previous member stops receiving — and it is
// the one thing here that has to reach the audit log.
func (s *Store) Upsert(ctx context.Context, tx *sql.Tx, userID, endpoint, p256dh, auth, userAgent string, now time.Time) (Subscription, UpsertResult, error) {
	ts := now.UTC().Format(tsFormat)
	var ua any
	if userAgent != "" {
		ua = userAgent
	}

	var existingID, existingUser string
	err := tx.QueryRowContext(ctx,
		`SELECT id, user_id FROM push_subscriptions WHERE endpoint = ?`, endpoint).Scan(&existingID, &existingUser)
	switch {
	case err == sql.ErrNoRows:
		id := idgen.New()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_seen_at, failing_since)
			 VALUES (?,?,?,?,?,?,?,?,NULL)`,
			id, userID, endpoint, p256dh, auth, ua, ts, ts); err != nil {
			return Subscription{}, UpsertResult{}, err
		}
		return Subscription{ID: id, UserID: userID, Endpoint: endpoint, CreatedAt: now.UTC(), LastSeenAt: now.UTC()},
			UpsertResult{Created: true}, nil
	case err != nil:
		return Subscription{}, UpsertResult{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE push_subscriptions
		    SET user_id = ?, p256dh = ?, auth = ?, user_agent = ?, last_seen_at = ?, failing_since = NULL
		  WHERE id = ?`,
		userID, p256dh, auth, ua, ts, existingID); err != nil {
		return Subscription{}, UpsertResult{}, err
	}
	var res UpsertResult
	if existingUser != userID {
		res.PreviousUserID = existingUser
	}
	sub, err := s.byID(ctx, tx, existingID)
	return sub, res, err
}

func (s *Store) byID(ctx context.Context, tx *sql.Tx, id string) (Subscription, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_seen_at, failing_since
		   FROM push_subscriptions WHERE id = ?`, id)
	return scanSubscription(row)
}

// Delete removes one of the caller's endpoints. It is scoped by user id so a
// member can never unsubscribe another member's device. Idempotent.
func (s *Store) Delete(ctx context.Context, tx *sql.Tx, userID, endpoint string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteSubscriptionByID prunes a dead endpoint (404/410, or failing too long).
func (s *Store) DeleteSubscriptionByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ?`, id)
	return err
}

// MarkFailing stamps failing_since on the first failure and leaves it alone
// afterwards, so the value is the age of the failure streak, not its last event.
func (s *Store) MarkFailing(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE push_subscriptions SET failing_since = ? WHERE id = ? AND failing_since IS NULL`,
		now.UTC().Format(tsFormat), id)
	return err
}

// ClearFailing ends a failure streak and records liveness.
func (s *Store) ClearFailing(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE push_subscriptions SET failing_since = NULL, last_seen_at = ? WHERE id = ?`,
		now.UTC().Format(tsFormat), id)
	return err
}

// ListForUser returns a member's own devices.
func (s *Store) ListForUser(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_seen_at, failing_since
		   FROM push_subscriptions WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, scanSubscription)
}

// EligibleSubscriptions resolves recipients to the endpoints that may actually
// receive this envelope: one query, joined against the mute preferences, so the
// fan-out never does a per-user lookup (no N+1).
//
// A missing preferences row means all-on (LEFT JOIN + COALESCE), which is why a
// member who never opened the settings panel still gets notified.
func (s *Store) EligibleSubscriptions(ctx context.Context, userIDs []string, category string, bypassMutes bool) ([]Subscription, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	catColumn, err := categoryColumn(category)
	if err != nil {
		return nil, err
	}

	q := `SELECT s.id, s.user_id, s.endpoint, s.p256dh, s.auth, s.user_agent,
	             s.created_at, s.last_seen_at, s.failing_since
	        FROM push_subscriptions s
	        LEFT JOIN notification_preferences p ON p.user_id = s.user_id
	       WHERE s.user_id IN (` + appdb.Placeholders(len(userIDs)) + `)`
	if !bypassMutes {
		q += ` AND COALESCE(p.enabled, 1) = 1 AND COALESCE(p.` + catColumn + `, 1) = 1`
	}
	q += ` ORDER BY s.user_id, s.created_at`

	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return appdb.Collect(rows, scanSubscription)
}

// categoryColumn maps a category onto its mute column. The mapping is explicit
// (never string-built from input) so no caller can inject a column name.
func categoryColumn(category string) (string, error) {
	switch category {
	case CategoryBroadcast:
		return "cat_broadcast", nil
	case CategoryTriggers:
		return "cat_triggers", nil
	case CategorySummaries:
		return "cat_summaries", nil
	case CategoryChat:
		return "cat_chat", nil
	default:
		return "", fmt.Errorf("push: unknown category %q", category)
	}
}

// ---- preferences ----

// rowQuerier is satisfied by both *sql.DB and *sql.Tx. The pool is capped at a
// single connection (SQLite is single-writer), so a read issued on the DB while
// a transaction is open would wait for a connection that the transaction itself
// is holding — a guaranteed deadlock. Reads that may run inside a transaction
// therefore take the querier explicitly.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Preferences reads a member's mutes, defaulting an absent row to all-on.
func (s *Store) Preferences(ctx context.Context, userID string) (Preferences, error) {
	return s.preferences(ctx, s.db, userID)
}

func (s *Store) preferences(ctx context.Context, q rowQuerier, userID string) (Preferences, error) {
	var (
		p                       Preferences
		enabled, b, t, sum, cht int
		updated                 sql.NullString
		err                     error
	)
	err = q.QueryRowContext(ctx,
		`SELECT enabled, cat_broadcast, cat_triggers, cat_summaries, cat_chat, updated_at
		   FROM notification_preferences WHERE user_id = ?`, userID).
		Scan(&enabled, &b, &t, &sum, &cht, &updated)
	if err == sql.ErrNoRows {
		return DefaultPreferences(), nil
	}
	if err != nil {
		return Preferences{}, err
	}
	p.Enabled = enabled == 1
	p.Categories = Categories{Broadcast: b == 1, Triggers: t == 1, Summaries: sum == 1, Chat: cht == 1}
	if updated.Valid {
		ts := parseTS(updated.String)
		p.UpdatedAt = &ts
	}
	return p, nil
}

// UpdatePreferences applies a partial patch, lazily creating the row.
func (s *Store) UpdatePreferences(ctx context.Context, tx *sql.Tx, userID string, patch PreferencesPatch, now time.Time) (Preferences, error) {
	cur, err := s.preferences(ctx, tx, userID)
	if err != nil {
		return Preferences{}, err
	}
	next := cur
	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.Broadcast != nil {
		next.Categories.Broadcast = *patch.Broadcast
	}
	if patch.Triggers != nil {
		next.Categories.Triggers = *patch.Triggers
	}
	if patch.Summaries != nil {
		next.Categories.Summaries = *patch.Summaries
	}
	if patch.Chat != nil {
		next.Categories.Chat = *patch.Chat
	}

	ts := now.UTC().Format(tsFormat)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO notification_preferences (user_id, enabled, cat_broadcast, cat_triggers, cat_summaries, cat_chat, updated_at)
		 VALUES (?,?,?,?,?,?,?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   enabled = excluded.enabled, cat_broadcast = excluded.cat_broadcast,
		   cat_triggers = excluded.cat_triggers, cat_summaries = excluded.cat_summaries,
		   cat_chat = excluded.cat_chat, updated_at = excluded.updated_at`,
		userID, b2i(next.Enabled), b2i(next.Categories.Broadcast), b2i(next.Categories.Triggers),
		b2i(next.Categories.Summaries), b2i(next.Categories.Chat), ts); err != nil {
		return Preferences{}, err
	}
	at := now.UTC()
	next.UpdatedAt = &at
	return next, nil
}

// ---- the member directory + audience resolution ----

// Audience is who a notification goes to (shared by broadcasts, trigger rules and
// schedules). Default scope is "all" (D66).
type Audience struct {
	Scope string   `json:"scope"` // all | roles | users
	Roles []string `json:"roles,omitempty"`
	Users []string `json:"users,omitempty"`
}

// Audience scopes.
const (
	ScopeAll   = "all"
	ScopeRoles = "roles"
	ScopeUsers = "users"
)

// Valid reports whether the audience names anybody at all. An explicitly EMPTY
// role or user selection is the one case a composer can fix, so it is a 422 —
// "all" with nobody subscribed is not an error, it is the household's state.
func (a Audience) Valid() bool {
	switch a.Scope {
	case ScopeAll, "":
		return true
	case ScopeRoles:
		return len(a.Roles) > 0
	case ScopeUsers:
		return len(a.Users) > 0
	default:
		return false
	}
}

// Member is a household member as home knows them. Home has no user table —
// auth owns identity — so this is projected from the session store, which caches
// each identity + roles at login and refreshes them on every role re-mint (FR-A2).
type Member struct {
	UserID        string   `json:"user_id"`
	Email         string   `json:"email"`
	DisplayName   string   `json:"display_name"`
	Roles         []string `json:"roles"`
	Subscriptions int      `json:"subscriptions"`
}

// Members lists everyone home has ever seen a session for, FRESHEST identity per
// user, with a count of their subscribed devices. It drives the "Vybraným lidem"
// picker and labels the delivery log.
//
// Freshest, not newest — the two stopped being the same thing when the re-mint
// began writing the identity as well as the roles, and the ⚠ block in the body
// says why the distinction is the whole point.
func (s *Store) Members(ctx context.Context) ([]Member, error) {
	// One row per user: the session whose identity was CONFIRMED most recently,
	// which is what `roles_refreshed_at` stamps — every re-mint writes the roles,
	// the email and the display name auth handed back (auth.SessionStore
	// .RefreshIdentity) and moves that stamp.
	//
	// ⚠ IT IS NOT THE NEWEST SESSION, AND THE DIFFERENCE IS A MEMBER WHO RENAMED
	// THEMSELVES. `MAX(created_at)` picks the last device to log in, which is not
	// the last device to be USED: a tablet signed into once in July and left in a
	// drawer outranks the laptop its owner works on every day, and it is a row
	// nothing will ever refresh — so the rename that the laptop's session picked up
	// within fifteen minutes would have gone on being invisible to the whole
	// household for as long as that tablet's row was the newest. Both columns are
	// written at Create, so for anybody with one session this changes nothing.
	// ⚠ UNORDERED ON PURPOSE — THE SORT IS IN GO, BELOW. SQLite's TRIM() strips the
	// ASCII space and nothing else, so an ORDER BY built on it disagreed with the
	// strings.TrimSpace the projection applies a few lines down: a display_name of
	// "\t" was blank to Go and a name to SQL, which sorted the row under whitespace
	// and showed it under its email. Two spellings of "is this blank" cannot be kept
	// in agreement; there is now one, and it is the one the member is shown under.
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.user_id, s.email, COALESCE(s.display_name, ''), s.roles,
		       (SELECT COUNT(*) FROM push_subscriptions ps WHERE ps.user_id = s.user_id)
		  FROM sessions s
		  JOIN (SELECT user_id, MAX(roles_refreshed_at) AS freshest FROM sessions GROUP BY user_id) latest
		    ON latest.user_id = s.user_id AND latest.freshest = s.roles_refreshed_at
		 GROUP BY s.user_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Member
	for rows.Next() {
		var (
			m         Member
			rolesJSON string
		)
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &rolesJSON, &m.Subscriptions); err != nil {
			return nil, err
		}
		// ⚠ A ROW WITH NO USER ID IS NOT A MEMBER, AND IT IS DROPPED IN THE
		// PROJECTION rather than in one consumer of it. chat's add-member picker
		// listed such a row as a button that could only answer 422 ("Chybí
		// uživatel.") — but the same row is an unnamed entry in admin's "Vybraným
		// lidem" picker, a blank Příjemce in the delivery log, and an empty string
		// inside a resolved push audience. One projection, one answer to "who is a
		// member": this is where the display name is normalised, so it is where the
		// id is too.
		if strings.TrimSpace(m.UserID) == "" {
			continue
		}
		_ = json.Unmarshal([]byte(rolesJSON), &m.Roles)
		// ⚠ TRIMMED BEFORE THE FALLBACK, because a name that is blank once trimmed
		// is not a name. NULL and "" already fell through to the email here; a
		// display_name of "   " did not — it survived as itself into every surface
		// that renders a member, and chat's add-member picker drew a bubble with
		// nothing in it. It drew it FIRST, too: a space sorts before every letter,
		// so the one unreadable row led the list.
		m.DisplayName = strings.TrimSpace(m.DisplayName)
		if m.DisplayName == "" {
			m.DisplayName = m.Email
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// ⚠ SORTED BY WHAT EACH MEMBER IS SHOWN AS, which is only knowable here: the
	// email fallback above is what a blank name resolves to, so the key has to be
	// read after it. UserID breaks ties so the order is total and the list does not
	// shuffle between two members who share a name.
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].DisplayName), strings.ToLower(out[j].DisplayName)
		if a != b {
			return a < b
		}
		return out[i].UserID < out[j].UserID
	})
	return out, nil
}

// ResolveAudience turns an audience into user ids (HANDOFF-7 §10).
//
//   - "all" is every user with at least one subscription — sending to a member
//     with no device is a no-op, so the set is exactly who could receive it;
//   - "roles" matches home's CACHED session roles, never anything the client
//     sent, and the "*" superuser matches every role;
//   - "users" is the given ids, filtered to members home actually knows.
func (s *Store) ResolveAudience(ctx context.Context, a Audience) ([]string, error) {
	if a.Scope == "" {
		a.Scope = ScopeAll
	}
	switch a.Scope {
	case ScopeAll:
		return s.subscribedUserIDs(ctx)
	case ScopeRoles:
		members, err := s.Members(ctx)
		if err != nil {
			return nil, err
		}
		want := make(map[string]bool, len(a.Roles))
		for _, r := range a.Roles {
			want[strings.ToLower(strings.TrimSpace(r))] = true
		}
		var out []string
		for _, m := range members {
			for _, role := range m.Roles {
				if role == "*" || want[strings.ToLower(role)] {
					out = append(out, m.UserID)
					break
				}
			}
		}
		return out, nil
	case ScopeUsers:
		members, err := s.Members(ctx)
		if err != nil {
			return nil, err
		}
		known := make(map[string]bool, len(members))
		for _, m := range members {
			known[m.UserID] = true
		}
		var out []string
		for _, id := range a.Users {
			if known[id] {
				out = append(out, id)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("push: unknown audience scope %q", a.Scope)
	}
}

func (s *Store) subscribedUserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM push_subscriptions ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---- helpers ----

func scanSubscription(row appdb.Scanner) (Subscription, error) {
	var (
		sub         Subscription
		ua, failing sql.NullString
		created, ls string
	)
	if err := row.Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &ua, &created, &ls, &failing); err != nil {
		return Subscription{}, err
	}
	if ua.Valid {
		v := ua.String
		sub.UserAgent = &v
	}
	sub.CreatedAt = parseTS(created)
	sub.LastSeenAt = parseTS(ls)
	if failing.Valid && failing.String != "" {
		t := parseTS(failing.String)
		sub.FailingSince = &t
	}
	return sub, nil
}

func parseTS(s string) time.Time {
	t, err := time.Parse(tsFormat, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DistinctUsers counts the members represented in a result set — the "recipients"
// half of a send result, after mute filtering.
func DistinctUsers(results []DeliveryResult) int {
	seen := make(map[string]bool, len(results))
	for _, r := range results {
		seen[r.UserID] = true
	}
	return len(seen)
}

// SortedUserIDs de-duplicates and orders an audience so a send is deterministic.
func SortedUserIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
