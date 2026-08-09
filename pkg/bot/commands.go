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

func (b *Bot) handleCommand(msg *tgbotapi.Message, user *db.User) {
	cmd := msg.Command()
	if cmd == "" {
		// If command starts with !, extract command name
		text := msg.Text
		if strings.HasPrefix(text, "!") {
			parts := strings.Fields(text[1:])
			if len(parts) > 0 {
				cmd = parts[0]
			}
		}
	}
	cmd = strings.ToLower(cmd)

	args := msg.CommandArguments()
	if args == "" && strings.HasPrefix(msg.Text, "!") {
		parts := strings.Fields(msg.Text)
		if len(parts) > 1 {
			args = strings.Join(parts[1:], " ")
		}
	}

	isSuperAdmin := b.cfg.SuperAdminID != 0 && user.UserID == b.cfg.SuperAdminID
	isModGroup := b.cfg.ModerationGroupID != 0 && msg.Chat.ID == b.cfg.ModerationGroupID
	isGroupAdmin := (msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()) && b.IsUserAdminInChat(msg.Chat.ID, user.UserID)
	isAuthorized := isSuperAdmin || isModGroup || isGroupAdmin

	// Ignore all commands from non-admin users (log silently and do not reply)
	// Exception: /setsuperadmin can be executed by the first user if super_admin_id is not yet set (0)
	if !isAuthorized && !(cmd == "setsuperadmin" && b.cfg.SuperAdminID == 0) {
		log.Printf("[Bot] Ignored command '/%s' from non-admin user %d (@%s) in chat %d (%s)", cmd, user.UserID, user.Username, msg.Chat.ID, msg.Chat.Title)
		return
	}

	switch cmd {
	case "start":
		b.replyText(msg, "👋 **Welcome to GoGCBot!**\n\nI am a Telegram group management and reputation bot.\nUse /help to view available commands.")

	case "help":
		b.replyText(msg, getHelpText(isSuperAdmin, isModGroup))

	case "status":
		stats, err := b.db.GetStats()
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Error getting stats: %v", err))
			return
		}

		modGroupSet := "Not Set ❌"
		if b.cfg.ModerationGroupID != 0 {
			modGroupSet = fmt.Sprintf("%d ✅", b.cfg.ModerationGroupID)
		}

		adminSet := "Not Set ❌"
		if b.cfg.SuperAdminID != 0 {
			adminSet = fmt.Sprintf("%d ✅", b.cfg.SuperAdminID)
		}

		statusText := fmt.Sprintf(
			"📊 **GoGCBot Status & Metrics**\n\n"+
				"🤖 **Bot Name**: @%s\n"+
				"👑 **Super Admin ID**: `%s`\n"+
				"🛡️ **Mod Group ID**: `%s`\n\n"+
				"👥 **Monitored Groups**: `%d`\n"+
				"👤 **Tracked Users**: `%d`\n"+
				"💬 **Logged Messages (7d max)**: `%d`\n"+
				"🚨 **Pending Flags**: `%d`\n"+
				"✅ **Resolved Flags**: `%d`\n\n"+
				"⚙️ **Auto-Flag**: %t (Low Rep < %d, Flag Links: %t)",
			b.botUser.UserName,
			adminSet,
			modGroupSet,
			stats.TotalGroups,
			stats.TotalUsers,
			stats.TotalMessages,
			stats.PendingFlags,
			stats.ResolvedFlags,
			b.cfg.AutoFlag.Enabled,
			b.cfg.AutoFlag.LowRepThreshold,
			b.cfg.AutoFlag.FlagOnLinks,
		)
		b.replyText(msg, statusText)

	case "setsuperadmin":
		if b.cfg.SuperAdminID != 0 && !isSuperAdmin {
			b.replyText(msg, fmt.Sprintf("❌ Super Admin is already set to user ID `%d`.", b.cfg.SuperAdminID))
			return
		}
		b.cfg.SuperAdminID = user.UserID
		isSuperAdmin = true
		isAuthorized = true
		_ = b.db.SetReputation(user.UserID, 100, "Promoted to Super Admin", user.UserID)
		user.Reputation = 100
		if err := b.SaveConfig(); err != nil {
			b.replyText(msg, fmt.Sprintf("⚠️ You (@%s, ID: `%d`) are set as Super Admin in memory, but saving config file failed: %v", user.Username, user.UserID, err))
			return
		}
		b.replyText(msg, fmt.Sprintf("✅ You (@%s, ID: `%d`) are now configured and saved as the Super Admin (Reputation: 100)!", user.Username, user.UserID))

	case "setmodgroup":
		if !isSuperAdmin && user.UserID != b.cfg.SuperAdminID {
			b.replyText(msg, "❌ Only the Super Admin can set the moderation group.")
			return
		}
		if !msg.Chat.IsGroup() && !msg.Chat.IsSuperGroup() {
			b.replyText(msg, "❌ This command must be executed inside the target Private Moderation Group.")
			return
		}
		b.cfg.ModerationGroupID = msg.Chat.ID
		isModGroup = true
		isAuthorized = true
		if err := b.SaveConfig(); err != nil {
			b.replyText(msg, fmt.Sprintf("⚠️ Moderation Group set, but saving config file failed: %v", err))
		}
		_ = b.db.SaveGroup(msg.Chat.ID, msg.Chat.Title, msg.Chat.Type)
		b.replyText(msg, fmt.Sprintf("✅ This group (`%s`, ID: `%d`) is now configured as the Private Moderation Group!", msg.Chat.Title, msg.Chat.ID))

	case "addgroup":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		if !msg.Chat.IsGroup() && !msg.Chat.IsSuperGroup() {
			b.replyText(msg, "❌ Must be called inside a group.")
			return
		}
		_ = b.db.SaveGroup(msg.Chat.ID, msg.Chat.Title, msg.Chat.Type)
		_ = b.db.SetGroupMonitored(msg.Chat.ID, true)
		b.replyText(msg, fmt.Sprintf("✅ Group `%s` (ID: `%d`) is now being monitored!", msg.Chat.Title, msg.Chat.ID))

	case "removegroup":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		_ = b.db.SetGroupMonitored(msg.Chat.ID, false)
		b.replyText(msg, fmt.Sprintf("🚫 Monitoring disabled for group `%s` (ID: `%d`).", msg.Chat.Title, msg.Chat.ID))

	case "listgroups":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		groups, err := b.db.GetMonitoredGroups()
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Error listing groups: %v", err))
			return
		}
		if len(groups) == 0 {
			b.replyText(msg, "No monitored groups currently configured.")
			return
		}
		var sb strings.Builder
		sb.WriteString("📋 **Monitored Groups List**:\n\n")
		for i, g := range groups {
			sb.WriteString(fmt.Sprintf("%d. %s (`%d`) [%s]\n", i+1, g.Title, g.ChatID, g.Type))
		}
		b.replyText(msg, sb.String())

	case "checkperms":
		perms, err := b.CheckBotPermissions(msg.Chat.ID)
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Failed to check permissions: %v", err))
			return
		}

		permText := fmt.Sprintf(
			"🛡️ **Bot Admin Permissions in Chat `%s`**:\n\n"+
				"• Admin Status: %s\n"+
				"• Delete Messages: %s\n"+
				"• Restrict Members: %s\n"+
				"• Pin Messages: %s\n"+
				"• Invite Users: %s\n"+
				"• Promote Members: %s",
			msg.Chat.Title,
			boolStatus(perms.IsAdmin),
			boolStatus(perms.CanDeleteMessages),
			boolStatus(perms.CanRestrictMembers),
			boolStatus(perms.CanPinMessages),
			boolStatus(perms.CanInviteUsers),
			boolStatus(perms.CanPromoteMembers),
		)
		b.replyText(msg, permText)

	case "flag":
		if msg.ReplyToMessage == nil {
			b.replyText(msg, "💡 Reply to a message with `/flag [reason]` to flag it for moderation review.")
			return
		}
		targetMsg := msg.ReplyToMessage
		if targetMsg.From == nil {
			return
		}
		reason := args
		if reason == "" {
			reason = "Flagged by user/moderator request"
		}
		targetUser, _ := b.db.GetOrCreateUser(targetMsg.From.ID, targetMsg.From.UserName, targetMsg.From.FirstName, targetMsg.From.LastName, b.cfg.Reputation.DefaultInitial)
		dbMsg := &db.Message{
			ChatID:    targetMsg.Chat.ID,
			MessageID: targetMsg.MessageID,
			UserID:    targetUser.UserID,
			Text:      targetMsg.Text,
			CreatedAt: targetMsg.Time(),
		}
		flag, err := b.db.CreateFlaggedPost(targetMsg.Chat.ID, targetMsg.MessageID, targetUser.UserID, reason)
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Error flagging message: %v", err))
			return
		}
		_ = b.SendModAlert(flag, dbMsg, targetUser, targetMsg.Chat.Title)
		b.replyText(msg, "🚨 Message flagged and sent to moderation team for review.")

	case "userinfo":
		targetUser := b.resolveUserFromArgsOrReply(msg, args)
		if targetUser == nil {
			b.replyText(msg, "❌ User not found. Reply to a user or specify user ID / @username.")
			return
		}
		msgCount, _ := b.db.GetUserMessageCount(targetUser.UserID)
		recentMsgs, _ := b.db.GetRecentUserMessages(targetUser.UserID, 3)

		var msgsSnippet strings.Builder
		for _, rm := range recentMsgs {
			msgsSnippet.WriteString(fmt.Sprintf("• [`%s`] %s\n", rm.CreatedAt.Format("01-02 15:04"), truncateText(rm.Text, 60)))
		}
		if len(recentMsgs) == 0 {
			msgsSnippet.WriteString("• No recent logged messages\n")
		}

		infoText := fmt.Sprintf(
			"👤 **User Info Card**:\n\n"+
				"• Name: %s %s (@%s)\n"+
				"• ID: `%d`\n"+
				"• Reputation: `%d`\n"+
				"• Warnings: `%d`\n"+
				"• Banned: %t\n"+
				"• Total Logged Posts: `%d`\n\n"+
				"📝 **Recent Activity**:\n%s",
			targetUser.FirstName, targetUser.LastName, targetUser.Username,
			targetUser.UserID,
			targetUser.Reputation,
			targetUser.WarnCount,
			targetUser.IsBanned,
			msgCount,
			msgsSnippet.String(),
		)
		b.replyText(msg, infoText)

	case "rep":
		parts := strings.Fields(args)
		if len(parts) == 0 && msg.ReplyToMessage != nil {
			targetUser, _ := b.db.GetUserByID(msg.ReplyToMessage.From.ID)
			if targetUser != nil {
				b.replyText(msg, fmt.Sprintf("⭐ User @%s (ID: `%d`) has Reputation: `%d`", targetUser.Username, targetUser.UserID, targetUser.Reputation))
				return
			}
		}

		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}

		targetUser := b.resolveUserFromArgsOrReply(msg, args)
		if targetUser == nil {
			b.replyText(msg, "Usage: `/rep <user_id|@username> [delta]` or reply to user `/rep +10`")
			return
		}

		// Look for delta in arguments
		var delta int
		for _, p := range parts {
			if d, err := strconv.Atoi(p); err == nil {
				delta = d
				break
			}
		}

		if delta != 0 {
			newRep, err := b.db.AdjustReputation(targetUser.UserID, delta, "Manual CLI command adjustment", user.UserID)
			if err != nil {
				b.replyText(msg, fmt.Sprintf("❌ Error adjusting rep: %v", err))
				return
			}
			b.replyText(msg, fmt.Sprintf("⭐ Updated reputation for @%s (ID: `%d`). Delta: %+d -> New Rep: `%d`", targetUser.Username, targetUser.UserID, delta, newRep))
		} else {
			b.replyText(msg, fmt.Sprintf("⭐ User @%s (ID: `%d`) Reputation: `%d`", targetUser.Username, targetUser.UserID, targetUser.Reputation))
		}

	case "warn":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		targetUser := b.resolveUserFromArgsOrReply(msg, args)
		if targetUser == nil {
			b.replyText(msg, "Usage: `/warn <user_id|@username>` or reply to user message.")
			return
		}
		warns, _ := b.db.IncrementWarning(targetUser.UserID)
		newRep, _ := b.db.AdjustReputation(targetUser.UserID, -b.cfg.Reputation.WarnPenalty, "Warning issued", user.UserID)
		b.replyText(msg, fmt.Sprintf("⚠️ Warning issued to @%s! Warns: `%d`, Rep: `%d` (-%d)", targetUser.Username, warns, newRep, b.cfg.Reputation.WarnPenalty))

	case "mute":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		targetUser := b.resolveUserFromArgsOrReply(msg, args)
		if targetUser == nil {
			b.replyText(msg, "Usage: `/mute <user_id|@username> [hours]`")
			return
		}
		hours := 24
		parts := strings.Fields(args)
		for _, p := range parts {
			if h, err := strconv.Atoi(p); err == nil && h > 0 {
				hours = h
				break
			}
		}
		err := b.MuteUserInGroup(msg.Chat.ID, targetUser.UserID, time.Duration(hours)*time.Hour)
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Failed to mute user: %v", err))
			return
		}
		newRep, _ := b.db.AdjustReputation(targetUser.UserID, -b.cfg.Reputation.MutePenalty, "Muted by command", user.UserID)
		b.replyText(msg, fmt.Sprintf("🔇 Muted @%s for %d hours in this chat. Rep: `%d` (-%d)", targetUser.Username, hours, newRep, b.cfg.Reputation.MutePenalty))

	case "ban":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		targetUser := b.resolveUserFromArgsOrReply(msg, args)
		if targetUser == nil {
			b.replyText(msg, "Usage: `/ban <user_id|@username>`")
			return
		}
		err := b.BanUserAcrossAllGroups(targetUser.UserID)
		if err != nil {
			b.replyText(msg, fmt.Sprintf("⚠️ Ban error: %v", err))
		}
		newRep, _ := b.db.AdjustReputation(targetUser.UserID, -b.cfg.Reputation.BanPenalty, "Banned by command", user.UserID)
		b.replyText(msg, fmt.Sprintf("🚫 Banned @%s across monitored groups. Rep: `%d` (-%d)", targetUser.Username, newRep, b.cfg.Reputation.BanPenalty))

	case "unban":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		targetUser := b.resolveUserFromArgsOrReply(msg, args)
		if targetUser == nil {
			b.replyText(msg, "Usage: `/unban <user_id|@username>`")
			return
		}
		_ = b.UnbanUserInGroup(msg.Chat.ID, targetUser.UserID)
		b.replyText(msg, fmt.Sprintf("✅ Unbanned @%s.", targetUser.Username))

	case "cleanup":
		if !isAuthorized {
			b.replyText(msg, "❌ Permission denied.")
			return
		}
		oldP, userP, err := b.cleaner.RunOnce()
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Cleanup failed: %v", err))
			return
		}
		b.replyText(msg, fmt.Sprintf("🧹 **Manual Retention Cleanup Done**!\n• Old Messages (>7d) Purged: `%d`\n• Excess User Messages (>50/user) Purged: `%d`", oldP, userP))
	}
}

