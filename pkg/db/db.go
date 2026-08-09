package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

type User struct {
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	FirstName  string    `json:"first_name"`
	LastName   string    `json:"last_name"`
	Reputation int       `json:"reputation"`
	WarnCount  int       `json:"warn_count"`
	IsBanned   bool      `json:"is_banned"`
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

func OpenDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	// SQLite connection string with WAL mode for performance
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	sqliteDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := sqliteDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	database := &DB{DB: sqliteDB}
	if err := database.InitSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return database, nil
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
	`
	_, err := d.Exec(schema)
	return err
}

// User Methods

func (d *DB) GetOrCreateUser(userID int64, username, firstName, lastName string, defaultRep int) (*User, error) {
	now := time.Now()
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, created_at, updated_at
		FROM users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		user = User{
			UserID:     userID,
			Username:   username,
			FirstName:  firstName,
			LastName:   lastName,
			Reputation: defaultRep,
			WarnCount:  0,
			IsBanned:   false,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		_, err := d.Exec(`
			INSERT INTO users (user_id, username, first_name, last_name, reputation, warn_count, is_banned, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, user.UserID, user.Username, user.FirstName, user.LastName, user.Reputation, user.WarnCount, user.IsBanned, user.CreatedAt, user.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return &user, nil
	} else if err != nil {
		return nil, err
	}

	// Update names/username if changed
	if user.Username != username || user.FirstName != firstName || user.LastName != lastName {
		user.Username = username
		user.FirstName = firstName
		user.LastName = lastName
		user.UpdatedAt = now
		d.Exec(`UPDATE users SET username = ?, first_name = ?, last_name = ?, updated_at = ? WHERE user_id = ?`,
			username, firstName, lastName, now, userID)
	}

	return &user, nil
}

func (d *DB) GetUserByID(userID int64) (*User, error) {
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, created_at, updated_at
		FROM users WHERE user_id = ?
	`, userID).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	if len(username) > 0 && username[0] == '@' {
		username = username[1:]
	}
	var user User
	err := d.QueryRow(`
		SELECT user_id, username, first_name, last_name, reputation, warn_count, is_banned, created_at, updated_at
		FROM users WHERE LOWER(username) = LOWER(?)
	`, username).Scan(&user.UserID, &user.Username, &user.FirstName, &user.LastName, &user.Reputation, &user.WarnCount, &user.IsBanned, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) AdjustReputation(userID int64, delta int, reason string, byUserID int64) (int, error) {
	now := time.Now()
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

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

func (d *DB) SetReputation(userID int64, targetRep int, reason string, byUserID int64) error {
	now := time.Now()
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

func (d *DB) SetUserBanned(userID int64, banned bool) error {
	now := time.Now()
	_, err := d.Exec(`UPDATE users SET is_banned = ?, updated_at = ? WHERE user_id = ?`, banned, now, userID)
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

// Message Methods

func (d *DB) SaveMessage(msg *Message) error {
	_, err := d.Exec(`
		INSERT INTO messages (chat_id, message_id, user_id, text, has_media, has_links, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id, message_id) DO NOTHING
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
	TotalUsers     int   `json:"total_users"`
	TotalGroups    int   `json:"total_groups"`
	TotalMessages  int   `json:"total_messages"`
	PendingFlags   int   `json:"pending_flags"`
	ResolvedFlags  int   `json:"resolved_flags"`
}

func (d *DB) GetStats() (*Stats, error) {
	var s Stats
	d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&s.TotalUsers)
	d.QueryRow(`SELECT COUNT(*) FROM groups WHERE is_monitored = 1`).Scan(&s.TotalGroups)
	d.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&s.TotalMessages)
	d.QueryRow(`SELECT COUNT(*) FROM flagged_posts WHERE status = 'pending'`).Scan(&s.PendingFlags)
	d.QueryRow(`SELECT COUNT(*) FROM flagged_posts WHERE status != 'pending'`).Scan(&s.ResolvedFlags)
	return &s, nil
}
