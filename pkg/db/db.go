package db

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	path string
}

type User struct {
	UserID       int64     `json:"user_id"`
	Username     string    `json:"username"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	LanguageCode string    `json:"language_code,omitempty"`
	IsPremium    bool      `json:"is_premium,omitempty"`
	Reputation   int       `json:"reputation"`
	WarnCount    int       `json:"warn_count"`
	IsBanned     bool      `json:"is_banned"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Group struct {
	ChatID      int64     `json:"chat_id"`
	Title       string    `json:"title"`
	Type        string    `json:"type"`
	IsMonitored bool      `json:"is_monitored"`
	AddedAt     time.Time `json:"added_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Message struct {
	ID        int64     `json:"id"`
	ChatID    int64     `json:"chat_id"`
	MessageID int       `json:"message_id"`
	UserID    int64     `json:"user_id"`
	Text      string    `json:"text"`
	HasMedia  bool      `json:"has_media"`
	HasLinks  bool      `json:"has_links"`
	CreatedAt time.Time `json:"created_at"`
}

type FlaggedPost struct {
	ID                int64      `json:"id"`
	GroupChatID       int64      `json:"group_chat_id"`
	GroupMessageID    int        `json:"group_message_id"`
	ModGroupMessageID int        `json:"mod_group_message_id"`
	UserID            int64      `json:"user_id"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	FlaggedAt         time.Time  `json:"flagged_at"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy        int64      `json:"resolved_by"`
}

type RepLog struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	ChangeAmount int       `json:"change_amount"`
	Reason       string    `json:"reason"`
	ByUserID     int64     `json:"by_user_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type UserProfile struct {
	UserID                 int64     `json:"user_id"`
	Username               string    `json:"username"`
	FirstName              string    `json:"first_name"`
	LastName               string    `json:"last_name"`
	LanguageCode           string    `json:"language_code,omitempty"`
	IsPremium              bool      `json:"is_premium,omitempty"`
	Bio                    string    `json:"bio"`
	HasPrivateForwards     bool      `json:"has_private_forwards,omitempty"`
	PersonalChatTitle      string    `json:"personal_chat_title,omitempty"`
	PersonalChatUsername   string    `json:"personal_chat_username,omitempty"`
	BusinessIntro          string    `json:"business_intro,omitempty"`
	PhotoFileID            string    `json:"photo_file_id"`
	PhotoFileUniqueID      string    `json:"photo_file_unique_id"`
	PhotoSmallFileID       string    `json:"photo_small_file_id"`
	PhotoSmallFileUniqueID string    `json:"photo_small_file_unique_id"`
	PhotoCount             int       `json:"photo_count"`
	HasPhoto               bool      `json:"has_photo"`
	NotFound               bool      `json:"not_found"`
	RawJSON                string    `json:"raw_json,omitempty"`
	FetchedAt              time.Time `json:"fetched_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SpamSnippet struct {
	ID        int64     `json:"id"`
	Snippet   string    `json:"snippet"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"created_at"`
}

func OpenDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory '%s': %w\n-> Action: Ensure parent directory '%s' is writable.", dir, err, dir)
		}
	}

	// SQLite connection string with WAL mode for performance
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	sqliteDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database at '%s': %w\n-> Action: Check file access permissions for '%s'.", dbPath, err, dbPath)
	}

	if err := sqliteDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite database at '%s': %w\n-> Action: Verify SQLite file is not corrupted or locked by another process.", dbPath, err)
	}

	database := &DB{DB: sqliteDB, path: dbPath}
	if err := database.AutoMigrate(); err != nil {
		return nil, fmt.Errorf("failed to initialize SQLite database schema: %w\n-> Action: Ensure database path '%s' is writable.", err, dbPath)
	}

	return database, nil
}

// Path returns the SQLite database file path.
func (d *DB) Path() string {
	return d.path
}

// BackupTo creates a consistent, standalone SQLite database backup file at the specified destination path.
func (d *DB) BackupTo(destPath string) error {
	// First checkpoint WAL journal to flush pending commits
	_, _ = d.Exec(`PRAGMA wal_checkpoint(PASSIVE);`)

	// Remove destination file if it already exists, as VACUUM INTO requires target file to NOT exist
	_ = os.Remove(destPath)

	escapedPath := strings.ReplaceAll(destPath, "'", "''")
	_, err := d.Exec(fmt.Sprintf("VACUUM INTO '%s';", escapedPath))
	if err == nil {
		return nil
	}

	// Fallback: checkpoint WAL with TRUNCATE and perform direct file copy if VACUUM INTO fails
	_, _ = d.Exec(`PRAGMA wal_checkpoint(TRUNCATE);`)
	if d.path != "" {
		srcFile, errSrc := os.Open(d.path)
		if errSrc == nil {
			defer srcFile.Close()
			dstFile, errDst := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if errDst == nil {
				defer dstFile.Close()
				if _, errCopy := io.Copy(dstFile, srcFile); errCopy == nil {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("failed to backup database: %w", err)
}

func (d *DB) AutoMigrate() error {
	if err := d.InitSchema(); err != nil {
		return err
	}

	// Schema evolution migrations
	migrations := []string{
		`ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT 0;`,
		`ALTER TABLE users ADD COLUMN language_code TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE users ADD COLUMN is_premium BOOLEAN NOT NULL DEFAULT 0;`,
		`CREATE TABLE IF NOT EXISTS user_profiles (
			user_id INTEGER PRIMARY KEY,
			username TEXT NOT NULL DEFAULT '',
			first_name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			language_code TEXT NOT NULL DEFAULT '',
			is_premium BOOLEAN NOT NULL DEFAULT 0,
			bio TEXT NOT NULL DEFAULT '',
			has_private_forwards BOOLEAN NOT NULL DEFAULT 0,
			personal_chat_title TEXT NOT NULL DEFAULT '',
			personal_chat_username TEXT NOT NULL DEFAULT '',
			business_intro TEXT NOT NULL DEFAULT '',
			photo_file_id TEXT NOT NULL DEFAULT '',
			photo_file_unique_id TEXT NOT NULL DEFAULT '',
			photo_small_file_id TEXT NOT NULL DEFAULT '',
			photo_small_file_unique_id TEXT NOT NULL DEFAULT '',
			photo_count INTEGER NOT NULL DEFAULT 0,
			has_photo BOOLEAN NOT NULL DEFAULT 0,
			not_found BOOLEAN NOT NULL DEFAULT 0,
			raw_json TEXT NOT NULL DEFAULT '',
			fetched_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_profiles_updated ON user_profiles(updated_at);`,
		`ALTER TABLE user_profiles ADD COLUMN not_found BOOLEAN NOT NULL DEFAULT 0;`,
		`ALTER TABLE user_profiles ADD COLUMN language_code TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE user_profiles ADD COLUMN is_premium BOOLEAN NOT NULL DEFAULT 0;`,
		`ALTER TABLE user_profiles ADD COLUMN has_private_forwards BOOLEAN NOT NULL DEFAULT 0;`,
		`ALTER TABLE user_profiles ADD COLUMN personal_chat_title TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE user_profiles ADD COLUMN personal_chat_username TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE user_profiles ADD COLUMN business_intro TEXT NOT NULL DEFAULT '';`,
		`CREATE TABLE IF NOT EXISTS spam_snippets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			snippet TEXT NOT NULL UNIQUE,
			category TEXT NOT NULL DEFAULT 'spam',
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_spam_snippets_snippet ON spam_snippets(snippet);`,
	}

	for _, stmt := range migrations {
		_, _ = d.Exec(stmt)
	}
	return nil
}

func (d *DB) InitSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		user_id INTEGER PRIMARY KEY,
		username TEXT NOT NULL DEFAULT '',
		first_name TEXT NOT NULL DEFAULT '',
		last_name TEXT NOT NULL DEFAULT '',
		language_code TEXT NOT NULL DEFAULT '',
		is_premium BOOLEAN NOT NULL DEFAULT 0,
		reputation INTEGER NOT NULL DEFAULT 100,
		warn_count INTEGER NOT NULL DEFAULT 0,
		is_banned BOOLEAN NOT NULL DEFAULT 0,
		is_admin BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS groups (
		chat_id INTEGER PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		type TEXT NOT NULL DEFAULT '',
		is_monitored BOOLEAN NOT NULL DEFAULT 1,
		added_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		message_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		text TEXT NOT NULL DEFAULT '',
		has_media BOOLEAN NOT NULL DEFAULT 0,
		has_links BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		UNIQUE(chat_id, message_id)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_user_created ON messages(user_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_created ON messages(chat_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);

	CREATE TABLE IF NOT EXISTS flagged_posts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		group_chat_id INTEGER NOT NULL,
		group_message_id INTEGER NOT NULL,
		mod_group_message_id INTEGER NOT NULL DEFAULT 0,
		user_id INTEGER NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		flagged_at DATETIME NOT NULL,
		resolved_at DATETIME,
		resolved_by INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_flagged_posts_status ON flagged_posts(status);

	CREATE TABLE IF NOT EXISTS reputation_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		change_amount INTEGER NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		by_user_id INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS user_profiles (
		user_id INTEGER PRIMARY KEY,
		username TEXT NOT NULL DEFAULT '',
		first_name TEXT NOT NULL DEFAULT '',
		last_name TEXT NOT NULL DEFAULT '',
		language_code TEXT NOT NULL DEFAULT '',
		is_premium BOOLEAN NOT NULL DEFAULT 0,
		bio TEXT NOT NULL DEFAULT '',
		has_private_forwards BOOLEAN NOT NULL DEFAULT 0,
		personal_chat_title TEXT NOT NULL DEFAULT '',
		personal_chat_username TEXT NOT NULL DEFAULT '',
		business_intro TEXT NOT NULL DEFAULT '',
		photo_file_id TEXT NOT NULL DEFAULT '',
		photo_file_unique_id TEXT NOT NULL DEFAULT '',
		photo_small_file_id TEXT NOT NULL DEFAULT '',
		photo_small_file_unique_id TEXT NOT NULL DEFAULT '',
		photo_count INTEGER NOT NULL DEFAULT 0,
		has_photo BOOLEAN NOT NULL DEFAULT 0,
		not_found BOOLEAN NOT NULL DEFAULT 0,
		raw_json TEXT NOT NULL DEFAULT '',
		fetched_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_user_profiles_updated ON user_profiles(updated_at);

	CREATE TABLE IF NOT EXISTS spam_snippets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		snippet TEXT NOT NULL UNIQUE,
		category TEXT NOT NULL DEFAULT 'spam',
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_spam_snippets_snippet ON spam_snippets(snippet);
	`
	_, err := d.Exec(schema)
	if err != nil {
		return err
	}

	// Auto-migration: ensure columns exist for pre-existing databases
	_, _ = d.Exec(`ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT 0;`)
	_, _ = d.Exec(`ALTER TABLE users ADD COLUMN language_code TEXT NOT NULL DEFAULT '';`)
	_, _ = d.Exec(`ALTER TABLE users ADD COLUMN is_premium BOOLEAN NOT NULL DEFAULT 0;`)
	return nil
}

// User Methods

func (d *DB) GetOrCreateUser(userID int64, username, firstName, lastName string, defaultRep int) (*User, bool, error) {
	now := time.Now()
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, language_code, is_premium, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.LanguageCode, &user.IsPremium, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		user = User{
			UserID:       userID,
			Username:     username,
			FirstName:    firstName,
			LastName:     lastName,
			LanguageCode: "",
			IsPremium:    false,
			Reputation:   defaultRep,
			WarnCount:    0,
			IsBanned:     false,
			IsAdmin:      false,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		_, err := d.Exec(`
			INSERT INTO users (user_id, username, first_name, last_name, language_code, is_premium, reputation, warn_count, is_banned, is_admin, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, user.UserID, user.Username, user.FirstName, user.LastName, user.LanguageCode, user.IsPremium, user.Reputation, user.WarnCount, user.IsBanned, user.IsAdmin, user.CreatedAt, user.UpdatedAt)
		if err != nil {
			return nil, false, err
		}
		return &user, true, nil
	} else if err != nil {
		return nil, false, err
	}

	// Update names/username if changed
	if user.Username != username || user.FirstName != firstName || user.LastName != lastName {
		user.Username = username
		user.FirstName = firstName
		user.LastName = lastName
		user.UpdatedAt = now
		_, _ = d.Exec(`UPDATE users SET username = ?, first_name = ?, last_name = ?, updated_at = ? WHERE user_id = ?`,
			username, firstName, lastName, now, userID)
	}

	return &user, false, nil
}

// UpdateUserMetadata updates the user's client app language code and Telegram Premium status.
func (d *DB) UpdateUserMetadata(userID int64, lang string, isPremium bool) error {
	now := time.Now()
	_, err := d.Exec(`
		UPDATE users 
		SET language_code = CASE WHEN ? != '' THEN ? ELSE language_code END,
		    is_premium = CASE WHEN ? = 1 THEN 1 ELSE is_premium END,
		    updated_at = ?
		WHERE user_id = ?
	`, lang, lang, isPremium, now, userID)
	return err
}

// UpdateUserName updates a user's username, first name, and last name.
func (d *DB) UpdateUserName(userID int64, username, firstName, lastName string) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE users SET username = ?, first_name = ?, last_name = ?, updated_at = ? WHERE user_id = ?`,
		username, firstName, lastName, now, userID)
	return err
}

func (d *DB) GetUserByID(userID int64) (*User, error) {
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, language_code, is_premium, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.LanguageCode, &user.IsPremium, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	username = strings.TrimSpace(username)
	username = strings.TrimPrefix(username, "@")
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, sql.ErrNoRows
	}

	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, language_code, is_premium, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users WHERE LOWER(TRIM(username, '@ ')) = LOWER(?)
	`, username).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.LanguageCode, &user.IsPremium, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetAllUsers(limit int) ([]User, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.Query(`
		SELECT user_id, username, first_name, last_name, language_code, is_premium, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users
		ORDER BY reputation DESC, created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.LanguageCode, &u.IsPremium, &u.Reputation, &u.WarnCount, &u.IsBanned, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (d *DB) AdjustReputation(userID int64, delta int, reason string, byUserID int64) (int, error) {
	now := time.Now()
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var newRep int
	err = tx.QueryRow(`
		UPDATE users 
		SET reputation = reputation + ?, updated_at = ? 
		WHERE user_id = ? 
		RETURNING reputation
	`, delta, now, userID).Scan(&newRep)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(`
		INSERT INTO reputation_logs (user_id, change_amount, reason, by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, delta, reason, byUserID, now)
	if err != nil {
		return 0, err
	}

	return newRep, tx.Commit()
}

func (d *DB) AdjustReputationWithCap(userID int64, delta int, maxCap int, reason string, byUserID int64) (int, error) {
	now := time.Now()
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentRep int
	err = tx.QueryRow(`SELECT reputation FROM users WHERE user_id = ?`, userID).Scan(&currentRep)
	if err != nil {
		return 0, err
	}

	newRep := currentRep + delta
	if maxCap > 0 && newRep > maxCap {
		newRep = maxCap
	}
	actualDelta := newRep - currentRep
	if actualDelta == 0 {
		return currentRep, nil
	}

	_, err = tx.Exec(`UPDATE users SET reputation = ?, updated_at = ? WHERE user_id = ?`, newRep, now, userID)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(`
		INSERT INTO reputation_logs (user_id, change_amount, reason, by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, actualDelta, reason, byUserID, now)
	if err != nil {
		return 0, err
	}

	return newRep, tx.Commit()
}

func (d *DB) HasReceivedDailyRepBump(userID int64, reasonPrefix string) (bool, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var count int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM reputation_logs
		WHERE user_id = ? AND reason LIKE ? AND created_at >= ?
	`, userID, reasonPrefix+"%", startOfDay).Scan(&count)
	return count > 0, err
}

func (d *DB) HasReceivedRepBonus(userID int64, reasonPrefix string) (bool, error) {
	var count int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM reputation_logs
		WHERE user_id = ? AND reason LIKE ?
	`, userID, reasonPrefix+"%").Scan(&count)
	return count > 0, err
}

func (d *DB) SetReputation(userID int64, targetRep int, reason string, byUserID int64) error {
	now := time.Now()
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var currentRep int
	err = tx.QueryRow(`SELECT reputation FROM users WHERE user_id = ?`, userID).Scan(&currentRep)
	if err != nil {
		return err
	}

	delta := targetRep - currentRep
	if delta == 0 {
		return nil
	}

	_, err = tx.Exec(`UPDATE users SET reputation = ?, updated_at = ? WHERE user_id = ?`, targetRep, now, userID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO reputation_logs (user_id, change_amount, reason, by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, delta, reason, byUserID, now)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) IncrementWarning(userID int64) (int, error) {
	now := time.Now()
	var newWarns int
	err := d.QueryRow(`
		UPDATE users 
		SET warn_count = warn_count + 1, updated_at = ? 
		WHERE user_id = ? 
		RETURNING warn_count
	`, now, userID).Scan(&newWarns)
	return newWarns, err
}

func (d *DB) ResetWarnings(userID int64) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE users SET warn_count = 0, updated_at = ? WHERE user_id = ?`, now, userID)
	return err
}

func (d *DB) SetUserBanned(userID int64, banned bool) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE users SET is_banned = ?, updated_at = ? WHERE user_id = ?`, banned, now, userID)
	return err
}

func (d *DB) SetUserAdmin(userID int64, isAdmin bool) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE users SET is_admin = ?, updated_at = ? WHERE user_id = ?`, isAdmin, now, userID)
	return err
}

// Group Methods

func (d *DB) SaveGroup(chatID int64, title, groupType string) error {
	now := time.Now()
	_, err := d.Exec(`
		INSERT INTO groups (chat_id, title, type, is_monitored, added_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			title = excluded.title,
			type = excluded.type,
			updated_at = excluded.updated_at
	`, chatID, title, groupType, now, now)
	return err
}

func (d *DB) SetGroupMonitored(chatID int64, monitored bool) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE groups SET is_monitored = ?, updated_at = ? WHERE chat_id = ?`, monitored, now, chatID)
	return err
}

func (d *DB) GetMonitoredGroups() ([]Group, error) {
	rows, err := d.Query(`SELECT chat_id, title, type, is_monitored, added_at, updated_at FROM groups WHERE is_monitored = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ChatID, &g.Title, &g.Type, &g.IsMonitored, &g.AddedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (d *DB) GetGroup(chatID int64) (*Group, error) {
	row := d.QueryRow(`SELECT chat_id, title, type, is_monitored, added_at, updated_at FROM groups WHERE chat_id = ?`, chatID)
	var g Group
	if err := row.Scan(&g.ChatID, &g.Title, &g.Type, &g.IsMonitored, &g.AddedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

// Message Methods

func (d *DB) SaveMessage(msg *Message) error {
	_, err := d.Exec(`
		INSERT INTO messages (chat_id, message_id, user_id, text, has_media, has_links, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id, message_id) DO UPDATE SET
			text = excluded.text,
			has_media = excluded.has_media,
			has_links = excluded.has_links
	`, msg.ChatID, msg.MessageID, msg.UserID, msg.Text, msg.HasMedia, msg.HasLinks, msg.CreatedAt)
	return err
}

func (d *DB) GetUserMessageCount(userID int64) (int, error) {
	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM messages WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

func (d *DB) GetRecentUserMessages(userID int64, limit int) ([]Message, error) {
	rows, err := d.Query(`
		SELECT id, chat_id, message_id, user_id, text, has_media, has_links, created_at
		FROM messages WHERE user_id = ?
		ORDER BY created_at DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChatID, &m.MessageID, &m.UserID, &m.Text, &m.HasMedia, &m.HasLinks, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// Flagged Posts Methods

func (d *DB) CreateFlaggedPost(chatID int64, messageID int, userID int64, reason string) (*FlaggedPost, error) {
	now := time.Now()
	res, err := d.Exec(`
		INSERT INTO flagged_posts (group_chat_id, group_message_id, user_id, reason, status, flagged_at)
		VALUES (?, ?, ?, ?, 'pending', ?)
	`, chatID, messageID, userID, reason, now)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &FlaggedPost{
		ID:             id,
		GroupChatID:    chatID,
		GroupMessageID: messageID,
		UserID:         userID,
		Reason:         reason,
		Status:         "pending",
		FlaggedAt:      now,
	}, nil
}

func (d *DB) CreateResolvedFlaggedPost(chatID int64, messageID int, userID int64, reason string, status string, resolvedBy int64) (*FlaggedPost, error) {
	now := time.Now()
	res, err := d.Exec(`
		INSERT INTO flagged_posts (group_chat_id, group_message_id, user_id, reason, status, flagged_at, resolved_at, resolved_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, chatID, messageID, userID, reason, status, now, now, resolvedBy)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &FlaggedPost{
		ID:             id,
		GroupChatID:    chatID,
		GroupMessageID: messageID,
		UserID:         userID,
		Reason:         reason,
		Status:         status,
		FlaggedAt:      now,
		ResolvedAt:     &now,
		ResolvedBy:     resolvedBy,
	}, nil
}

func (d *DB) UpdateFlagModMessageID(flagID int64, modMsgID int) error {
	_, err := d.Exec(`UPDATE flagged_posts SET mod_group_message_id = ? WHERE id = ?`, modMsgID, flagID)
	return err
}

func (d *DB) ResolveFlaggedPost(flagID int64, status string, resolvedBy int64) error {
	now := time.Now()
	_, err := d.Exec(`
		UPDATE flagged_posts 
		SET status = ?, resolved_at = ?, resolved_by = ? 
		WHERE id = ?
	`, status, now, resolvedBy, flagID)
	return err
}

func (d *DB) GetFlaggedPost(flagID int64) (*FlaggedPost, error) {
	var fp FlaggedPost
	var resolvedAt sql.NullTime
	err := d.QueryRow(`
		SELECT id, group_chat_id, group_message_id, mod_group_message_id, user_id, reason, status, flagged_at, resolved_at, resolved_by
		FROM flagged_posts WHERE id = ?
	`, flagID).Scan(&fp.ID, &fp.GroupChatID, &fp.GroupMessageID, &fp.ModGroupMessageID, &fp.UserID, &fp.Reason, &fp.Status, &fp.FlaggedAt, &resolvedAt, &fp.ResolvedBy)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		fp.ResolvedAt = &resolvedAt.Time
	}
	return &fp, nil
}

func (d *DB) GetPendingFlagsCount() (int, error) {
	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM flagged_posts WHERE status = 'pending'`).Scan(&count)
	return count, err
}

// Retention & Cleanup Methods

func (d *DB) PruneOldMessages(days int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	res, err := d.Exec(`DELETE FROM messages WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) PruneUserPostHistory(maxPostsPerUser int) (int64, error) {
	// Keep latest maxPostsPerUser posts per user across all groups using SQLite window function
	query := `
	DELETE FROM messages
	WHERE id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY created_at DESC) as rnum
			FROM messages
		) WHERE rnum > ?
	)
	`
	res, err := d.Exec(query, maxPostsPerUser)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Stats

type Stats struct {
	TotalUsers        int `json:"total_users"`
	TotalGroups       int `json:"total_groups"`
	TotalMessages     int `json:"total_messages"`
	PendingFlags      int `json:"pending_flags"`
	ResolvedFlags     int `json:"resolved_flags"`
	TotalUserProfiles int `json:"total_user_profiles"`
}

func (d *DB) GetStats() (*Stats, error) {
	var s Stats
	if err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&s.TotalUsers); err != nil {
		return nil, err
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM groups WHERE is_monitored = 1`).Scan(&s.TotalGroups); err != nil {
		return nil, err
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&s.TotalMessages); err != nil {
		return nil, err
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM flagged_posts WHERE status = 'pending'`).Scan(&s.PendingFlags); err != nil {
		return nil, err
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM flagged_posts WHERE status != 'pending'`).Scan(&s.ResolvedFlags); err != nil {
		return nil, err
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM user_profiles`).Scan(&s.TotalUserProfiles)
	return &s, nil
}

// User Profile Methods

func (d *DB) SaveUserProfile(p *UserProfile) error {
	now := time.Now()
	if p.FetchedAt.IsZero() {
		p.FetchedAt = now
	}
	p.UpdatedAt = now

	_, err := d.Exec(`
		INSERT INTO user_profiles (
			user_id, username, first_name, last_name, language_code, is_premium, bio,
			has_private_forwards, personal_chat_title, personal_chat_username, business_intro,
			photo_file_id, photo_file_unique_id, photo_small_file_id, photo_small_file_unique_id,
			photo_count, has_photo, not_found, raw_json, fetched_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			username = excluded.username,
			first_name = excluded.first_name,
			last_name = excluded.last_name,
			language_code = CASE WHEN excluded.language_code != '' THEN excluded.language_code ELSE user_profiles.language_code END,
			is_premium = CASE WHEN excluded.is_premium = 1 THEN 1 ELSE user_profiles.is_premium END,
			bio = excluded.bio,
			has_private_forwards = excluded.has_private_forwards,
			personal_chat_title = excluded.personal_chat_title,
			personal_chat_username = excluded.personal_chat_username,
			business_intro = excluded.business_intro,
			photo_file_id = excluded.photo_file_id,
			photo_file_unique_id = excluded.photo_file_unique_id,
			photo_small_file_id = excluded.photo_small_file_id,
			photo_small_file_unique_id = excluded.photo_small_file_unique_id,
			photo_count = excluded.photo_count,
			has_photo = excluded.has_photo,
			not_found = excluded.not_found,
			raw_json = excluded.raw_json,
			fetched_at = excluded.fetched_at,
			updated_at = excluded.updated_at
	`, p.UserID, p.Username, p.FirstName, p.LastName, p.LanguageCode, p.IsPremium, p.Bio,
		p.HasPrivateForwards, p.PersonalChatTitle, p.PersonalChatUsername, p.BusinessIntro,
		p.PhotoFileID, p.PhotoFileUniqueID, p.PhotoSmallFileID, p.PhotoSmallFileUniqueID,
		p.PhotoCount, p.HasPhoto, p.NotFound, p.RawJSON, p.FetchedAt, p.UpdatedAt)
	return err
}

func (d *DB) GetUserProfile(userID int64) (*UserProfile, error) {
	var p UserProfile
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, language_code, is_premium, bio,
		       has_private_forwards, personal_chat_title, personal_chat_username, business_intro,
		       photo_file_id, photo_file_unique_id, photo_small_file_id, photo_small_file_unique_id,
		       photo_count, has_photo, not_found, raw_json, fetched_at, updated_at
		FROM user_profiles WHERE user_id = ?
	`, userID).Scan(
		&p.UserID, &p.Username, &p.FirstName, &p.LastName, &p.LanguageCode, &p.IsPremium, &p.Bio,
		&p.HasPrivateForwards, &p.PersonalChatTitle, &p.PersonalChatUsername, &p.BusinessIntro,
		&p.PhotoFileID, &p.PhotoFileUniqueID, &p.PhotoSmallFileID, &p.PhotoSmallFileUniqueID,
		&p.PhotoCount, &p.HasPhoto, &p.NotFound, &p.RawJSON, &p.FetchedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetUserProfileByUsername retrieves the cached UserProfile matching the given username.
func (d *DB) GetUserProfileByUsername(username string) (*UserProfile, error) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return nil, fmt.Errorf("empty username")
	}
	var p UserProfile
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, language_code, is_premium, bio,
		       has_private_forwards, personal_chat_title, personal_chat_username, business_intro,
		       photo_file_id, photo_file_unique_id, photo_small_file_id, photo_small_file_unique_id,
		       photo_count, has_photo, not_found, raw_json, fetched_at, updated_at
		FROM user_profiles WHERE LOWER(username) = LOWER(?)
		ORDER BY updated_at DESC LIMIT 1
	`, username).Scan(
		&p.UserID, &p.Username, &p.FirstName, &p.LastName, &p.LanguageCode, &p.IsPremium, &p.Bio,
		&p.HasPrivateForwards, &p.PersonalChatTitle, &p.PersonalChatUsername, &p.BusinessIntro,
		&p.PhotoFileID, &p.PhotoFileUniqueID, &p.PhotoSmallFileID, &p.PhotoSmallFileUniqueID,
		&p.PhotoCount, &p.HasPhoto, &p.NotFound, &p.RawJSON, &p.FetchedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ReputationLog represents a single reputation adjustment audit log entry.
type ReputationLog struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	ChangeAmount int       `json:"change_amount"`
	Reason       string    `json:"reason"`
	ByUserID     int64     `json:"by_user_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// GetUserReputationLogs returns the audit history of reputation adjustments for a user.
func (d *DB) GetUserReputationLogs(userID int64, limit int) ([]ReputationLog, error) {
	query := `
		SELECT id, user_id, change_amount, reason, by_user_id, created_at
		FROM reputation_logs
		WHERE user_id = ?
		ORDER BY created_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := d.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []ReputationLog
	for rows.Next() {
		var rl ReputationLog
		if err := rows.Scan(&rl.ID, &rl.UserID, &rl.ChangeAmount, &rl.Reason, &rl.ByUserID, &rl.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, rl)
	}
	return logs, nil
}

// GetUserFlaggedPosts returns the moderation and trigger ban history for a user.
func (d *DB) GetUserFlaggedPosts(userID int64, limit int) ([]FlaggedPost, error) {
	query := `
		SELECT id, group_chat_id, group_message_id, mod_group_message_id, user_id, reason, status, flagged_at, resolved_at, resolved_by
		FROM flagged_posts
		WHERE user_id = ?
		ORDER BY flagged_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := d.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []FlaggedPost
	for rows.Next() {
		var fp FlaggedPost
		var resolvedAt sql.NullTime
		if err := rows.Scan(&fp.ID, &fp.GroupChatID, &fp.GroupMessageID, &fp.ModGroupMessageID, &fp.UserID, &fp.Reason, &fp.Status, &fp.FlaggedAt, &resolvedAt, &fp.ResolvedBy); err != nil {
			return nil, err
		}
		if resolvedAt.Valid {
			fp.ResolvedAt = &resolvedAt.Time
		}
		posts = append(posts, fp)
	}
	return posts, nil
}

// UserFullDump contains comprehensive information about a single Telegram user.
type UserFullDump struct {
	User           *User           `json:"user"`
	Profile        *UserProfile    `json:"profile,omitempty"`
	MessageCount   int             `json:"message_count"`
	RecentMessages []Message       `json:"recent_messages,omitempty"`
	ReputationLogs []ReputationLog `json:"reputation_logs,omitempty"`
	FlaggedPosts   []FlaggedPost   `json:"flagged_posts,omitempty"`
	IsSpamBioMatch bool            `json:"is_spam_bio_match"`
	MatchedBioKws  []string        `json:"matched_bio_keywords,omitempty"`
	IsSuperAdmin   bool            `json:"is_super_admin"`
}

// GetUserFullDump searches for a user by numeric user ID or @username, aggregating all known records.
func (d *DB) GetUserFullDump(identifier string, superAdminID int64, extraKeywords ...string) (*UserFullDump, error) {
	rawIdentifier := identifier
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("user identifier cannot be empty")
	}

	var user *User
	var profile *UserProfile

	// 1. Try parsing as numeric Telegram user ID
	if id, err := strconv.ParseInt(strings.TrimPrefix(identifier, "@"), 10, 64); err == nil && id != 0 {
		u, errUser := d.GetUserByID(id)
		if errUser == nil && u != nil {
			user = u
		}
		p, errProf := d.GetUserProfile(id)
		if errProf == nil && p != nil {
			profile = p
		}
	}

	// 2. If not found by numeric ID, try username lookup
	if user == nil && profile == nil {
		cleanUsername := strings.TrimPrefix(identifier, "@")
		u, errUser := d.GetUserByUsername(cleanUsername)
		if errUser == nil && u != nil {
			user = u
		}
		p, errProf := d.GetUserProfileByUsername(cleanUsername)
		if errProf == nil && p != nil {
			profile = p
		}
	}

	// If we found a profile but no user row yet, check user by profile.UserID
	if user == nil && profile != nil {
		u, errUser := d.GetUserByID(profile.UserID)
		if errUser == nil && u != nil {
			user = u
		}
	}

	// If we found a user row but no profile row yet, check profile by user.UserID
	if user != nil && profile == nil {
		p, errProf := d.GetUserProfile(user.UserID)
		if errProf == nil && p != nil {
			profile = p
		}
	}

	if user == nil && profile == nil {
		return nil, fmt.Errorf("user %q not found in database", rawIdentifier)
	}

	var targetID int64
	if user != nil {
		targetID = user.UserID
	} else if profile != nil {
		targetID = profile.UserID
	}

	// Fallback stub user if user table didn't have record but profile existed
	if user == nil && profile != nil {
		user = &User{
			UserID:    profile.UserID,
			Username:  profile.Username,
			FirstName: profile.FirstName,
			LastName:  profile.LastName,
			CreatedAt: profile.FetchedAt,
			UpdatedAt: profile.UpdatedAt,
		}
	}

	msgCount, _ := d.GetUserMessageCount(targetID)
	recentMsgs, _ := d.GetRecentUserMessages(targetID, 20)
	repLogs, _ := d.GetUserReputationLogs(targetID, 50)
	flaggedPosts, _ := d.GetUserFlaggedPosts(targetID, 50)

	var isSpamMatch bool
	var matchedKws []string
	if profile != nil {
		dbSnippets, _ := d.GetSpamSnippetStrings()
		allKws := append([]string{}, extraKeywords...)
		allKws = append(allKws, dbSnippets...)
		isSpamMatch, matchedKws = MatchSpamBioProfile(profile, allKws...)
	}

	isSuperAdmin := (superAdminID != 0 && targetID == superAdminID)

	return &UserFullDump{
		User:           user,
		Profile:        profile,
		MessageCount:   msgCount,
		RecentMessages: recentMsgs,
		ReputationLogs: repLogs,
		FlaggedPosts:   flaggedPosts,
		IsSpamBioMatch: isSpamMatch,
		MatchedBioKws:  matchedKws,
		IsSuperAdmin:   isSuperAdmin,
	}, nil
}

// MatchSpamBioProfile checks all text fields of a user profile (bio, personal channel title/username, business intro) for spam keywords.
func MatchSpamBioProfile(p *UserProfile, additionalKeywords ...string) (bool, []string) {
	if p == nil {
		return false, nil
	}
	var texts []string
	if strings.TrimSpace(p.Bio) != "" {
		texts = append(texts, p.Bio)
	}
	if strings.TrimSpace(p.PersonalChatTitle) != "" {
		texts = append(texts, p.PersonalChatTitle)
	}
	if strings.TrimSpace(p.PersonalChatUsername) != "" {
		texts = append(texts, p.PersonalChatUsername)
	}
	if strings.TrimSpace(p.BusinessIntro) != "" {
		texts = append(texts, p.BusinessIntro)
	}
	if len(texts) == 0 {
		return false, nil
	}
	combined := strings.Join(texts, " | ")
	return MatchSpamBioAll(combined, additionalKeywords...)
}

// FormatUserDump formats a UserFullDump into a detailed Markdown report.
func FormatUserDump(dump *UserFullDump) string {
	if dump == nil || dump.User == nil {
		return "*(No user data)*\n"
	}

	var sb strings.Builder
	u := dump.User

	handleStr := "-"
	if u.Username != "" {
		handleStr = "@" + u.Username
	}
	fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if fullName == "" {
		fullName = "-"
	}

	role := "Regular User"
	if dump.IsSuperAdmin {
		role = "👑 Super Admin"
	} else if u.IsAdmin {
		role = "🛡️ Bot Administrator"
	}

	status := "🟢 Active / Unbanned"
	if u.IsBanned {
		status = "🚫 Banned"
	}

	langStr := "-"
	if u.LanguageCode != "" {
		langStr = fmt.Sprintf("`%s`", u.LanguageCode)
	}
	premiumStr := "❌ No"
	if u.IsPremium {
		premiumStr = "⭐ Yes (Telegram Premium)"
	}

	sb.WriteString(fmt.Sprintf("# 👤 Telegram User Dossier: %s (ID: `%d`)\n\n", handleStr, u.UserID))

	sb.WriteString("## 📌 Account Overview\n")
	sb.WriteString(fmt.Sprintf("- **User ID**: `%d`\n", u.UserID))
	sb.WriteString(fmt.Sprintf("- **Username**: %s\n", handleStr))
	sb.WriteString(fmt.Sprintf("- **Display Name**: %s\n", fullName))
	sb.WriteString(fmt.Sprintf("- **Reputation**: `%d`\n", u.Reputation))
	sb.WriteString(fmt.Sprintf("- **Warnings**: `%d`\n", u.WarnCount))
	sb.WriteString(fmt.Sprintf("- **Language Code**: %s\n", langStr))
	sb.WriteString(fmt.Sprintf("- **Telegram Premium**: %s\n", premiumStr))
	sb.WriteString(fmt.Sprintf("- **Role**: %s\n", role))
	sb.WriteString(fmt.Sprintf("- **Status**: %s\n", status))
	sb.WriteString(fmt.Sprintf("- **First Seen (Created At)**: %s\n", u.CreatedAt.Format("2006-01-02 15:04:05 MST")))
	sb.WriteString(fmt.Sprintf("- **Last Active (Updated At)**: %s\n\n", u.UpdatedAt.Format("2006-01-02 15:04:05 MST")))

	sb.WriteString("## 📋 Telegram Profile & Bio\n")
	if dump.Profile == nil {
		sb.WriteString("*(No profile cached in database - use `/fetchprofile` or `gogcbot backfill-profiles`)*\n\n")
	} else if dump.Profile.NotFound {
		sb.WriteString("⚠️ **Profile Status**: Not found on Telegram (marked to skip redundant re-fetching)\n")
		sb.WriteString(fmt.Sprintf("- **Last Attempted**: %s\n\n", dump.Profile.FetchedAt.Format("2006-01-02 15:04:05 MST")))
	} else {
		p := dump.Profile
		sb.WriteString("✅ **Profile Status**: Cached in Database\n")
		photoStr := "❌ No photo"
		if p.HasPhoto {
			photoStr = fmt.Sprintf("✅ Yes (%d photos)", p.PhotoCount)
		}
		sb.WriteString(fmt.Sprintf("- **Profile Photo**: %s\n", photoStr))
		if p.PhotoFileID != "" {
			sb.WriteString(fmt.Sprintf("- **Photo File ID (Large)**: `%s`\n", p.PhotoFileID))
		}
		if p.PhotoSmallFileID != "" {
			sb.WriteString(fmt.Sprintf("- **Photo File ID (Small)**: `%s`\n", p.PhotoSmallFileID))
		}
		if p.HasPrivateForwards {
			sb.WriteString("- **Private Forwards**: 🔒 Restricted by user\n")
		}
		if p.PersonalChatTitle != "" || p.PersonalChatUsername != "" {
			chatTitle := p.PersonalChatTitle
			if chatTitle == "" {
				chatTitle = "Personal Channel"
			}
			chatHandle := ""
			if p.PersonalChatUsername != "" {
				chatHandle = fmt.Sprintf(" (@%s)", p.PersonalChatUsername)
			}
			sb.WriteString(fmt.Sprintf("- **Personal Channel**: `%s`%s\n", chatTitle, chatHandle))
		}
		if p.BusinessIntro != "" {
			sb.WriteString(fmt.Sprintf("- **Business Intro**: `%s`\n", escapeMarkdownCell(p.BusinessIntro)))
		}
		sb.WriteString(fmt.Sprintf("- **Last Fetched**: %s\n", p.FetchedAt.Format("2006-01-02 15:04:05 MST")))

		spamFilterStr := "🟢 Clean"
		if dump.IsSpamBioMatch || len(dump.MatchedBioKws) > 0 {
			spamFilterStr = fmt.Sprintf("🚨 Spam Match [Matched: `%s`]", strings.Join(dump.MatchedBioKws, ", "))
		}
		sb.WriteString(fmt.Sprintf("- **Spam Profile Filter**: %s\n", spamFilterStr))

		if strings.TrimSpace(p.Bio) != "" {
			sb.WriteString("- **Bio**:\n```\n")
			sb.WriteString(p.Bio)
			sb.WriteString("\n```\n\n")
		}

		if strings.TrimSpace(p.RawJSON) != "" {
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, []byte(p.RawJSON), "", "  "); err == nil {
				sb.WriteString("- **Raw Telegram Profile JSON**:\n```json\n")
				sb.WriteString(prettyJSON.String())
				sb.WriteString("\n```\n\n")
			} else {
				sb.WriteString("- **Raw Telegram Profile JSON**:\n```json\n")
				sb.WriteString(p.RawJSON)
				sb.WriteString("\n```\n\n")
			}
		}
	}

	// Messages
	sb.WriteString(fmt.Sprintf("## 💬 Activity & Messages (Total Logged: %d)\n", dump.MessageCount))
	if len(dump.RecentMessages) == 0 {
		sb.WriteString("*(No logged messages for this user)*\n\n")
	} else {
		sb.WriteString("| # | Chat ID | Message ID | Media | Links | Timestamp | Message Content |\n")
		sb.WriteString("|---|---|---|---|---|---|---|\n")
		for i, m := range dump.RecentMessages {
			mediaStr := "No"
			if m.HasMedia {
				mediaStr = "Yes"
			}
			linkStr := "No"
			if m.HasLinks {
				linkStr = "Yes"
			}
			msgText := escapeMarkdownCell(truncateString(m.Text, 80))
			sb.WriteString(fmt.Sprintf("| %d | `%d` | `%d` | %s | %s | `%s` | %s |\n",
				i+1, m.ChatID, m.MessageID, mediaStr, linkStr, m.CreatedAt.Format("2006-01-02 15:04:05 MST"), msgText))
		}
		sb.WriteString("\n")
	}

	// Reputation Logs
	sb.WriteString(fmt.Sprintf("## ⭐ Reputation Audit Trail (Total Logs: %d)\n", len(dump.ReputationLogs)))
	if len(dump.ReputationLogs) == 0 {
		sb.WriteString("*(No reputation changes recorded for this user)*\n\n")
	} else {
		sb.WriteString("| # | Delta | Reason | Action By | Timestamp |\n")
		sb.WriteString("|---|---|---|---|---|\n")
		for i, rl := range dump.ReputationLogs {
			deltaStr := fmt.Sprintf("%+d", rl.ChangeAmount)
			byUserStr := fmt.Sprintf("`%d`", rl.ByUserID)
			if rl.ByUserID == 0 {
				byUserStr = "System (`0`)"
			}
			reasonStr := escapeMarkdownCell(rl.Reason)
			sb.WriteString(fmt.Sprintf("| %d | `%s` | %s | %s | `%s` |\n",
				i+1, deltaStr, reasonStr, byUserStr, rl.CreatedAt.Format("2006-01-02 15:04:05 MST")))
		}
		sb.WriteString("\n")
	}

	// Flagged Posts & Moderation
	sb.WriteString(fmt.Sprintf("## 🚨 Moderation & Flagged Posts (Total Records: %d)\n", len(dump.FlaggedPosts)))
	if len(dump.FlaggedPosts) == 0 {
		sb.WriteString("*(No moderation flags or trigger bans for this user)*\n\n")
	} else {
		sb.WriteString("| # | Flag ID | Chat ID | Message ID | Status | Reason | Resolved By | Timestamp |\n")
		sb.WriteString("|---|---|---|---|---|---|---|---|\n")
		for i, fp := range dump.FlaggedPosts {
			reasonStr := escapeMarkdownCell(fp.Reason)
			statusStr := fp.Status
			if statusStr == "banned" {
				statusStr = "🚫 banned"
			} else if statusStr == "approved" {
				statusStr = "✅ approved"
			} else if statusStr == "deleted" {
				statusStr = "🗑️ deleted"
			}
			resolvedByStr := fmt.Sprintf("`%d`", fp.ResolvedBy)
			sb.WriteString(fmt.Sprintf("| %d | `%d` | `%d` | `%d` | %s | %s | %s | `%s` |\n",
				i+1, fp.ID, fp.GroupChatID, fp.GroupMessageID, statusStr, reasonStr, resolvedByStr, fp.FlaggedAt.Format("2006-01-02 15:04:05 MST")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (d *DB) GetUsersWithoutProfile(limit int) ([]User, error) {
	query := `
		SELECT u.user_id, u.username, u.first_name, u.last_name, u.language_code, u.is_premium, u.reputation, u.warn_count, u.is_banned, u.is_admin, u.created_at, u.updated_at
		FROM users u
		LEFT JOIN user_profiles p ON u.user_id = p.user_id
		WHERE p.user_id IS NULL
		ORDER BY u.reputation DESC, u.created_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.LanguageCode, &u.IsPremium, &u.Reputation, &u.WarnCount, &u.IsBanned, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// GetLowRepUsersForRescan queries unbanned, non-admin users with reputation <= maxRep
// whose profile was fetched before cutoff (or never fetched). If cutoff is zero, the time filter is bypassed.
func (d *DB) GetLowRepUsersForRescan(maxRep int, cutoff time.Time, limit int) ([]User, error) {
	var query string
	var args []interface{}

	if cutoff.IsZero() {
		query = `
			SELECT u.user_id, u.username, u.first_name, u.last_name, u.language_code, u.is_premium, u.reputation, u.warn_count, u.is_banned, u.is_admin, u.created_at, u.updated_at
			FROM users u
			LEFT JOIN user_profiles p ON u.user_id = p.user_id
			WHERE u.is_banned = 0 AND u.is_admin = 0 AND u.reputation <= ?
			ORDER BY u.reputation ASC, u.user_id DESC
		`
		args = append(args, maxRep)
	} else {
		query = `
			SELECT u.user_id, u.username, u.first_name, u.last_name, u.language_code, u.is_premium, u.reputation, u.warn_count, u.is_banned, u.is_admin, u.created_at, u.updated_at
			FROM users u
			LEFT JOIN user_profiles p ON u.user_id = p.user_id
			WHERE u.is_banned = 0 AND u.is_admin = 0 AND u.reputation <= ?
			  AND (p.fetched_at IS NULL OR p.fetched_at < ?)
			ORDER BY u.reputation ASC, u.user_id DESC
		`
		args = append(args, maxRep, cutoff)
	}

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.LanguageCode, &u.IsPremium, &u.Reputation, &u.WarnCount, &u.IsBanned, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (d *DB) GetAllUserProfiles(limit int) ([]UserProfile, error) {
	query := `
		SELECT user_id, username, first_name, last_name, language_code, is_premium, bio,
		       has_private_forwards, personal_chat_title, personal_chat_username, business_intro,
		       photo_file_id, photo_file_unique_id, photo_small_file_id, photo_small_file_unique_id,
		       photo_count, has_photo, not_found, raw_json, fetched_at, updated_at
		FROM user_profiles
		ORDER BY updated_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := d.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []UserProfile
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(
			&p.UserID, &p.Username, &p.FirstName, &p.LastName, &p.LanguageCode, &p.IsPremium, &p.Bio,
			&p.HasPrivateForwards, &p.PersonalChatTitle, &p.PersonalChatUsername, &p.BusinessIntro,
			&p.PhotoFileID, &p.PhotoFileUniqueID, &p.PhotoSmallFileID, &p.PhotoSmallFileUniqueID,
			&p.PhotoCount, &p.HasPhoto, &p.NotFound, &p.RawJSON, &p.FetchedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

func (d *DB) GetUserProfileCount() (int, error) {
	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM user_profiles`).Scan(&count)
	return count, err
}

// User Directory Report Types and Methods

// UserReportItem holds comprehensive metadata for good and bad users.
type UserReportItem struct {
	UserID       int64     `json:"user_id"`
	Username     string    `json:"username"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	Reputation   int       `json:"reputation"`
	WarnCount    int       `json:"warn_count"`
	IsBanned     bool      `json:"is_banned"`
	IsAdmin      bool      `json:"is_admin"`
	IsSuperAdmin bool      `json:"is_super_admin"`
	Role         string    `json:"role"`
	MessageCount int       `json:"message_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Ban / Moderation metadata (for bad users)
	IsManualBan  bool       `json:"is_manual_ban"`  // Banned manually by moderator or command
	IsTriggerBan bool       `json:"is_trigger_ban"` // Banned by automated detection trigger
	BanType      string     `json:"ban_type"`       // "Manual (Moderator)", "Manual (Command)", "Automated Trigger", "Banned"
	BannedBy     int64      `json:"banned_by,omitempty"`
	BannedByName string     `json:"banned_by_name,omitempty"`
	BanReason    string     `json:"ban_reason,omitempty"`
	BannedAt     *time.Time `json:"banned_at,omitempty"`
	TriggerName  string     `json:"trigger_name,omitempty"`

	// Good user metadata
	ApprovalCount int    `json:"approval_count"` // Number of moderator approvals received
	ShieldyBonus  bool   `json:"shieldy_bonus"`  // True if received Shieldy verification bonus
	Notes         string `json:"notes,omitempty"`
}

// UserReportOptions configures user directory report generation.
type UserReportOptions struct {
	SuperAdminID   int64  `json:"super_admin_id"`
	GoodOnly       bool   `json:"good_only"`
	BadOnly        bool   `json:"bad_only"`
	ManualBansOnly bool   `json:"manual_bans_only"`
	Limit          int    `json:"limit"`
	DatabaseName   string `json:"database_name"`
}

// GetUserDirectoryReport retrieves and classifies all known users into good and bad users with rich metadata.
func (d *DB) GetUserDirectoryReport(superAdminID int64) (goodUsers []UserReportItem, badUsers []UserReportItem, err error) {
	// 1. Fetch all users
	rows, err := d.Query(`
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users
		ORDER BY reputation DESC, created_at DESC
	`)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var allUsers []User
	userMap := make(map[int64]*User)
	for rows.Next() {
		var u User
		var rawCreatedAt, rawUpdatedAt any
		if err := rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.Reputation, &u.WarnCount, &u.IsBanned, &u.IsAdmin, &rawCreatedAt, &rawUpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("failed to scan user: %w", err)
		}
		u.CreatedAt = parseTime(rawCreatedAt)
		u.UpdatedAt = parseTime(rawUpdatedAt)
		allUsers = append(allUsers, u)
		userCopy := u
		userMap[u.UserID] = &userCopy
	}

	// 2. Fetch message counts per user
	msgCounts := make(map[int64]int)
	msgRows, err := d.Query(`SELECT user_id, COUNT(*) FROM messages GROUP BY user_id`)
	if err == nil {
		defer msgRows.Close()
		for msgRows.Next() {
			var uid int64
			var cnt int
			if err := msgRows.Scan(&uid, &cnt); err == nil {
				msgCounts[uid] = cnt
			}
		}
	}

	// 3. Fetch flagged posts for ban resolutions
	type flagBanInfo struct {
		reason     string
		resolvedBy int64
		resolvedAt *time.Time
	}
	flagBans := make(map[int64]flagBanInfo)
	fpRows, err := d.Query(`
		SELECT user_id, reason, status, resolved_by, resolved_at
		FROM flagged_posts
		ORDER BY id ASC
	`)
	if err == nil {
		defer fpRows.Close()
		for fpRows.Next() {
			var uid int64
			var reason, status string
			var resolvedBy int64
			var rawResolvedAt any
			if err := fpRows.Scan(&uid, &reason, &status, &resolvedBy, &rawResolvedAt); err == nil {
				if status == "banned" {
					var resAt *time.Time
					if t := parseTime(rawResolvedAt); !t.IsZero() {
						resAt = &t
					}
					flagBans[uid] = flagBanInfo{
						reason:     reason,
						resolvedBy: resolvedBy,
						resolvedAt: resAt,
					}
				}
			}
		}
	}

	// 4. Fetch reputation logs for approvals, shieldy verification, and ban logs
	approvalCounts := make(map[int64]int)
	shieldyBonus := make(map[int64]bool)
	repBanLogs := make(map[int64]RepLog)

	rlRows, err := d.Query(`
		SELECT id, user_id, change_amount, reason, by_user_id, created_at
		FROM reputation_logs
		ORDER BY id ASC
	`)
	if err == nil {
		defer rlRows.Close()
		for rlRows.Next() {
			var rl RepLog
			var rawCreatedAt any
			if err := rlRows.Scan(&rl.ID, &rl.UserID, &rl.ChangeAmount, &rl.Reason, &rl.ByUserID, &rawCreatedAt); err == nil {
				rl.CreatedAt = parseTime(rawCreatedAt)

				if strings.HasPrefix(rl.Reason, "Approved by moderator") {
					approvalCounts[rl.UserID]++
				}
				if strings.HasPrefix(rl.Reason, "Shieldy verification") {
					shieldyBonus[rl.UserID] = true
				}
				if strings.Contains(rl.Reason, "Banned by moderator") ||
					strings.Contains(rl.Reason, "Banned by command") ||
					strings.Contains(rl.Reason, "trigger") ||
					strings.Contains(rl.Reason, "Detection trigger") {
					repBanLogs[rl.UserID] = rl
				}
			}
		}
	}

	// 5. Categorize each user into Good or Bad user
	for _, u := range allUsers {
		item := UserReportItem{
			UserID:        u.UserID,
			Username:      u.Username,
			FirstName:     u.FirstName,
			LastName:      u.LastName,
			Reputation:    u.Reputation,
			WarnCount:     u.WarnCount,
			IsBanned:      u.IsBanned,
			IsAdmin:       u.IsAdmin,
			MessageCount:  msgCounts[u.UserID],
			CreatedAt:     u.CreatedAt,
			UpdatedAt:     u.UpdatedAt,
			ApprovalCount: approvalCounts[u.UserID],
			ShieldyBonus:  shieldyBonus[u.UserID],
		}

		if superAdminID != 0 && u.UserID == superAdminID {
			item.IsSuperAdmin = true
			item.Role = "Super Admin 👑"
		} else if u.IsAdmin {
			item.Role = "Bot Admin 🛡️"
		} else if item.ApprovalCount > 0 {
			item.Role = "Approved Member ✅"
		} else if item.ShieldyBonus {
			item.Role = "Verified Member 🛡️"
		} else {
			item.Role = "Member"
		}

		if u.IsBanned {
			// Populate ban metadata
			if fb, ok := flagBans[u.UserID]; ok {
				if fb.resolvedBy == 0 || strings.Contains(fb.reason, "trigger") || strings.Contains(fb.reason, "Detection trigger") {
					item.IsTriggerBan = true
					item.BanType = "Automated Trigger"
					item.BanReason = fb.reason
					item.TriggerName = extractTriggerName(fb.reason)
					item.BannedAt = fb.resolvedAt
				} else {
					item.IsManualBan = true
					item.BanType = "Manual (Moderator)"
					item.BannedBy = fb.resolvedBy
					item.BanReason = fb.reason
					item.BannedAt = fb.resolvedAt
				}
			} else if rl, ok := repBanLogs[u.UserID]; ok {
				if strings.Contains(rl.Reason, "Banned by moderator") {
					item.IsManualBan = true
					item.BanType = "Manual (Moderator)"
					item.BannedBy = rl.ByUserID
					item.BanReason = "Banned by moderator"
					item.BannedAt = &rl.CreatedAt
				} else if strings.Contains(rl.Reason, "Banned by command") {
					item.IsManualBan = true
					item.BanType = "Manual (Command)"
					item.BannedBy = rl.ByUserID
					item.BanReason = "Banned via admin command"
					item.BannedAt = &rl.CreatedAt
				} else if strings.Contains(rl.Reason, "trigger") || strings.Contains(rl.Reason, "Detection trigger") {
					item.IsTriggerBan = true
					item.BanType = "Automated Trigger"
					item.BanReason = rl.Reason
					item.TriggerName = extractTriggerName(rl.Reason)
					item.BannedAt = &rl.CreatedAt
				} else {
					item.BanType = "Banned"
					item.BanReason = rl.Reason
					item.BannedAt = &rl.CreatedAt
				}
			} else {
				item.BanType = "Banned"
				item.BanReason = "Banned"
				item.BannedAt = &u.UpdatedAt
			}

			// Format moderator name if banned by someone
			if item.BannedBy != 0 {
				if mod, ok := userMap[item.BannedBy]; ok {
					if mod.Username != "" {
						item.BannedByName = fmt.Sprintf("@%s (`%d`)", mod.Username, item.BannedBy)
					} else {
						modName := strings.TrimSpace(mod.FirstName + " " + mod.LastName)
						if modName == "" {
							modName = fmt.Sprintf("User %d", item.BannedBy)
						}
						item.BannedByName = fmt.Sprintf("%s (`%d`)", modName, item.BannedBy)
					}
				} else {
					item.BannedByName = fmt.Sprintf("`%d`", item.BannedBy)
				}
			} else if item.IsTriggerBan {
				item.BannedByName = "System / Detector"
			}

			badUsers = append(badUsers, item)
		} else {
			// Good user notes
			var notes []string
			if item.ApprovalCount > 0 {
				notes = append(notes, fmt.Sprintf("Approved by mod (x%d)", item.ApprovalCount))
			}
			if item.ShieldyBonus {
				notes = append(notes, "Shieldy verified")
			}
			if len(notes) > 0 {
				item.Notes = strings.Join(notes, "; ")
			}

			goodUsers = append(goodUsers, item)
		}
	}

	// Sort good users: SuperAdmin first, then BotAdmin, then Reputation DESC, MessageCount DESC, CreatedAt DESC
	sort.SliceStable(goodUsers, func(i, j int) bool {
		if goodUsers[i].IsSuperAdmin != goodUsers[j].IsSuperAdmin {
			return goodUsers[i].IsSuperAdmin
		}
		if goodUsers[i].IsAdmin != goodUsers[j].IsAdmin {
			return goodUsers[i].IsAdmin
		}
		if goodUsers[i].Reputation != goodUsers[j].Reputation {
			return goodUsers[i].Reputation > goodUsers[j].Reputation
		}
		if goodUsers[i].MessageCount != goodUsers[j].MessageCount {
			return goodUsers[i].MessageCount > goodUsers[j].MessageCount
		}
		return goodUsers[i].CreatedAt.After(goodUsers[j].CreatedAt)
	})

	// Sort bad users: Manual bans first, then Trigger bans; within group, most recently banned first
	sort.SliceStable(badUsers, func(i, j int) bool {
		if badUsers[i].IsManualBan != badUsers[j].IsManualBan {
			return badUsers[i].IsManualBan
		}
		if badUsers[i].IsTriggerBan != badUsers[j].IsTriggerBan {
			return badUsers[i].IsTriggerBan
		}
		var tI, tJ time.Time
		if badUsers[i].BannedAt != nil {
			tI = *badUsers[i].BannedAt
		} else {
			tI = badUsers[i].UpdatedAt
		}
		if badUsers[j].BannedAt != nil {
			tJ = *badUsers[j].BannedAt
		} else {
			tJ = badUsers[j].UpdatedAt
		}
		return tI.After(tJ)
	})

	return goodUsers, badUsers, nil
}

// GenerateUserDirectoryMarkdown formats the user directory report into structured GitHub Flavored Markdown.
func GenerateUserDirectoryMarkdown(goodUsers []UserReportItem, badUsers []UserReportItem, opts UserReportOptions) string {
	var sb strings.Builder

	dbName := opts.DatabaseName
	if dbName == "" {
		dbName = "SQLite Database"
	}

	totalUsers := len(goodUsers) + len(badUsers)

	// Partition bad users for detailed reporting
	var manualBans []UserReportItem
	var triggerBans []UserReportItem
	var otherBans []UserReportItem
	for _, b := range badUsers {
		if b.IsManualBan {
			manualBans = append(manualBans, b)
		} else if b.IsTriggerBan {
			triggerBans = append(triggerBans, b)
		} else {
			otherBans = append(otherBans, b)
		}
	}

	sb.WriteString("# 📋 GoGCBot User Directory\n\n")
	sb.WriteString(fmt.Sprintf("- **Generated At**: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	sb.WriteString(fmt.Sprintf("- **Database**: `%s`\n", dbName))
	sb.WriteString(fmt.Sprintf("- **Total Tracked Users**: %d (%d Known Good, %d Known Bad [%d Manual Moderator Bans, %d Automated Trigger Bans])\n\n",
		totalUsers, len(goodUsers), len(badUsers), len(manualBans), len(triggerBans)))

	// Section 1: Known Good Users
	if !opts.BadOnly && !opts.ManualBansOnly {
		displayGood := goodUsers
		if opts.Limit > 0 && len(displayGood) > opts.Limit {
			displayGood = displayGood[:opts.Limit]
		}

		sb.WriteString(fmt.Sprintf("## 🟢 Known Good Users (%d)\n\n", len(goodUsers)))
		if len(displayGood) == 0 {
			sb.WriteString("*No users in this category.*\n\n")
		} else {
			sb.WriteString("| # | User ID | Username | Display Name | Role | Rep | Warns | Messages | Notes | First Seen | Last Active |\n")
			sb.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
			for i, u := range displayGood {
				username := "-"
				if u.Username != "" {
					username = "@" + escapeMarkdownCell(u.Username)
				}
				name := strings.TrimSpace(u.FirstName + " " + u.LastName)
				if name == "" {
					name = "-"
				}
				notes := u.Notes
				if notes == "" {
					notes = "-"
				}

				firstSeen := "-"
				if !u.CreatedAt.IsZero() {
					firstSeen = u.CreatedAt.Format("2006-01-02 15:04")
				}
				lastActive := "-"
				if !u.UpdatedAt.IsZero() {
					lastActive = u.UpdatedAt.Format("2006-01-02 15:04")
				}

				sb.WriteString(fmt.Sprintf("| %d | `%d` | %s | %s | %s | %d | %d | %d | %s | %s | %s |\n",
					i+1, u.UserID, username, escapeMarkdownCell(name), u.Role, u.Reputation, u.WarnCount, u.MessageCount,
					escapeMarkdownCell(notes), firstSeen, lastActive))
			}
			sb.WriteString("\n")
		}
	}

	// Section 2: Known Bad Users
	if !opts.GoodOnly {
		sb.WriteString(fmt.Sprintf("## 🔴 Known Bad Users (%d)\n\n", len(badUsers)))

		// Sub-section 2a: Manually Banned by Moderators
		displayManual := manualBans
		if opts.Limit > 0 && len(displayManual) > opts.Limit {
			displayManual = displayManual[:opts.Limit]
		}
		sb.WriteString(fmt.Sprintf("### 🔨 Manually Banned by Moderators (%d)\n\n", len(manualBans)))
		sb.WriteString("> Users reviewed and manually banned by moderators or admin commands.\n\n")

		if len(displayManual) == 0 {
			sb.WriteString("*No users in this category.*\n\n")
		} else {
			sb.WriteString("| # | User ID | Username | Display Name | Banned By | Ban Reason | Rep | Warns | Messages | Banned Date |\n")
			sb.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
			for i, u := range displayManual {
				username := "-"
				if u.Username != "" {
					username = "@" + escapeMarkdownCell(u.Username)
				}
				name := strings.TrimSpace(u.FirstName + " " + u.LastName)
				if name == "" {
					name = "-"
				}
				bannedBy := u.BannedByName
				if bannedBy == "" {
					bannedBy = "-"
				}
				banReason := u.BanReason
				if banReason == "" {
					banReason = "Banned by moderator"
				}
				banDate := "-"
				if u.BannedAt != nil && !u.BannedAt.IsZero() {
					banDate = u.BannedAt.Format("2006-01-02 15:04")
				} else if !u.UpdatedAt.IsZero() {
					banDate = u.UpdatedAt.Format("2006-01-02 15:04")
				}

				sb.WriteString(fmt.Sprintf("| %d | `%d` | %s | %s | %s | %s | %d | %d | %d | %s |\n",
					i+1, u.UserID, username, escapeMarkdownCell(name), bannedBy, escapeMarkdownCell(banReason),
					u.Reputation, u.WarnCount, u.MessageCount, banDate))
			}
			sb.WriteString("\n")
		}

		if !opts.ManualBansOnly {
			// Sub-section 2b: Automatically Banned by Detection Triggers
			displayTrigger := triggerBans
			if opts.Limit > 0 && len(displayTrigger) > opts.Limit {
				displayTrigger = displayTrigger[:opts.Limit]
			}
			sb.WriteString(fmt.Sprintf("### 🤖 Automatically Banned by Detection Triggers (%d)\n\n", len(triggerBans)))
			sb.WriteString("> Users caught and banned by automated spam and detection triggers.\n\n")

			if len(displayTrigger) == 0 {
				sb.WriteString("*No users in this category.*\n\n")
			} else {
				sb.WriteString("| # | User ID | Username | Display Name | Trigger | Reason | Rep | Warns | Messages | Trigger Date |\n")
				sb.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
				for i, u := range displayTrigger {
					username := "-"
					if u.Username != "" {
						username = "@" + escapeMarkdownCell(u.Username)
					}
					name := strings.TrimSpace(u.FirstName + " " + u.LastName)
					if name == "" {
						name = "-"
					}
					trigger := u.TriggerName
					if trigger == "" {
						trigger = "detector"
					}
					reason := u.BanReason
					if reason == "" {
						reason = "Trigger ban"
					}
					triggerDate := "-"
					if u.BannedAt != nil && !u.BannedAt.IsZero() {
						triggerDate = u.BannedAt.Format("2006-01-02 15:04")
					} else if !u.UpdatedAt.IsZero() {
						triggerDate = u.UpdatedAt.Format("2006-01-02 15:04")
					}

					sb.WriteString(fmt.Sprintf("| %d | `%d` | %s | %s | `%s` | %s | %d | %d | %d | %s |\n",
						i+1, u.UserID, username, escapeMarkdownCell(name), escapeMarkdownCell(trigger), escapeMarkdownCell(reason),
						u.Reputation, u.WarnCount, u.MessageCount, triggerDate))
				}
				sb.WriteString("\n")
			}

			// Sub-section 2c: Other Banned Users (if any)
			if len(otherBans) > 0 {
				displayOther := otherBans
				if opts.Limit > 0 && len(displayOther) > opts.Limit {
					displayOther = displayOther[:opts.Limit]
				}
				sb.WriteString(fmt.Sprintf("### 🚫 Other Banned Users (%d)\n\n", len(otherBans)))
				sb.WriteString("| # | User ID | Username | Display Name | Reason | Rep | Warns | Messages | Date |\n")
				sb.WriteString("|---|---|---|---|---|---|---|---|---|\n")
				for i, u := range displayOther {
					username := "-"
					if u.Username != "" {
						username = "@" + escapeMarkdownCell(u.Username)
					}
					name := strings.TrimSpace(u.FirstName + " " + u.LastName)
					if name == "" {
						name = "-"
					}
					date := "-"
					if u.BannedAt != nil && !u.BannedAt.IsZero() {
						date = u.BannedAt.Format("2006-01-02 15:04")
					} else if !u.UpdatedAt.IsZero() {
						date = u.UpdatedAt.Format("2006-01-02 15:04")
					}

					sb.WriteString(fmt.Sprintf("| %d | `%d` | %s | %s | %s | %d | %d | %d | %s |\n",
						i+1, u.UserID, username, escapeMarkdownCell(name), escapeMarkdownCell(u.BanReason),
						u.Reputation, u.WarnCount, u.MessageCount, date))
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// Helpers

func parseTime(val any) time.Time {
	if val == nil {
		return time.Time{}
	}
	switch t := val.(type) {
	case time.Time:
		return t
	case *time.Time:
		if t != nil {
			return *t
		}
		return time.Time{}
	case string:
		return parseTimeString(t)
	case []byte:
		return parseTimeString(string(t))
	default:
		return time.Time{}
	}
}

func parseTimeString(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if idx := strings.Index(s, " m="); idx != -1 {
		s = s[:idx]
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700 -0700",
		"2006-01-02 15:04:05.999999999 +0800 +08",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700 -0700",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func escapeMarkdownCell(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

func extractTriggerName(reason string) string {
	start := strings.Index(reason, "(")
	end := strings.Index(reason, ")")
	if start != -1 && end != -1 && end > start+1 {
		return reason[start+1 : end]
	}
	if strings.Contains(reason, "cjk") {
		return "new_user_cjk"
	}
	if strings.Contains(reason, "chinese") {
		return "new_user_chinese"
	}
	return "detection_trigger"
}

// Spam Bio Tracking Types and Methods

// SpamBioKeywords contains common promotional, discount card, gift card, and syndicate scam terms seen in Telegram bio spam.
var SpamBioKeywords = []string{
	"锦鲤代发",
	"代发",
	"油卡",
	"础油卡",
	"加油卡",
	"中石化",
	"中石油",
	"E卡",
	"e卡",
	"京东E卡",
	"京东卡",
	"沃尔玛",
	"永辉",
	"携程",
	"天猫",
	"苹果礼品卡",
	"礼品卡",
	"Steam",
	"steam",
	"6折",
	"7折",
	"8折",
	"9折",
	"折础",
	"慢充",
	"代充",
	"代缴",
	"代付",
	"刷单",
	"兼职",
	"日结",
	"外汇盘",
	"币盘",
	"NFT盘",
	"商城盘",
	"模特视频",
	"六百一天",
	"六佰一天",
	"六栢o壹天",
	"無風險",
	"无风险",
	"演员来",
	"出卡",
	"收卡",
	"跑分",
	"承兑",
	"精准粉",
	"引流",
	"盘口",
	"包赔",
	"日赚",
	"月入",
}

// MatchSpamBio checks if a user bio matches known spam/marketing/syndicate keywords or custom filters.
func MatchSpamBio(bio string, customKeywords ...string) (bool, []string) {
	if strings.TrimSpace(bio) == "" {
		return false, nil
	}

	bioLower := strings.ToLower(bio)
	var matched []string
	seen := make(map[string]bool)

	// If specific custom keywords are provided, match against them
	hasCustom := false
	for _, kw := range customKeywords {
		kw = strings.TrimSpace(kw)
		if kw != "" {
			hasCustom = true
			if strings.Contains(bioLower, strings.ToLower(kw)) && !seen[strings.ToLower(kw)] {
				matched = append(matched, kw)
				seen[strings.ToLower(kw)] = true
			}
		}
	}
	if hasCustom {
		return len(matched) > 0, matched
	}

	// Match against default spam keywords
	for _, kw := range SpamBioKeywords {
		if strings.Contains(bioLower, strings.ToLower(kw)) && !seen[strings.ToLower(kw)] {
			matched = append(matched, kw)
			seen[strings.ToLower(kw)] = true
		}
	}

	return len(matched) > 0, matched
}

// MatchSpamBioAll checks if a user bio matches SpamBioKeywords, DB spam snippets, or any additional keywords.
func MatchSpamBioAll(bio string, additionalKeywords ...string) (bool, []string) {
	allKws := append([]string{}, SpamBioKeywords...)
	allKws = append(allKws, additionalKeywords...)
	return MatchSpamBio(bio, allKws...)
}

// UnknownUserItem holds metadata for an unbanned new/unknown user with or without a bio.
type UnknownUserItem struct {
	UserID               int64     `json:"user_id"`
	Username             string    `json:"username"`
	FirstName            string    `json:"first_name"`
	LastName             string    `json:"last_name"`
	LanguageCode         string    `json:"language_code,omitempty"`
	IsPremium            bool      `json:"is_premium,omitempty"`
	Reputation           int       `json:"reputation"`
	WarnCount            int       `json:"warn_count"`
	MessageCount         int       `json:"message_count"`
	Bio                  string    `json:"bio"`
	PersonalChatTitle    string    `json:"personal_chat_title,omitempty"`
	PersonalChatUsername string    `json:"personal_chat_username,omitempty"`
	BusinessIntro        string    `json:"business_intro,omitempty"`
	HasPhoto             bool      `json:"has_photo"`
	PhotoCount           int       `json:"photo_count"`
	CreatedAt            time.Time `json:"created_at"`
	FetchedAt            time.Time `json:"fetched_at"`
	MatchedKeywords      []string  `json:"matched_keywords"`
	IsSpamMatch          bool      `json:"is_spam_match"`
}

// SpamBioUserItem is an alias for UnknownUserItem for backwards compatibility.
type SpamBioUserItem = UnknownUserItem

// DefaultUnknownUserMaxReputation defines the default maximum reputation score (<= 20) for unknown/new users.
const DefaultUnknownUserMaxReputation = 20

// UnknownUserOptions configures filtering for GetUnbannedUnknownUsers.
type UnknownUserOptions struct {
	Keyword            string   `json:"keyword"`
	ConfiguredKeywords []string `json:"configured_keywords"`
	MaxPosts           int      `json:"max_posts"`
	MaxReputation      int      `json:"max_reputation"`
	Limit              int      `json:"limit"`
	DatabaseName       string   `json:"database_name"`
}

// SpamBioOptions is an alias for UnknownUserOptions for backwards compatibility.
type SpamBioOptions = UnknownUserOptions

// GetUnbannedUnknownUsers retrieves unbanned new users (with few or no messages, with or without bios) matching filters.
func (d *DB) GetUnbannedUnknownUsers(opts UnknownUserOptions) ([]UnknownUserItem, error) {
	query := `
		SELECT u.user_id, u.username, u.first_name, u.last_name, u.language_code, u.is_premium,
		       u.reputation, u.warn_count, u.is_admin, u.created_at,
		       COALESCE(p.bio, ''), COALESCE(p.has_photo, 0), COALESCE(p.photo_count, 0),
		       COALESCE(p.fetched_at, u.created_at),
		       COALESCE(p.personal_chat_title, ''), COALESCE(p.personal_chat_username, ''),
		       COALESCE(p.business_intro, ''),
		       (SELECT COUNT(*) FROM messages m WHERE m.user_id = u.user_id) AS msg_count
		FROM users u
		LEFT JOIN user_profiles p ON u.user_id = p.user_id
		WHERE u.is_banned = 0
		ORDER BY u.created_at DESC
	`

	rows, err := d.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query unknown users: %w", err)
	}
	defer rows.Close()

	var results []UnknownUserItem
	for rows.Next() {
		var item UnknownUserItem
		var rawCreatedAt, rawFetchedAt any
		var isAdmin bool

		if err := rows.Scan(
			&item.UserID, &item.Username, &item.FirstName, &item.LastName,
			&item.LanguageCode, &item.IsPremium,
			&item.Reputation, &item.WarnCount, &isAdmin, &rawCreatedAt,
			&item.Bio, &item.HasPhoto, &item.PhotoCount, &rawFetchedAt,
			&item.PersonalChatTitle, &item.PersonalChatUsername, &item.BusinessIntro,
			&item.MessageCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan unknown user item: %w", err)
		}

		if isAdmin {
			continue
		}

		// Filter by reputation (exclude users with high reputation > 20 by default)
		maxRep := opts.MaxReputation
		if maxRep == 0 {
			maxRep = DefaultUnknownUserMaxReputation
		}
		if maxRep > 0 && item.Reputation > maxRep {
			continue
		}

		// Filter by max posts if specified (> 0)
		if opts.MaxPosts > 0 && item.MessageCount > opts.MaxPosts {
			continue
		}

		item.CreatedAt = parseTime(rawCreatedAt)
		item.FetchedAt = parseTime(rawFetchedAt)

		// Profile context for spam keyword evaluation
		prof := &UserProfile{
			UserID:               item.UserID,
			Username:             item.Username,
			FirstName:            item.FirstName,
			LastName:             item.LastName,
			Bio:                  item.Bio,
			PersonalChatTitle:    item.PersonalChatTitle,
			PersonalChatUsername: item.PersonalChatUsername,
			BusinessIntro:        item.BusinessIntro,
		}

		kw := strings.TrimSpace(opts.Keyword)
		if kw != "" {
			kwLower := strings.ToLower(kw)
			var targetTexts []string
			if item.Bio != "" {
				targetTexts = append(targetTexts, item.Bio)
			}
			if item.PersonalChatTitle != "" {
				targetTexts = append(targetTexts, item.PersonalChatTitle)
			}
			if item.PersonalChatUsername != "" {
				targetTexts = append(targetTexts, item.PersonalChatUsername)
			}
			if item.BusinessIntro != "" {
				targetTexts = append(targetTexts, item.BusinessIntro)
			}
			if item.Username != "" {
				targetTexts = append(targetTexts, item.Username)
			}
			name := strings.TrimSpace(item.FirstName + " " + item.LastName)
			if name != "" {
				targetTexts = append(targetTexts, name)
			}

			combined := strings.ToLower(strings.Join(targetTexts, " | "))
			if !strings.Contains(combined, kwLower) {
				continue
			}
			item.MatchedKeywords = []string{kw}
		} else {
			// Check against spam keywords
			dbSnippets, _ := d.GetSpamSnippetStrings()
			allKws := append([]string{}, SpamBioKeywords...)
			allKws = append(allKws, dbSnippets...)
			allKws = append(allKws, opts.ConfiguredKeywords...)
			_, matched := MatchSpamBioProfile(prof, allKws...)
			item.MatchedKeywords = matched
		}
		item.IsSpamMatch = len(item.MatchedKeywords) > 0

		results = append(results, item)
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}

	return results, nil
}

// GetUnbannedSpamBioUsers is an alias for GetUnbannedUnknownUsers for backwards compatibility.
func (d *DB) GetUnbannedSpamBioUsers(opts SpamBioOptions) ([]SpamBioUserItem, error) {
	return d.GetUnbannedUnknownUsers(opts)
}

// GenerateUnknownUsersMarkdown formats the list of unbanned unknown users into GitHub Flavored Markdown.
func GenerateUnknownUsersMarkdown(items []UnknownUserItem, opts UnknownUserOptions) string {
	var sb strings.Builder

	dbName := opts.DatabaseName
	if dbName == "" {
		dbName = "SQLite Database"
	}

	sb.WriteString("# 📋 Unbanned Unknown & New Users\n\n")
	sb.WriteString(fmt.Sprintf("- **Generated At**: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	sb.WriteString(fmt.Sprintf("- **Database**: `%s`\n", dbName))
	if opts.Keyword != "" {
		sb.WriteString(fmt.Sprintf("- **Keyword Filter**: `%s`\n", opts.Keyword))
	} else {
		sb.WriteString("- **Keyword Filter**: *(none - matched all unknown/new users)*\n")
	}
	if opts.MaxPosts > 0 {
		sb.WriteString(fmt.Sprintf("- **Max Logged Posts**: `%d`\n", opts.MaxPosts))
	}
	maxRep := opts.MaxReputation
	if maxRep == 0 {
		maxRep = DefaultUnknownUserMaxReputation
	}
	if maxRep > 0 {
		sb.WriteString(fmt.Sprintf("- **Max Reputation**: `%d`\n", maxRep))
	}
	sb.WriteString(fmt.Sprintf("- **Total Matched Users**: %d\n\n", len(items)))

	if len(items) == 0 {
		sb.WriteString("*No unbanned unknown users matching the criteria found.*\n")
		return sb.String()
	}

	sb.WriteString("| # | User ID | Username | Display Name | Rep | Posts | Spam Match | Matched Keywords | Bio / Profile Snippet | Action |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")

	for i, u := range items {
		username := "-"
		if u.Username != "" {
			username = "@" + escapeMarkdownCell(u.Username)
		}
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = "-"
		}
		kwStr := "-"
		if len(u.MatchedKeywords) > 0 {
			kwStr = strings.Join(u.MatchedKeywords, ", ")
		}
		spamMatchStr := "No"
		if u.IsSpamMatch || len(u.MatchedKeywords) > 0 {
			spamMatchStr = "⚠️ YES"
		}

		profileSnippet := u.Bio
		if profileSnippet == "" && u.PersonalChatTitle != "" {
			profileSnippet = "[Chan] " + u.PersonalChatTitle
		} else if profileSnippet == "" && u.BusinessIntro != "" {
			profileSnippet = "[Biz] " + u.BusinessIntro
		}
		if profileSnippet == "" {
			profileSnippet = "-"
		}
		bioSnippet := escapeMarkdownCell(truncateString(profileSnippet, 60))

		sb.WriteString(fmt.Sprintf(
			"| %d | `%d` | %s | %s | %d | %d | %s | `%s` | %s | `/ban %d` |\n",
			i+1, u.UserID, username, escapeMarkdownCell(name), u.Reputation, u.MessageCount,
			spamMatchStr, escapeMarkdownCell(kwStr), bioSnippet, u.UserID,
		))
	}

	return sb.String()
}

// GenerateSpamBioMarkdown is an alias for GenerateUnknownUsersMarkdown for backwards compatibility.
func GenerateSpamBioMarkdown(items []SpamBioUserItem, opts SpamBioOptions) string {
	return GenerateUnknownUsersMarkdown(items, opts)
}

func truncateString(s string, maxLen int) string {
	s = strings.ToValidUTF8(s, "")
	runes := []rune(s)
	if maxLen <= 0 || len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// AddSpamSnippet inserts a spam snippet into the spam_snippets table.
func (d *DB) AddSpamSnippet(snippet string, category string) error {
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return fmt.Errorf("snippet cannot be empty")
	}
	if category == "" {
		category = "spam"
	}
	now := time.Now()
	_, err := d.Exec(`
		INSERT INTO spam_snippets (snippet, category, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(snippet) DO UPDATE SET category = excluded.category;
	`, snippet, category, now)
	if err != nil {
		return fmt.Errorf("failed to add spam snippet '%s': %w", snippet, err)
	}
	return nil
}

// RemoveSpamSnippet deletes a snippet by exact text or ID string.
func (d *DB) RemoveSpamSnippet(snippet string) error {
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return fmt.Errorf("snippet cannot be empty")
	}
	_, err := d.Exec(`DELETE FROM spam_snippets WHERE snippet = ? OR id = ?;`, snippet, snippet)
	if err != nil {
		return fmt.Errorf("failed to remove spam snippet '%s': %w", snippet, err)
	}
	return nil
}

// GetAllSpamSnippets returns all snippets stored in the spam_snippets table.
func (d *DB) GetAllSpamSnippets() ([]SpamSnippet, error) {
	rows, err := d.Query(`SELECT id, snippet, category, created_at FROM spam_snippets ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query spam snippets: %w", err)
	}
	defer rows.Close()

	var snippets []SpamSnippet
	for rows.Next() {
		var s SpamSnippet
		var rawCreatedAt any
		if err := rows.Scan(&s.ID, &s.Snippet, &s.Category, &rawCreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan spam snippet: %w", err)
		}
		s.CreatedAt = parseTime(rawCreatedAt)
		snippets = append(snippets, s)
	}
	return snippets, nil
}

// GetSpamSnippetStrings returns a slice of all snippet strings in the database.
func (d *DB) GetSpamSnippetStrings() ([]string, error) {
	snippets, err := d.GetAllSpamSnippets()
	if err != nil {
		return nil, err
	}
	var res []string
	for _, s := range snippets {
		if strings.TrimSpace(s.Snippet) != "" {
			res = append(res, s.Snippet)
		}
	}
	return res, nil
}

// SyncSpamSnippets syncs and populates a slice of snippet strings into the spam_snippets table.
func (d *DB) SyncSpamSnippets(snippets []string) error {
	for _, snip := range snippets {
		snip = strings.TrimSpace(snip)
		if snip != "" {
			if err := d.AddSpamSnippet(snip, "spam"); err != nil {
				return err
			}
		}
	}
	return nil
}