func (b *Bot) resolveUserFromArgsOrReply(msg *tgbotapi.Message, args string) *db.User {
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
		u, err := b.db.GetOrCreateUser(msg.ReplyToMessage.From.ID, msg.ReplyToMessage.From.UserName, msg.ReplyToMessage.From.FirstName, msg.ReplyToMessage.From.LastName, b.cfg.Reputation.DefaultInitial)
		if err == nil {
			return u
		}
	}

	parts := strings.Fields(args)
	for _, p := range parts {
		if strings.HasPrefix(p, "@") {
			u, err := b.db.GetUserByUsername(p)
			if err == nil {
				return u
			}
		}
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			u, err := b.db.GetUserByID(id)
			if err == nil {
				return u
			}
		}
	}

	return nil
}

func (b *Bot) IsUserAdminInChat(chatID int64, userID int64) bool {
	if chatID == 0 || userID == 0 {
		return false
	}
	cm, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
		},
	})
	if err != nil {
		return false
	}
	return cm.Status == "administrator" || cm.Status == "creator"
}

func (b *Bot) replyText(msg *tgbotapi.Message, text string) {
	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	reply.ReplyToMessageID = msg.MessageID
	reply.ParseMode = tgbotapi.ModeMarkdown
	if _, err := b.api.Send(reply); err != nil {
		reply.ParseMode = ""
		b.api.Send(reply)
	}
}

