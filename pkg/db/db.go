package db

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	path string
}

type User struct {
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	FirstName  string    `json:"first_name"`
	LastName   string    `json:"last_name"`
	Reputation int       `json:"reputation"`
	WarnCount  int       `json:"warn_count"`
	IsBanned   bool      `json:"is_banned"`
	IsAdmin    bool      `json:"is_admin"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
	Bio                    string    `json:"bio"`
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
		`CREATE TABLE IF NOT EXISTS user_profiles (
			user_id INTEGER PRIMARY KEY,
			username TEXT NOT NULL DEFAULT '',
			first_name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			bio TEXT NOT NULL DEFAULT '',
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
		bio TEXT NOT NULL DEFAULT '',
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
	`
	_, err := d.Exec(schema)
	if err != nil {
		return err
	}

	// Auto-migration: ensure is_admin column exists for pre-existing databases
	_, _ = d.Exec(`ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT 0;`)
	return nil
}

// User Methods

func (d *DB) GetOrCreateUser(userID int64, username, firstName, lastName string, defaultRep int) (*User, bool, error) {
	now := time.Now()
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		user = User{
			UserID:     userID,
			Username:   username,
			FirstName:  firstName,
			LastName:   lastName,
			Reputation: defaultRep,
			WarnCount:  0,
			IsBanned:   false,
			IsAdmin:    false,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		_, err := d.Exec(`
			INSERT INTO users (user_id, username, first_name, last_name, reputation, warn_count, is_banned, is_admin, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, user.UserID, user.Username, user.FirstName, user.LastName, user.Reputation, user.WarnCount, user.IsBanned, user.IsAdmin, user.CreatedAt, user.UpdatedAt)
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

func (d *DB) GetUserByID(userID int64) (*User, error) {
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
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
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, is_admin, created_at, updated_at
		FROM users WHERE LOWER(TRIM(username, '@ ')) = LOWER(?)
	`, username).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.IsAdmin, &user.CreatedAt, &user.UpdatedAt)
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
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, is_admin, created_at, updated_at
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
		if err := rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.Reputation, &u.WarnCount, &u.IsBanned, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
			user_id, username, first_name, last_name, bio,
			photo_file_id, photo_file_unique_id, photo_small_file_id, photo_small_file_unique_id,
			photo_count, has_photo, not_found, raw_json, fetched_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			username = excluded.username,
			first_name = excluded.first_name,
			last_name = excluded.last_name,
			bio = excluded.bio,
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
	`, p.UserID, p.Username, p.FirstName, p.LastName, p.Bio,
		p.PhotoFileID, p.PhotoFileUniqueID, p.PhotoSmallFileID, p.PhotoSmallFileUniqueID,
		p.PhotoCount, p.HasPhoto, p.NotFound, p.RawJSON, p.FetchedAt, p.UpdatedAt)
	return err
}

func (d *DB) GetUserProfile(userID int64) (*UserProfile, error) {
	var p UserProfile
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, bio,
		       photo_file_id, photo_file_unique_id, photo_small_file_id, photo_small_file_unique_id,
		       photo_count, has_photo, not_found, raw_json, fetched_at, updated_at
		FROM user_profiles WHERE user_id = ?
	`, userID).Scan(
		&p.UserID, &p.Username, &p.FirstName, &p.LastName, &p.Bio,
		&p.PhotoFileID, &p.PhotoFileUniqueID, &p.PhotoSmallFileID, &p.PhotoSmallFileUniqueID,
		&p.PhotoCount, &p.HasPhoto, &p.NotFound, &p.RawJSON, &p.FetchedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *DB) GetUsersWithoutProfile(limit int) ([]User, error) {
	query := `
		SELECT u.user_id, u.username, u.first_name, u.last_name, u.reputation, u.warn_count, u.is_banned, u.is_admin, u.created_at, u.updated_at
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
		if err := rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.Reputation, &u.WarnCount, &u.IsBanned, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (d *DB) GetAllUserProfiles(limit int) ([]UserProfile, error) {
	query := `
		SELECT user_id, username, first_name, last_name, bio,
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
			&p.UserID, &p.Username, &p.FirstName, &p.LastName, &p.Bio,
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
	UserID        int64      `json:"user_id"`
	Username      string     `json:"username"`
	FirstName     string     `json:"first_name"`
	LastName      string     `json:"last_name"`
	Reputation    int        `json:"reputation"`
	WarnCount     int        `json:"warn_count"`
	IsBanned      bool       `json:"is_banned"`
	IsAdmin       bool       `json:"is_admin"`
	IsSuperAdmin  bool       `json:"is_super_admin"`
	Role          string     `json:"role"`
	MessageCount  int        `json:"message_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

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

