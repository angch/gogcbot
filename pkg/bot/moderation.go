package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type BotPermissions struct {
	IsAdmin              bool
	CanDeleteMessages    bool
	CanRestrictMembers   bool
	CanPinMessages       bool
	CanInviteUsers       bool
	CanPromoteMembers    bool
}

func (b *Bot) CheckBotPermissions(chatID int64) (*BotPermissions, error) {
	cm, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
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

	sentMsg, err := b.api.Send(msgConfig)
	if err != nil {
		// Fallback without markdown formatting if markdown parsing fails
		msgConfig.ParseMode = ""
		sentMsg, err = b.api.Send(msgConfig)
		if err != nil {
			return fmt.Errorf("failed to send mod alert: %w", err)
		}
	}

	// Record mod group message ID in flagged_posts table
	return b.db.UpdateFlagModMessageID(flag.ID, sentMsg.MessageID)
}

func (b *Bot) DeleteGroupMessage(chatID int64, messageID int) error {
	deleteMsgConfig := tgbotapi.NewDeleteMessage(chatID, messageID)
	_, err := b.api.Request(deleteMsgConfig)
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

	_, err := b.api.Request(restrictConfig)
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

	_, err := b.api.Request(banConfig)
	return err
}

func (b *Bot) BanUserAcrossAllGroups(userID int64) error {
	groups, err := b.db.GetMonitoredGroups()
	if err != nil {
		return err
	}

	for _, g := range groups {
		_ = b.BanUserInGroup(g.ChatID, userID)
	}

	return b.db.SetUserBanned(userID, true)
}

func (b *Bot) UnbanUserInGroup(chatID int64, userID int64) error {
	unbanConfig := tgbotapi.UnbanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		OnlyIfBanned: true,
	}

	_, err := b.api.Request(unbanConfig)
	if err == nil {
		_ = b.db.SetUserBanned(userID, false)
	}
	return err
}

func escapeMarkdown(s string) string {
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