func boolStatus(val bool) string {
	if val {
		return "✅ Allowed"
	}
	return "❌ Missing"
}

func floatToHours(h int) int {
	if h <= 0 {
		return 24
	}
	return h
}

func getHelpText(isSuperAdmin, isModGroup bool) string {
	return "🤖 **GoGCBot Commands & Usage**\n\n" +
		"**General Commands**:\n" +
		"• `/start` - Start bot interaction\n" +
		"• `/help` - Show help instructions\n" +
		"• `/status` - Show bot & system metrics\n" +
		"• `/checkperms` - Verify bot admin rights in current chat\n" +
		"• `/flag [reason]` - Reply to a message to flag it for moderation review\n" +
		"• `/userinfo <user_id|@username>` - Show user history & reputation\n\n" +
		"**Moderator & Super Admin Commands**:\n" +
		"• `/setmodgroup` - Set current chat as the Private Moderation Group (Admin only)\n" +
		"• `/addgroup` / `/removegroup` - Enable/disable group monitoring\n" +
		"• `/listgroups` - List all monitored groups\n" +
		"• `/rep <user|@username> [delta]` - Check or adjust reputation\n" +
		"• `/warn <user|@username>` - Issue warning & deduct rep\n" +
		"• `/mute <user|@username> [hours]` - Mute user in chat\n" +
		"• `/ban <user|@username>` - Ban user across monitored groups\n" +
		"• `/unban <user|@username>` - Unban user\n" +
		"• `/cleanup` - Manually run 7-day logs & 50-post-per-user retention cleanup"
}
