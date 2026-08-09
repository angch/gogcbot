package bot

import (
	"fmt"
	"log"
	"strings"

	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(update.CallbackQuery)
		return
	}

	if update.Message != nil {
		b.handleMessage(update.Message)
		return
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if msg.From == nil {
		return
	}

	// Ignore messages from bots themselves
	if msg.From.IsBot {
		return
	}

	chat := msg.Chat
	// If message is in a supergroup/group/channel, track the group
	if chat.IsGroup() || chat.IsSuperGroup() {
		_ = b.db.SaveGroup(chat.ID, chat.Title, chat.Type)
	}

	// Get or create user profile
	user, err := b.db.GetOrCreateUser(
		msg.From.ID,
		msg.From.UserName,
		msg.From.FirstName,
		msg.From.LastName,
		b.cfg.Reputation.DefaultInitial,
	)
	if err != nil {
		log.Printf("[Bot] Error getting/creating user %d: %v", msg.From.ID, err)
		return
	}

	// Ensure SuperAdmin and Group Admins have initial 100 reputation score
	isSuperAdmin := b.cfg.SuperAdminID != 0 && user.UserID == b.cfg.SuperAdminID
	isGroupAdmin := (chat.IsGroup() || chat.IsSuperGroup()) && b.IsUserAdminInChat(chat.ID, user.UserID)
	if (isSuperAdmin || isGroupAdmin) && user.Reputation < 100 {
		if err := b.db.SetReputation(user.UserID, 100, "Admin/SuperAdmin initial reputation setup", user.UserID); err == nil {
			user.Reputation = 100
		}
	}

	// Check if user is globally flagged as banned
	if user.IsBanned && (chat.IsGroup() || chat.IsSuperGroup()) {
		_ = b.DeleteGroupMessage(chat.ID, msg.MessageID)
		_ = b.BanUserInGroup(chat.ID, user.UserID)
		return
	}

	// Detect links & media
	hasMedia := msg.Photo != nil || msg.Video != nil || msg.Document != nil || msg.Audio != nil || msg.Animation != nil || msg.Sticker != nil
	hasLinks := containsLinks(msg)

	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	// Record message in DB for 7-day log & 50-posts history
	dbMsg := &db.Message{
		ChatID:    chat.ID,
		MessageID: msg.MessageID,
		UserID:    user.UserID,
		Text:      text,
		HasMedia:  hasMedia,
		HasLinks:  hasLinks,
		CreatedAt: msg.Time(),
	}
	if err := b.db.SaveMessage(dbMsg); err != nil {
		log.Printf("[Bot] Error saving message: %v", err)
	}

	// Handle command if message is a command
	if msg.IsCommand() || strings.HasPrefix(text, "!") {
		b.handleCommand(msg, user)
		return
	}

	// Moderation check only for monitored groups (not private chats or the moderation group itself)
	if (chat.IsGroup() || chat.IsSuperGroup()) && chat.ID != b.cfg.ModerationGroupID {
		b.checkAutoFlagRules(msg, dbMsg, user, chat.Title)
	}
}

func (b *Bot) checkAutoFlagRules(msg *tgbotapi.Message, dbMsg *db.Message, user *db.User, groupTitle string) {
	if !b.cfg.AutoFlag.Enabled {
		return
	}

	var reasons []string

	// Rule 1: Low Reputation + Link
	if b.cfg.AutoFlag.FlagOnLinks && dbMsg.HasLinks && user.Reputation < b.cfg.AutoFlag.LowRepThreshold {
		reasons = append(reasons, fmt.Sprintf("Low Reputation (%d < %d) with link", user.Reputation, b.cfg.AutoFlag.LowRepThreshold))
	}

	// Rule 2: New User + Link
	userMsgCount, _ := b.db.GetUserMessageCount(user.UserID)
	if b.cfg.AutoFlag.FlagOnLinks && dbMsg.HasLinks && userMsgCount <= b.cfg.AutoFlag.NewUserMinPosts {
		reasons = append(reasons, fmt.Sprintf("New user (%d <= %d posts) with link", userMsgCount, b.cfg.AutoFlag.NewUserMinPosts))
	}

	// Rule 3: Blocked Keywords
	lowerText := strings.ToLower(dbMsg.Text)
	for _, kw := range b.cfg.AutoFlag.BlockedKeywords {
		if kw != "" && strings.Contains(lowerText, strings.ToLower(kw)) {
			reasons = append(reasons, fmt.Sprintf("Contains keyword: '%s'", kw))
			break
		}
	}

	// Rule 4: Reputation below flag threshold
	if user.Reputation <= b.cfg.Reputation.FlagThreshold {
		reasons = append(reasons, fmt.Sprintf("Reputation at/below threshold (%d <= %d)", user.Reputation, b.cfg.Reputation.FlagThreshold))
	}

	if len(reasons) > 0 {
		reasonStr := strings.Join(reasons, " | ")
		log.Printf("[AutoFlag] Triggered for user %d in chat %d (%s): %s", user.UserID, msg.Chat.ID, groupTitle, reasonStr)

		flag, err := b.db.CreateFlaggedPost(msg.Chat.ID, msg.MessageID, user.UserID, reasonStr)
		if err != nil {
			log.Printf("[AutoFlag] Error saving flagged post: %v", err)
			return
		}

		if err := b.SendModAlert(flag, dbMsg, user, groupTitle); err != nil {
			log.Printf("[AutoFlag] Error sending mod alert: %v", err)
		}
	}
}

func containsLinks(msg *tgbotapi.Message) bool {
	if msg == nil {
		return false
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	if strings.Contains(text, "http://") || strings.Contains(text, "https://") || strings.Contains(text, "t.me/") || strings.Contains(text, "telegram.me/") {
		return true
	}

	entities := msg.Entities
	if entities == nil {
		entities = msg.CaptionEntities
	}

	for _, e := range entities {
		if e.Type == "url" || e.Type == "text_link" {
			return true
		}
	}

	return false
}
