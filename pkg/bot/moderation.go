package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/angch/gogcbot/pkg/db"
	"github.com/angch/gogcbot/pkg/detector"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type BotPermissions struct {
	IsAdmin            bool
	CanDeleteMessages  bool
	CanRestrictMembers bool
	CanPinMessages     bool
	CanInviteUsers     bool
	CanPromoteMembers  bool
}

func (b *Bot) CheckBotPermissions(chatID int64) (*BotPermissions, error) {
	cm, err := b.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: b.botUser.ID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get bot chat member status: %w", err)
	}

	p := &BotPermissions{
		IsAdmin:            cm.Status == "administrator" || cm.Status == "creator",
		CanDeleteMessages:  cm.CanDeleteMessages,
		CanRestrictMembers: cm.CanRestrictMembers,
		CanPinMessages:     cm.CanPinMessages,
		CanInviteUsers:     cm.CanInviteUsers,
		CanPromoteMembers:  cm.CanPromoteMembers,
	}

	return p, nil
}

func (b *Bot) SendModAlert(flag *db.FlaggedPost, msg *db.Message, user *db.User, groupTitle string) error {
	modGroupID := b.cfg.ModerationGroupID
	if modGroupID == 0 {
		log.Printf("[Bot] Warning: ModerationGroupID not set. Cannot send flag alert for post ID %d in chat %d", msg.MessageID, msg.ChatID)
		return nil
	}

	totalUserMsgs, _ := b.db.GetUserMessageCount(user.UserID)

	text := fmt.Sprintf(
		"🚨 **FLAGGED POST REVIEW**\n\n"+
			"📌 **Reason**: `%s`\n\n"+
			"👤 **Poster**: %s (@%s)\n"+
			"🆔 **User ID**: `%d`\n"+
			"⭐ **Reputation**: `%d` | ⚠️ **Warns**: `%d` | 💬 **Logged Posts**: `%d`\n\n"+
			"👥 **Group**: %s (`%d`)\n"+
			"🆔 **Message ID**: `%d`\n"+
			"🕒 **Time**: `%s`\n\n"+
			"📝 **Message Snippet**:\n```\n%s\n```",
		escapeMarkdown(flag.Reason),
		escapeMarkdown(user.FirstName+" "+user.LastName),
		escapeMarkdown(user.Username),
		user.UserID,
		user.Reputation,
		user.WarnCount,
		totalUserMsgs,
		escapeMarkdown(groupTitle),
		msg.ChatID,
		msg.MessageID,
		flag.FlaggedAt.Format("2006-01-02 15:04:05 MST"),
		truncateText(msg.Text, 500),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Approve (+5)", fmt.Sprintf("mod:approve:%d", flag.ID)),
			tgbotapi.NewInlineKeyboardButtonData("🗑️ Delete (-10)", fmt.Sprintf("mod:delete:%d", flag.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚠️ Delete & Warn (-20)", fmt.Sprintf("mod:warn:%d", flag.ID)),
			tgbotapi.NewInlineKeyboardButtonData("🔇 Mute 24h (-30)", fmt.Sprintf("mod:mute:%d", flag.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚫 Ban User (-50)", fmt.Sprintf("mod:ban:%d", flag.ID)),
			tgbotapi.NewInlineKeyboardButtonData("📜 History", fmt.Sprintf("mod:history:%d", flag.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Rep +10", fmt.Sprintf("mod:rep_plus:%d", flag.ID)),
			tgbotapi.NewInlineKeyboardButtonData("➖ Rep -10", fmt.Sprintf("mod:rep_minus:%d", flag.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Dismiss", fmt.Sprintf("mod:dismiss:%d", flag.ID)),
		),
	)

	msgConfig := tgbotapi.NewMessage(modGroupID, text)
	msgConfig.ParseMode = tgbotapi.ModeMarkdown
	msgConfig.ReplyMarkup = keyboard

	sentMsg, err := b.Send(msgConfig)
	if err != nil {
		// Fallback without markdown formatting if markdown parsing fails
		msgConfig.ParseMode = ""
		sentMsg, err = b.Send(msgConfig)
		if err != nil {
			return fmt.Errorf("failed to send mod alert: %w", err)
		}
	}

	// Record mod group message ID in flagged_posts table
	return b.db.UpdateFlagModMessageID(flag.ID, sentMsg.MessageID)
}

func (b *Bot) DeleteGroupMessage(chatID int64, messageID int) error {
	deleteMsgConfig := tgbotapi.NewDeleteMessage(chatID, messageID)
	_, err := b.Request(deleteMsgConfig)
	return err
}

func (b *Bot) MuteUserInGroup(chatID int64, userID int64, duration time.Duration) error {
	untilDate := time.Now().Add(duration).Unix()
	// Restrict send messages
	restrictConfig := tgbotapi.RestrictChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		UntilDate: untilDate,
		Permissions: &tgbotapi.ChatPermissions{
			CanSendMessages:       false,
			CanSendMediaMessages:  false,
			CanSendPolls:          false,
			CanSendOtherMessages:  false,
			CanAddWebPagePreviews: false,
		},
	}

	_, err := b.Request(restrictConfig)
	return err
}

func (b *Bot) BanUserInGroup(chatID int64, userID int64) error {
	banConfig := tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		RevokeMessages: true,
	}

	_, err := b.Request(banConfig)
	if err != nil {
		return fmt.Errorf("telegram ban error: %w", err)
	}
	return nil
}

func (b *Bot) BanUserAcrossAllGroups(userID int64, currentChatID ...int64) error {
	var errs []string

	chatIDs := make(map[int64]bool)
	for _, cid := range currentChatID {
		if cid != 0 {
			chatIDs[cid] = true
		}
	}

	groups, err := b.db.GetMonitoredGroups()
	if err == nil {
		for _, g := range groups {
			chatIDs[g.ChatID] = true
		}
	}

	for cid := range chatIDs {
		if err := b.BanUserInGroup(cid, userID); err != nil {
			log.Printf("[Bot] Failed to ban user %d in chat %d: %v", userID, cid, err)
			errs = append(errs, fmt.Sprintf("chat %d (%v)", cid, err))
		} else {
			delay := time.Duration(b.cfg.Shieldy.RecheckDelayMinutes) * time.Minute
			if delay <= 0 {
				delay = 6 * time.Minute
			}
			b.ScheduleBanRecheck(cid, userID, delay)
		}
	}

	_ = b.db.SetUserBanned(userID, true)

	if len(errs) > 0 {
		return fmt.Errorf("failed to kick in some chats: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ScheduleBanRecheck schedules a delayed check (e.g. 6 minutes) to verify if Shieldy or Telegram
// modified a permanent ban into a timed ban. If so, it re-issues the permanent ban.
func (b *Bot) ScheduleBanRecheck(chatID int64, userID int64, delay time.Duration) {
	go func() {
		time.Sleep(delay)

		user, err := b.db.GetUserByID(userID)
		if err != nil || user == nil || !user.IsBanned {
			log.Printf("[Ban Recheck] User %d is no longer marked as banned in DB, skipping recheck", userID)
			return
		}

		cm, err := b.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: chatID,
				UserID: userID,
			},
		})
		if err != nil {
			log.Printf("[Ban Recheck Error] Failed to get chat member status for user %d in chat %d: %v", userID, chatID, err)
			return
		}

		// Detect if ban was changed:
		// Permanent ban in Telegram has Status == "kicked" AND UntilDate == 0.
		// If Status != "kicked" OR UntilDate != 0, Shieldy or Telegram changed the ban to a timed ban or restriction.
		banChanged := cm.Status != "kicked" || cm.UntilDate != 0
		if banChanged {
			log.Printf("[Ban Recheck Alert] Ban for user %d in chat %d was changed by Shieldy/Telegram (Status: %s, UntilDate: %d). Reissuing permanent ban...",
				userID, chatID, cm.Status, cm.UntilDate)

			if err := b.BanUserInGroup(chatID, userID); err != nil {
				log.Printf("[Ban Recheck Error] Failed to reissue ban for user %d in chat %d: %v", userID, chatID, err)
			} else {
				log.Printf("[Ban Recheck Success] Reissued permanent ban for user %d in chat %d", userID, chatID)
			}
		} else {
			log.Printf("[Ban Recheck] User %d in chat %d remains permanently banned (Status: %s)", userID, chatID, cm.Status)
		}
	}()
}

func (b *Bot) UnbanUserInGroup(chatID int64, userID int64) error {
	unbanConfig := tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		OnlyIfBanned: true,
	}

	_, err := b.Request(unbanConfig)
	// Unconditionally clear banned flag in DB even if Telegram reports user is already unbanned
	_ = b.db.SetUserBanned(userID, false)
	return err
}

func (b *Bot) UnbanUserAcrossAllGroups(userID int64, currentChatID ...int64) error {
	chatIDs := make(map[int64]bool)
	for _, cid := range currentChatID {
		if cid != 0 {
			chatIDs[cid] = true
		}
	}

	groups, err := b.db.GetMonitoredGroups()
	if err == nil {
		for _, g := range groups {
			chatIDs[g.ChatID] = true
		}
	}

	for cid := range chatIDs {
		_ = b.UnbanUserInGroup(cid, userID)
	}

	_ = b.db.SetUserBanned(userID, false)

	// Restore user reputation to at least FlagThreshold + 10 (or 50) if currently low/negative
	user, err := b.db.GetUserByID(userID)
	if err == nil && user.Reputation <= b.cfg.Reputation.FlagThreshold {
		targetRep := b.cfg.Reputation.FlagThreshold + 10
		if targetRep < 50 {
			targetRep = 50
		}
		_ = b.db.SetReputation(userID, targetRep, "Unbanned & reputation restored", userID)
	}

	return nil
}

func (b *Bot) PromoteUserInBot(userID int64) error {
	return b.db.SetUserAdmin(userID, true)
}

func (b *Bot) DemoteUserInBot(userID int64) error {
	return b.db.SetUserAdmin(userID, false)
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "[", "\\[")
	return s
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// SendTriggerBanAlert sends a notification message to the management monitoring channel (b.cfg.ModerationGroupID)
// whenever a user ban is triggered by an automated detection trigger.
func (b *Bot) SendTriggerBanAlert(chatID int64, user *db.User, messageID int, reason string) error {
	modGroupID := b.cfg.ModerationGroupID
	if modGroupID == 0 {
		log.Printf("[Bot] Warning: ModerationGroupID not set. Cannot send trigger ban alert for user %d in chat %d", user.UserID, chatID)
		return nil
	}

	groupTitle := fmt.Sprintf("Chat %d", chatID)
	if group, err := b.db.GetGroup(chatID); err == nil && group != nil && group.Title != "" {
		groupTitle = group.Title
	}

	totalUserMsgs, _ := b.db.GetUserMessageCount(user.UserID)

	userDisplayName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if userDisplayName == "" {
		userDisplayName = fmt.Sprintf("User %d", user.UserID)
	}

	usernameStr := user.Username
	if usernameStr == "" {
		usernameStr = "none"
	}

	text := fmt.Sprintf(
		"🚫 **TRIGGER BAN EXECUTED**\n\n"+
			"📌 **Reason**: `%s`\n\n"+
			"👤 **User**: %s (@%s)\n"+
			"🆔 **User ID**: `%d`\n"+
			"⭐ **Reputation**: `%d` | ⚠️ **Warns**: `%d` | 💬 **Logged Posts**: `%d`\n\n"+
			"👥 **Group**: %s (`%d`)\n"+
			"🆔 **Message ID**: `%d`\n"+
			"🕒 **Time**: `%s`",
		escapeMarkdown(reason),
		escapeMarkdown(userDisplayName),
		escapeMarkdown(usernameStr),
		user.UserID,
		user.Reputation,
		user.WarnCount,
		totalUserMsgs,
		escapeMarkdown(groupTitle),
		chatID,
		messageID,
		time.Now().Format("2006-01-02 15:04:05 MST"),
	)

	msgConfig := tgbotapi.NewMessage(modGroupID, text)
	msgConfig.ParseMode = tgbotapi.ModeMarkdown

	_, err := b.Send(msgConfig)
	if err != nil {
		// Fallback without markdown formatting if markdown fails
		msgConfig.ParseMode = ""
		_, err = b.Send(msgConfig)
		if err != nil {
			log.Printf("[Bot Error] Failed to send trigger ban alert to mod group: %v", err)
			return fmt.Errorf("failed to send trigger ban alert: %w", err)
		}
	}
	return nil
}

// SendFirstEmptyMessageInfo sends a silent notification to the moderation channel (b.cfg.ModerationGroupID)
// when a user's first message seen in a monitored group/channel is empty.
func (b *Bot) SendFirstEmptyMessageInfo(chatID int64, msg *db.Message, user *db.User, groupTitle string) error {
	modGroupID := b.cfg.ModerationGroupID
	if modGroupID == 0 {
		log.Printf("[Bot] Warning: ModerationGroupID not set. Cannot send first empty message info for user %d in chat %d", user.UserID, chatID)
		return nil
	}

	if groupTitle == "" {
		groupTitle = fmt.Sprintf("Chat %d", chatID)
		if group, err := b.db.GetGroup(chatID); err == nil && group != nil && group.Title != "" {
			groupTitle = group.Title
		}
	}

	totalUserMsgs, _ := b.db.GetUserMessageCount(user.UserID)

	userDisplayName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if userDisplayName == "" {
		userDisplayName = fmt.Sprintf("User %d", user.UserID)
	}

	usernameStr := user.Username
	if usernameStr == "" {
		usernameStr = "none"
	}

	text := fmt.Sprintf(
		"ℹ️ **FIRST MESSAGE SEEN (EMPTY)**\n\n"+
			"📌 **Info**: Poster's first message is empty (unflagged)\n\n"+
			"👤 **User**: %s (@%s)\n"+
			"🆔 **User ID**: `%d`\n"+
			"⭐ **Reputation**: `%d` | ⚠️ **Warns**: `%d` | 💬 **Logged Posts**: `%d`\n\n"+
			"👥 **Group**: %s (`%d`)\n"+
			"🆔 **Message ID**: `%d`\n"+
			"🕒 **Time**: `%s`",
		escapeMarkdown(userDisplayName),
		escapeMarkdown(usernameStr),
		user.UserID,
		user.Reputation,
		user.WarnCount,
		totalUserMsgs,
		escapeMarkdown(groupTitle),
		chatID,
		msg.MessageID,
		msg.CreatedAt.Format("2006-01-02 15:04:05 MST"),
	)

	msgConfig := tgbotapi.NewMessage(modGroupID, text)
	msgConfig.ParseMode = tgbotapi.ModeMarkdown
	msgConfig.DisableNotification = true

	_, err := b.Send(msgConfig)
	if err != nil {
		// Fallback without markdown formatting if markdown fails
		msgConfig.ParseMode = ""
		_, err = b.Send(msgConfig)
		if err != nil {
			log.Printf("[Bot Error] Failed to send first empty message info to mod group: %v", err)
			return fmt.Errorf("failed to send first empty message info: %w", err)
		}
	}
	return nil
}

// ExecuteActions executes a set of actions returned by detection triggers against a chat message and user.
func (b *Bot) ExecuteActions(chatID int64, user *db.User, messageID int, actions []detector.Action) {
	for _, act := range actions {
		switch act.Type {
		case detector.ActionDeleteMessage:
			log.Printf("[Bot Action] Deleting message %d in chat %d (reason: %s)", messageID, chatID, act.Reason)
			if err := b.DeleteGroupMessage(chatID, messageID); err != nil {
				log.Printf("[Bot Action Error] Failed to delete message %d in chat %d: %v", messageID, chatID, err)
			}

		case detector.ActionBanUser:
			log.Printf("[Bot Action] Banning user %d in chat %d (reason: %s)", user.UserID, chatID, act.Reason)
			if err := b.BanUserInGroup(chatID, user.UserID); err != nil {
				log.Printf("[Bot Action Error] Failed to ban user %d in chat %d: %v", user.UserID, chatID, err)
			} else {
				delay := time.Duration(b.cfg.Shieldy.RecheckDelayMinutes) * time.Minute
				if delay <= 0 {
					delay = 6 * time.Minute
				}
				b.ScheduleBanRecheck(chatID, user.UserID, delay)
			}
			_ = b.db.SetUserBanned(user.UserID, true)

			if err := b.SendTriggerBanAlert(chatID, user, messageID, act.Reason); err != nil {
				log.Printf("[Bot Action Error] Failed to send trigger ban alert: %v", err)
			}

		case detector.ActionAdjustReputation:
			log.Printf("[Bot Action] Adjusting reputation for user %d by %d (reason: %s)", user.UserID, act.RepDelta, act.Reason)
			newRep, err := b.db.AdjustReputation(user.UserID, act.RepDelta, act.Reason, 0)
			if err != nil {
				log.Printf("[Bot Action Error] Failed to adjust reputation for user %d: %v", user.UserID, err)
			} else {
				user.Reputation = newRep
			}

		case detector.ActionFlagMessage:
			log.Printf("[Bot Action] Flagging message %d in chat %d for moderation review (reason: %s)", messageID, chatID, act.Reason)
			flag, err := b.db.CreateFlaggedPost(chatID, messageID, user.UserID, act.Reason)
			if err == nil && b.cfg.ModerationGroupID != 0 {
				dbMsg := &db.Message{
					ChatID:    chatID,
					MessageID: messageID,
					UserID:    user.UserID,
					Text:      act.Reason,
				}
				_ = b.SendModAlert(flag, dbMsg, user, "")
			}
		}
	}
}
