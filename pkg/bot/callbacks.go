package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleCallbackQuery(cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.Message == nil {
		return
	}

	data := cb.Data
	if !strings.HasPrefix(data, "mod:") {
		return
	}

	// Verify callback originated in moderation group or from super admin / mod
	if b.cfg.ModerationGroupID != 0 && cb.Message.Chat.ID != b.cfg.ModerationGroupID && cb.From.ID != b.cfg.SuperAdminID {
		b.answerCallback(cb.ID, "⚠️ You are not authorized to perform moderation actions.", true)
		return
	}

	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		b.answerCallback(cb.ID, "Invalid action data", true)
		return
	}

	action := parts[1]
	flagID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		b.answerCallback(cb.ID, "Invalid flag ID", true)
		return
	}

	flag, err := b.db.GetFlaggedPost(flagID)
	if err != nil {
		b.answerCallback(cb.ID, "Flagged post record not found", true)
		return
	}

	modUser := cb.From
	modIdentifier := fmt.Sprintf("@%s", modUser.UserName)
	if modUser.UserName == "" {
		modIdentifier = modUser.FirstName
	}

	user, err := b.db.GetUserByID(flag.UserID)
	if err != nil {
		b.answerCallback(cb.ID, "Poster user record not found", true)
		return
	}

	switch action {
	case "approve":
		if flag.Status != "pending" {
			b.answerCallback(cb.ID, fmt.Sprintf("Already resolved as: %s", flag.Status), true)
			return
		}

		newRep, _ := b.db.AdjustReputation(user.UserID, b.cfg.Reputation.ApproveBonus, "Approved by moderator", modUser.ID)
		_ = b.db.ResolveFlaggedPost(flagID, "approved", modUser.ID)

		b.answerCallback(cb.ID, "✅ Approved post & rewarded user +5 Rep!", false)
		b.updateModCardText(cb.Message, flag, user, "✅ Approved", modIdentifier, fmt.Sprintf("Approved by %s. Bonus +5 Rep awarded (New Rep: %d).", modIdentifier, newRep))

	case "delete":
		if err := b.DeleteGroupMessage(flag.GroupChatID, flag.GroupMessageID); err != nil {
			log.Printf("[Callback] Error deleting message %d in chat %d: %v", flag.GroupMessageID, flag.GroupChatID, err)
		}

		newRep, _ := b.db.AdjustReputation(user.UserID, -b.cfg.Reputation.DeletePenalty, "Post deleted by moderator", modUser.ID)
		_ = b.db.ResolveFlaggedPost(flagID, "deleted", modUser.ID)

		b.answerCallback(cb.ID, "🗑️ Post deleted & -10 Rep deducted.", false)
		b.updateModCardText(cb.Message, flag, user, "🗑️ Deleted", modIdentifier, fmt.Sprintf("Post deleted by %s. Deducted -10 Rep (New Rep: %d).", modIdentifier, newRep))

	case "warn":
		_ = b.DeleteGroupMessage(flag.GroupChatID, flag.GroupMessageID)
		warns, _ := b.db.IncrementWarning(user.UserID)
		newRep, _ := b.db.AdjustReputation(user.UserID, -b.cfg.Reputation.WarnPenalty, "Warned by moderator", modUser.ID)
		_ = b.db.ResolveFlaggedPost(flagID, "warned", modUser.ID)

		b.answerCallback(cb.ID, "⚠️ User warned, post deleted & -20 Rep deducted.", false)
		b.updateModCardText(cb.Message, flag, user, "⚠️ Warned", modIdentifier, fmt.Sprintf("Warned by %s. Total Warnings: %d. Deducted -20 Rep (New Rep: %d).", modIdentifier, warns, newRep))

	case "mute":
		_ = b.DeleteGroupMessage(flag.GroupChatID, flag.GroupMessageID)
		err := b.MuteUserInGroup(flag.GroupChatID, user.UserID, 24*time.Hour)
		muteResult := "Muted user for 24 hours in group."
		if err != nil {
			muteResult = fmt.Sprintf("Mute failed: %v", err)
		}
		newRep, _ := b.db.AdjustReputation(user.UserID, -b.cfg.Reputation.MutePenalty, "Muted 24h by moderator", modUser.ID)
		_ = b.db.ResolveFlaggedPost(flagID, "muted", modUser.ID)

		b.answerCallback(cb.ID, "🔇 User muted 24h, post deleted & -30 Rep.", false)
		b.updateModCardText(cb.Message, flag, user, "🔇 Muted 24h", modIdentifier, fmt.Sprintf("Muted 24h by %s (%s). Deducted -30 Rep (New Rep: %d).", modIdentifier, muteResult, newRep))

	case "ban":
		_ = b.DeleteGroupMessage(flag.GroupChatID, flag.GroupMessageID)
		err := b.BanUserAcrossAllGroups(user.UserID, flag.GroupChatID)
		banResult := "Banned across all monitored groups."
		if err != nil {
			banResult = fmt.Sprintf("Ban error: %v", err)
		}
		newRep, _ := b.db.AdjustReputation(user.UserID, -b.cfg.Reputation.BanPenalty, "Banned by moderator", modUser.ID)
		_ = b.db.ResolveFlaggedPost(flagID, "banned", modUser.ID)

		b.answerCallback(cb.ID, "🚫 User banned across all monitored groups!", false)
		b.updateModCardText(cb.Message, flag, user, "🚫 Banned User", modIdentifier, fmt.Sprintf("Banned by %s (%s). Deducted -50 Rep (New Rep: %d).", modIdentifier, banResult, newRep))

	case "rep_plus":
		newRep, _ := b.db.AdjustReputation(user.UserID, 10, "Manual mod adjustment (+10)", modUser.ID)
		b.answerCallback(cb.ID, fmt.Sprintf("➕ Increased Rep by 10 (New: %d)", newRep), false)

	case "rep_minus":
		newRep, _ := b.db.AdjustReputation(user.UserID, -10, "Manual mod adjustment (-10)", modUser.ID)
		b.answerCallback(cb.ID, fmt.Sprintf("➖ Decreased Rep by 10 (New: %d)", newRep), false)

	case "history":
		msgs, _ := b.db.GetRecentUserMessages(user.UserID, 5)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📜 **Recent Posts for User %s (ID: %d)**:\n", user.FirstName, user.UserID))
		if len(msgs) == 0 {
			sb.WriteString("No recorded messages in database.")
		} else {
			for i, m := range msgs {
				sb.WriteString(fmt.Sprintf("%d. [`%s`] %s\n", i+1, m.CreatedAt.Format("01-02 15:04"), truncateText(m.Text, 80)))
			}
		}
		b.answerCallback(cb.ID, "Showing history in mod group message...", false)

		histMsg := tgbotapi.NewMessage(cb.Message.Chat.ID, sb.String())
		histMsg.ReplyToMessageID = cb.Message.MessageID
		histMsg.ParseMode = tgbotapi.ModeMarkdown
		b.Send(histMsg)

	case "dismiss":
		_ = b.db.ResolveFlaggedPost(flagID, "dismissed", modUser.ID)
		b.answerCallback(cb.ID, "❌ Flag dismissed.", false)
		b.updateModCardText(cb.Message, flag, user, "❌ Dismissed", modIdentifier, fmt.Sprintf("Dismissed by %s without action.", modIdentifier))
	}
}

func (b *Bot) answerCallback(callbackID string, text string, showAlert bool) {
	cbConfig := tgbotapi.NewCallback(callbackID, text)
	cbConfig.ShowAlert = showAlert
	b.Request(cbConfig)
}

func (b *Bot) updateModCardText(modMsg *tgbotapi.Message, flag *db.FlaggedPost, user *db.User, statusLabel string, modIdentifier string, detail string) {
	updatedText := fmt.Sprintf(
		"%s\n\n"+
			"━━━━━━━━━━━━━━━━━━━━━━\n"+
			"⚙️ **RESOLUTION STATUS**: `%s`\n"+
			"👨‍⚖️ **Moderator**: %s\n"+
			"💬 **Details**: %s",
		modMsg.Text,
		statusLabel,
		modIdentifier,
		detail,
	)

	editMsg := tgbotapi.NewEditMessageText(modMsg.Chat.ID, modMsg.MessageID, updatedText)
	// Remove keyboard once resolved
	editMsg.ReplyMarkup = nil
	b.Send(editMsg)
}
