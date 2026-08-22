package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleCommand(msg *tgbotapi.Message, user *db.User) {
	cmd, args := parseCommand(msg)
	if cmd == "" {
		return
	}

	isSuperAdmin := b.cfg.SuperAdminID != 0 && user.UserID == b.cfg.SuperAdminID
	isModGroup := b.cfg.ModerationGroupID != 0 && msg.Chat.ID == b.cfg.ModerationGroupID
	isGroupAdmin := (msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()) && b.IsUserAdminInChat(msg.Chat.ID, user.UserID)
	isBotAdmin := user.IsAdmin
	isModGroupMember := b.cfg.ModerationGroupID != 0 && b.IsUserInModGroup(user.UserID)
	isAuthorized := isSuperAdmin || isModGroup || isGroupAdmin || isBotAdmin || isModGroupMember

	// Ignore all commands from non-admin users (log silently and do not reply)
	// Exception: /setsuperadmin can be executed by the first user if super_admin_id is not yet set (0)
	if !isAuthorized && !(cmd == "setsuperadmin" && b.cfg.SuperAdminID == 0) {
		log.Printf("[Bot] Ignored command '/%s' from non-admin user %d (@%s) in chat %d (%s)", cmd, user.UserID, user.Username, msg.Chat.ID, msg.Chat.Title)
		return
	}

	switch cmd {
	case "start":
		b.cmdStart(msg)
	case "help":
		b.cmdHelp(msg, isSuperAdmin, isModGroup)
	case "status":
		b.cmdStatus(msg)
	case "setsuperadmin":
		b.cmdSetSuperAdmin(msg, user, isSuperAdmin)
	case "setmodgroup":
		b.cmdSetModGroup(msg, user, isSuperAdmin)
	case "addgroup":
		b.cmdAddGroup(msg, isAuthorized)
	case "removegroup":
		b.cmdRemoveGroup(msg, isAuthorized)
	case "listgroups":
		b.cmdListGroups(msg, isAuthorized)
	case "checkperms":
		b.cmdCheckPerms(msg)
	case "flag":
		b.cmdFlag(msg, args)
	case "userinfo":
		b.cmdUserInfo(msg, args)
	case "fetchprofile", "profile":
		b.cmdFetchProfile(msg, args, isAuthorized)
	case "backfillprofiles", "backfillprofile":
		b.cmdBackfillProfiles(msg, args, isAuthorized)
	case "rep":
		b.cmdRep(msg, user, args, isAuthorized)
	case "warn":
		b.cmdWarn(msg, user, args, isAuthorized)
	case "resetwarns", "resetwarn", "unwarn", "clearwarns":
		b.cmdResetWarns(msg, args, isAuthorized)
	case "mute":
		b.cmdMute(msg, user, args, isAuthorized)
	case "ban":
		b.cmdBan(msg, user, args, isAuthorized)
	case "unban":
		b.cmdUnban(msg, args, isAuthorized)
	case "promote":
		b.cmdPromote(msg, user, args, isAuthorized)
	case "demote", "removeadmin":
		b.cmdDemote(msg, user, args, isSuperAdmin)
	case "listusers", "users":
		b.cmdListUsers(msg, isAuthorized)
	case "listunknownusers", "unknownusers", "listspambios", "spambios", "listspambiousers", "spambiousers", "spamusers":
		b.cmdListUnknownUsers(msg, args, isAuthorized)
	case "rescanusers", "rescan", "rescanprofiles":
		b.cmdRescanUsers(msg, args, isAuthorized)
	case "bancheck", "checkbans", "verifybans":
		b.cmdBanCheck(msg, args, isAuthorized)
	case "cleanup":
		b.cmdCleanup(msg, isAuthorized)
	case "getdb", "backup", "db", "dumpdb", "downloaddb":
		b.cmdGetDB(msg, user)
	}
}

func parseCommand(msg *tgbotapi.Message) (string, string) {
	cmd := msg.Command()
	if cmd == "" && strings.HasPrefix(msg.Text, "!") {
		parts := strings.Fields(msg.Text[1:])
		if len(parts) > 0 {
			cmd = parts[0]
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

	return cmd, args
}

func (b *Bot) cmdStart(msg *tgbotapi.Message) {
	b.replyText(msg, "👋 **Welcome to GoGCBot!**\n\nI am a Telegram group management and reputation bot.\nUse /help to view available commands.")
}

func (b *Bot) cmdHelp(msg *tgbotapi.Message, isSuperAdmin, isModGroup bool) {
	b.replyText(msg, getHelpText(isSuperAdmin, isModGroup))
}

func (b *Bot) cmdStatus(msg *tgbotapi.Message) {
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
			"🖼️ **Cached User Profiles**: `%d`\n"+
			"💬 **Logged Messages (7d max)**: `%d`\n"+
			"🚨 **Pending Flags**: `%d`\n"+
			"✅ **Resolved Flags**: `%d`\n\n"+
			"⚙️ **Auto-Flag**: %t (Low Rep < %d, Flag Links: %t)",
		b.botUser.UserName,
		adminSet,
		modGroupSet,
		stats.TotalGroups,
		stats.TotalUsers,
		stats.TotalUserProfiles,
		stats.TotalMessages,
		stats.PendingFlags,
		stats.ResolvedFlags,
		b.cfg.AutoFlag.Enabled,
		b.cfg.AutoFlag.LowRepThreshold,
		b.cfg.AutoFlag.FlagOnLinks,
	)
	b.replyText(msg, statusText)
}

func (b *Bot) cmdSetSuperAdmin(msg *tgbotapi.Message, user *db.User, isSuperAdmin bool) {
	if b.cfg.SuperAdminID != 0 && !isSuperAdmin {
		b.replyText(msg, fmt.Sprintf("❌ Super Admin is already set to user ID `%d`.", b.cfg.SuperAdminID))
		return
	}
	b.cfg.SuperAdminID = user.UserID
	_ = b.db.SetReputation(user.UserID, 100, "Promoted to Super Admin", user.UserID)
	user.Reputation = 100
	if err := b.SaveConfig(); err != nil {
		b.replyText(msg, fmt.Sprintf("⚠️ You (@%s, ID: `%d`) are set as Super Admin in memory, but saving config file failed: %v", user.Username, user.UserID, err))
		return
	}
	b.replyText(msg, fmt.Sprintf("✅ You (@%s, ID: `%d`) are now configured and saved as the Super Admin (Reputation: 100)!", user.Username, user.UserID))
}

func (b *Bot) cmdSetModGroup(msg *tgbotapi.Message, user *db.User, isSuperAdmin bool) {
	if !isSuperAdmin && user.UserID != b.cfg.SuperAdminID {
		b.replyText(msg, "❌ Only the Super Admin can set the moderation group.")
		return
	}
	if !msg.Chat.IsGroup() && !msg.Chat.IsSuperGroup() {
		b.replyText(msg, "❌ This command must be executed inside the target Private Moderation Group.")
		return
	}
	b.cfg.ModerationGroupID = msg.Chat.ID
	if err := b.SaveConfig(); err != nil {
		b.replyText(msg, fmt.Sprintf("⚠️ Moderation Group set, but saving config file failed: %v", err))
	}
	_ = b.db.SaveGroup(msg.Chat.ID, msg.Chat.Title, msg.Chat.Type)
	b.replyText(msg, fmt.Sprintf("✅ This group (`%s`, ID: `%d`) is now configured as the Private Moderation Group!", msg.Chat.Title, msg.Chat.ID))
}

func (b *Bot) cmdAddGroup(msg *tgbotapi.Message, isAuthorized bool) {
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
}

func (b *Bot) cmdRemoveGroup(msg *tgbotapi.Message, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}
	_ = b.db.SetGroupMonitored(msg.Chat.ID, false)
	b.replyText(msg, fmt.Sprintf("🚫 Monitoring disabled for group `%s` (ID: `%d`).", msg.Chat.Title, msg.Chat.ID))
}

func (b *Bot) cmdListGroups(msg *tgbotapi.Message, isAuthorized bool) {
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
}

func (b *Bot) cmdCheckPerms(msg *tgbotapi.Message) {
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
}

func (b *Bot) cmdFlag(msg *tgbotapi.Message, args string) {
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
	targetUser, _, err := b.db.GetOrCreateUser(targetMsg.From.ID, targetMsg.From.UserName, targetMsg.From.FirstName, targetMsg.From.LastName, b.cfg.Reputation.DefaultInitial)
	if err != nil || targetUser == nil {
		b.replyText(msg, "❌ Failed to retrieve target user for flagging.")
		return
	}
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
}

func (b *Bot) cmdUserInfo(msg *tgbotapi.Message, args string) {
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

	profileSection := "• Bio: *(Not cached - use `/fetchprofile`)*\n• Profile Photo: *(Unknown)*"
	if p, err := b.db.GetUserProfile(targetUser.UserID); err == nil && p != nil {
		if p.NotFound {
			profileSection = fmt.Sprintf("• Profile Status: ⚠️ Not Found on Telegram\n• Last Attempted: `%s`", p.FetchedAt.Format("01-02 15:04"))
		} else {
			bioSnippet := p.Bio
			if bioSnippet == "" {
				bioSnippet = "*(None)*"
			} else {
				bioSnippet = fmt.Sprintf("%q", truncateText(bioSnippet, 50))
			}
			photoStr := "❌ No photo"
			if p.HasPhoto {
				photoStr = fmt.Sprintf("✅ Yes (%d photos)", p.PhotoCount)
			}
			var extraLines strings.Builder
			if p.PersonalChatTitle != "" || p.PersonalChatUsername != "" {
				handle := ""
				if p.PersonalChatUsername != "" {
					handle = fmt.Sprintf(" (@%s)", p.PersonalChatUsername)
				}
				extraLines.WriteString(fmt.Sprintf("\n• Channel: %s%s", escapeMarkdown(p.PersonalChatTitle), escapeMarkdown(handle)))
			}
			if p.BusinessIntro != "" {
				extraLines.WriteString(fmt.Sprintf("\n• Business: %s", escapeMarkdown(truncateText(p.BusinessIntro, 50))))
			}
			profileSection = fmt.Sprintf("• Bio: %s\n• Profile Photo: %s%s\n• Profile Fetched: `%s`", bioSnippet, photoStr, extraLines.String(), p.FetchedAt.Format("01-02 15:04"))
		}
	}

	infoText := fmt.Sprintf(
		"👤 **User Info Card**:\n\n"+
			"• Name: %s %s (@%s)\n"+
			"• ID: `%d`\n"+
			"• Reputation: `%d`\n"+
			"• Warnings: `%d`\n"+
			"• Banned: %t\n"+
			"• Total Logged Posts: `%d`\n\n"+
			"📋 **Profile Info**:\n%s\n\n"+
			"📝 **Recent Activity**:\n%s",
		escapeMarkdown(targetUser.FirstName), escapeMarkdown(targetUser.LastName), escapeMarkdown(targetUser.Username),
		targetUser.UserID,
		targetUser.Reputation,
		targetUser.WarnCount,
		targetUser.IsBanned,
		msgCount,
		profileSection,
		msgsSnippet.String(),
	)
	b.replyText(msg, infoText)
}

func (b *Bot) cmdFetchProfile(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}
	targetUser := b.resolveUserFromArgsOrReply(msg, args)
	if targetUser == nil {
		b.replyText(msg, "Usage: `/fetchprofile <user_id|@username>` or reply to user message.")
		return
	}

	profile, err := b.FetchUserProfile(targetUser.UserID)
	if err != nil {
		if profile != nil && profile.NotFound {
			b.replyText(msg, fmt.Sprintf("⚠️ Profile for user `%d` (@%s) was not found on Telegram.\nMarked as not found in database (attempted: `%s`).", targetUser.UserID, targetUser.Username, profile.FetchedAt.Format("2006-01-02 15:04:05 MST")))
			return
		}
		b.replyText(msg, fmt.Sprintf("❌ Failed to fetch user profile from Telegram: %v", err))
		return
	}

	displayName := strings.TrimSpace(profile.FirstName + " " + profile.LastName)
	if displayName == "" {
		displayName = targetUser.FirstName + " " + targetUser.LastName
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = fmt.Sprintf("User %d", targetUser.UserID)
	}

	usernameStr := profile.Username
	if usernameStr == "" {
		usernameStr = targetUser.Username
	}
	if usernameStr != "" {
		usernameStr = "@" + usernameStr
	} else {
		usernameStr = "none"
	}

	bioText := profile.Bio
	if bioText == "" {
		bioText = "*(None / Not Set)*"
	} else {
		bioText = fmt.Sprintf("```\n%s\n```", escapeMarkdown(bioText))
	}

	photoStatus := "❌ No photo"
	if profile.HasPhoto {
		photoStatus = fmt.Sprintf("✅ Available (%d photo(s))", profile.PhotoCount)
		if profile.PhotoFileID != "" {
			photoStatus += fmt.Sprintf("\n• Photo File ID: `%s`", profile.PhotoFileID)
		}
	}

	cardText := fmt.Sprintf(
		"👤 **User Profile Card**:\n\n"+
			"• Name: %s (%s)\n"+
			"• User ID: `%d`\n"+
			"• Profile Photo: %s\n"+
			"• Last Fetched: `%s`\n\n"+
			"📝 **Bio**:\n%s",
		escapeMarkdown(displayName),
		escapeMarkdown(usernameStr),
		profile.UserID,
		photoStatus,
		profile.FetchedAt.Format("2006-01-02 15:04:05 MST"),
		bioText,
	)
	if profile.PersonalChatTitle != "" || profile.PersonalChatUsername != "" {
		handle := ""
		if profile.PersonalChatUsername != "" {
			handle = fmt.Sprintf(" (@%s)", profile.PersonalChatUsername)
		}
		cardText += fmt.Sprintf("\n\n📢 **Personal Channel**:\n%s%s", escapeMarkdown(profile.PersonalChatTitle), escapeMarkdown(handle))
	}
	if profile.BusinessIntro != "" {
		cardText += fmt.Sprintf("\n\n💼 **Business Intro**:\n```\n%s\n```", escapeMarkdown(profile.BusinessIntro))
	}
	b.replyText(msg, cardText)
}

func (b *Bot) cmdBackfillProfiles(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}

	force := strings.Contains(strings.ToLower(args), "force") || strings.Contains(strings.ToLower(args), "all")

	var userCount int
	if force {
		all, _ := b.db.GetAllUsers(0)
		userCount = len(all)
	} else {
		missing, _ := b.db.GetUsersWithoutProfile(0)
		userCount = len(missing)
	}

	if userCount == 0 {
		b.replyText(msg, "ℹ️ All tracked users already have profiles saved in `user_profiles`. (Use `/backfillprofiles force` to force re-fetch all).")
		return
	}

	b.replyText(msg, fmt.Sprintf("⏳ Starting Telegram profile backfill for `%d` users (Force: `%t`)... Running in background.", userCount, force))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		success, failed, err := b.BackfillUserProfiles(ctx, 100*time.Millisecond, force, nil)
		if err != nil && err != context.Canceled {
			b.replyText(msg, fmt.Sprintf("⚠️ Profile backfill finished with error: %v (Success: `%d`, Failed: `%d`)", err, success, failed))
			return
		}
		b.replyText(msg, fmt.Sprintf("✅ **User Profile Backfill Complete**!\n\n• Successfully Fetched: `%d`\n• Failed / Not Found: `%d`", success, failed))
	}()
}

func (b *Bot) cmdRescanUsers(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}

	opts := RescanOptions{
		MaxReputation: 20,
		Hours:         24,
		Delay:         100 * time.Millisecond,
	}

	lowerArgs := strings.ToLower(args)
	if strings.Contains(lowerArgs, "force") || strings.Contains(lowerArgs, "all") {
		opts.Force = true
	}
	if strings.Contains(lowerArgs, "dryrun") || strings.Contains(lowerArgs, "dry") {
		opts.DryRun = true
	}

	// Parse custom max rep or hours if supplied: e.g. "maxrep 30" or "hours 12"
	fields := strings.Fields(lowerArgs)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if (f == "rep" || f == "maxrep" || f == "max-rep") && i+1 < len(fields) {
			if v, err := strconv.Atoi(fields[i+1]); err == nil {
				opts.MaxReputation = v
				i++
			}
		} else if (f == "hours" || f == "hr" || f == "age") && i+1 < len(fields) {
			if v, err := strconv.Atoi(fields[i+1]); err == nil {
				opts.Hours = v
				i++
			}
		}
	}

	var cutoff time.Time
	if !opts.Force && opts.Hours > 0 {
		cutoff = time.Now().Add(-time.Duration(opts.Hours) * time.Hour)
	}

	candidates, err := b.db.GetLowRepUsersForRescan(opts.MaxReputation, cutoff, 0)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Error querying candidate users for rescan: %v", err))
		return
	}

	if len(candidates) == 0 {
		b.replyText(msg, fmt.Sprintf("ℹ️ No low-reputation users (rep <= %d) found needing rescan (last scan > %d hours ago). Use `/rescanusers force` to rescan all low-rep users.", opts.MaxReputation, opts.Hours))
		return
	}

	modeStr := ""
	if opts.DryRun {
		modeStr = " (DRY RUN - No bans will be executed)"
	}
	b.replyText(msg, fmt.Sprintf("🔍 **Starting user rescan** for `%d` candidate users%s...\n• Max Reputation: `%d`\n• Scan Age Threshold: `>%d hours` (Force: `%t`)\n\nRunning in background. Summary will be reported when finished.", len(candidates), modeStr, opts.MaxReputation, opts.Hours, opts.Force))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		res, err := b.RescanLowRepUsers(ctx, opts, nil)
		if err != nil && err != context.Canceled {
			b.replyText(msg, fmt.Sprintf("⚠️ User rescan encountered an error: %v", err))
			return
		}

		b.replyText(msg, fmt.Sprintf(
			"✅ **User Rescan Complete**!\n\n"+
				"📊 **Summary**:\n"+
				"• Total Candidates: `%d`\n"+
				"• Scanned: `%d`\n"+
				"• 🚫 Banned (Triggered Join Rules): `%d`\n"+
				"• ✨ Clean: `%d`\n"+
				"• ⚠️ Errors / Not Found: `%d`\n"+
				"• Dry Run: `%t`",
			res.TotalCandidates, res.ScannedCount, res.BannedCount, res.CleanCount, res.ErrorCount, opts.DryRun,
		))
	}()
}

func (b *Bot) cmdBanCheck(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied. You must be an administrator or moderation group member.")
		return
	}

	if !b.TryStartBanCheck() {
		b.replyText(msg, "⚠️ A ban check is already in progress. Please wait for it to complete.")
		return
	}

	opts := BanCheckOptions{
		Delay: 1 * time.Second,
	}

	lowerArgs := strings.ToLower(args)
	if strings.Contains(lowerArgs, "dryrun") || strings.Contains(lowerArgs, "dry") {
		opts.DryRun = true
	}

	// Parse optional custom delay (must be >= 1s / 1000ms)
	fields := strings.Fields(lowerArgs)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if (f == "delay" || f == "delay-ms" || f == "delayms" || f == "rate") && i+1 < len(fields) {
			if v, err := strconv.Atoi(fields[i+1]); err == nil {
				if v > 0 {
					opts.Delay = time.Duration(v) * time.Millisecond
				}
				i++
			}
		}
	}

	bannedUsers, err := b.db.GetBannedUsers()
	if err != nil {
		b.FinishBanCheck()
		b.replyText(msg, fmt.Sprintf("❌ Error querying banned users: %v", err))
		return
	}
	if len(bannedUsers) == 0 {
		b.FinishBanCheck()
		b.replyText(msg, "ℹ️ No banned users found in database.")
		return
	}

	groups, err := b.db.GetMonitoredGroups()
	if err != nil {
		b.FinishBanCheck()
		b.replyText(msg, fmt.Sprintf("❌ Error querying monitored groups: %v", err))
		return
	}
	if len(groups) == 0 {
		b.FinishBanCheck()
		b.replyText(msg, "ℹ️ No monitored groups/channels found in database.")
		return
	}

	totalChecks := len(bannedUsers) * len(groups)
	estMinutes := (totalChecks * int(opts.Delay/time.Second)) / 60
	if estMinutes < 1 {
		estMinutes = 1
	}

	modeStr := ""
	if opts.DryRun {
		modeStr = " (DRY RUN - No kick bans will be executed)"
	}

	b.replyText(msg, fmt.Sprintf(
		"🔍 **Starting Ban Check across monitored channels & groups**%s...\n\n"+
			"• 🚫 **Banned Users in DB**: `%d`\n"+
			"• 👥 **Monitored Groups/Channels**: `%d`\n"+
			"• 📋 **Total Checks**: `%d`\n"+
			"• ⏱️ **Rate Limit**: `<= 1 req/sec` (~`%d` min est.)\n\n"+
			"Running in background. Summary report will be sent here upon completion.",
		modeStr, len(bannedUsers), len(groups), totalChecks, estMinutes,
	))

	go func() {
		defer b.FinishBanCheck()

		timeout := time.Duration(totalChecks*3)*time.Second + 10*time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		res, err := b.CheckBannedUsersAcrossGroups(ctx, opts, nil)
		if err != nil && err != context.Canceled {
			b.replyText(msg, fmt.Sprintf("⚠️ Ban check encountered an error: %v", err))
			return
		}

		if res == nil {
			return
		}

		b.replyText(msg, fmt.Sprintf(
			"✅ **Ban Check Complete**!\n\n"+
				"📊 **Summary**:\n"+
				"• 🚫 Banned Users in DB: `%d`\n"+
				"• 👥 Monitored Groups/Channels: `%d`\n"+
				"• 📋 Total Group-User Checks: `%d`\n"+
				"• ✅ Already Banned: `%d`\n"+
				"• 🔨 Newly Re-banned / Enforced: `%d`\n"+
				"• ⚠️ Errors: `%d`\n"+
				"• ⏱️ Elapsed Time: `%s`\n"+
				"• 🧪 Dry Run: `%t`",
			res.TotalBannedUsers, res.TotalGroups, res.TotalChecks,
			res.AlreadyBanned, res.RebannedCount, res.ErrorCount,
			res.Duration.Round(time.Second), opts.DryRun,
		))
	}()
}

func parseRepArgs(args string, isReply bool) (targetArg string, isAbsolute bool, absVal int, hasDelta bool, deltaVal int) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return "", false, 0, false, 0
	}

	for i := 0; i < len(parts); i++ {
		p := parts[i]

		if strings.HasPrefix(p, "=") {
			valStr := strings.TrimPrefix(p, "=")
			if valStr == "" && i+1 < len(parts) {
				valStr = parts[i+1]
				i++
			}
			if v, err := strconv.Atoi(valStr); err == nil {
				isAbsolute = true
				absVal = v
				continue
			}
		}

		if strings.HasPrefix(p, "+") || strings.HasPrefix(p, "-") {
			if v, err := strconv.Atoi(p); err == nil {
				hasDelta = true
				deltaVal = v
				continue
			}
		}

		if strings.HasPrefix(p, "@") && targetArg == "" {
			targetArg = p
			continue
		}

		if v, err := strconv.Atoi(p); err == nil {
			if targetArg == "" && !isReply && (v > 10000 || v < -10000) {
				targetArg = p
			} else if !hasDelta && !isAbsolute {
				hasDelta = true
				deltaVal = v
			}
		} else if targetArg == "" && !isReply {
			targetArg = p
		}
	}

	return targetArg, isAbsolute, absVal, hasDelta, deltaVal
}

func (b *Bot) cmdRep(msg *tgbotapi.Message, user *db.User, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}

	isReply := msg.ReplyToMessage != nil
	targetArg, isAbsolute, absVal, hasDelta, deltaVal := parseRepArgs(args, isReply)

	var targetUser *db.User
	if isReply {
		targetUser = b.resolveUserFromArgsOrReply(msg, "")
	} else if targetArg != "" {
		targetUser = b.resolveUserFromArgsOrReply(msg, targetArg)
	}

	if targetUser == nil {
		if targetArg != "" {
			b.replyText(msg, fmt.Sprintf("❌ User '%s' was not found in the database.\n\n👉 **Tip**: If the user hasn't posted in chat yet, use their numeric Telegram User ID (e.g. `/rep 123456789 =100`), reply to their message, or tag them from Telegram's mention dropdown.", targetArg))
		} else {
			b.replyText(msg, "Usage: `/rep <user_id|@username> [delta|=value]` or reply to user message e.g. `/rep =100` or `/rep +10`")
		}
		return
	}

	if isAbsolute {
		err := b.db.SetReputation(targetUser.UserID, absVal, "Manual absolute rep setting", user.UserID)
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Error setting reputation: %v", err))
			return
		}
		targetUser.Reputation = absVal
		b.replyText(msg, fmt.Sprintf("⭐ Set reputation for @%s (ID: `%d`) to `= %d`.", targetUser.Username, targetUser.UserID, absVal))
	} else if hasDelta {
		newRep, err := b.db.AdjustReputation(targetUser.UserID, deltaVal, "Manual CLI command adjustment", user.UserID)
		if err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Error adjusting reputation: %v", err))
			return
		}
		targetUser.Reputation = newRep
		b.replyText(msg, fmt.Sprintf("⭐ Updated reputation for @%s (ID: `%d`). Delta: `%+d` -> New Rep: `%d`.", targetUser.Username, targetUser.UserID, deltaVal, newRep))
	} else {
		b.replyText(msg, fmt.Sprintf("⭐ User @%s (ID: `%d`) Reputation: `%d`.", targetUser.Username, targetUser.UserID, targetUser.Reputation))
	}
}

func (b *Bot) cmdWarn(msg *tgbotapi.Message, user *db.User, args string, isAuthorized bool) {
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
}

func (b *Bot) cmdResetWarns(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}
	targetUser := b.resolveUserFromArgsOrReply(msg, args)
	if targetUser == nil {
		b.replyText(msg, "Usage: `/resetwarns <user_id|@username>` or reply to user message.")
		return
	}

	if err := b.db.ResetWarnings(targetUser.UserID); err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Failed to reset warnings for @%s: %v", targetUser.Username, err))
		return
	}

	targetUser.WarnCount = 0
	b.replyText(msg, fmt.Sprintf("🧹 Reset all warnings for @%s (ID: `%d`). Warn count: `0`.", targetUser.Username, targetUser.UserID))
}

func (b *Bot) cmdMute(msg *tgbotapi.Message, user *db.User, args string, isAuthorized bool) {
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
}

func (b *Bot) cmdBan(msg *tgbotapi.Message, user *db.User, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}
	targetUser := b.resolveUserFromArgsOrReply(msg, args)
	if targetUser == nil {
		b.replyText(msg, "Usage: `/ban <user_id|@username>`")
		return
	}
	err := b.BanUserAcrossAllGroups(targetUser.UserID, msg.Chat.ID)
	newRep, _ := b.db.AdjustReputation(targetUser.UserID, -b.cfg.Reputation.BanPenalty, "Banned by command", user.UserID)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("⚠️ User @%s (ID: `%d`) is set as Banned in DB, but Telegram kick failed: %v.\n\n👉 **Ensure the bot is a Telegram Administrator with 'Restrict Members' (can_restrict_members) permission.**", targetUser.Username, targetUser.UserID, err))
		return
	}
	b.replyText(msg, fmt.Sprintf("🚫 Banned & kicked @%s across monitored groups. Rep: `%d` (-%d)", targetUser.Username, newRep, b.cfg.Reputation.BanPenalty))
}

func (b *Bot) cmdUnban(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}
	targetUser := b.resolveUserFromArgsOrReply(msg, args)
	if targetUser == nil {
		b.replyText(msg, "Usage: `/unban <user_id|@username>`")
		return
	}
	_ = b.UnbanUserAcrossAllGroups(targetUser.UserID, msg.Chat.ID)
	b.replyText(msg, fmt.Sprintf("✅ Unbanned @%s across monitored groups.", targetUser.Username))
}

func (b *Bot) cmdPromote(msg *tgbotapi.Message, user *db.User, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}
	targetUser := b.resolveUserFromArgsOrReply(msg, args)
	if targetUser == nil {
		b.replyText(msg, "Usage: `/promote <user_id|@username>` or reply to user message.")
		return
	}
	err := b.PromoteUserInBot(targetUser.UserID)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Failed to promote user in GoGCBot: %v", err))
		return
	}
	_ = b.db.SetReputation(targetUser.UserID, 100, "Promoted to Bot Admin", user.UserID)
	targetUser.IsAdmin = true
	targetUser.Reputation = 100
	b.replyText(msg, fmt.Sprintf("👑 Promoted @%s (ID: `%d`) to Bot Administrator in GoGCBot! Reputation set to 100.", targetUser.Username, targetUser.UserID))
}

func (b *Bot) cmdDemote(msg *tgbotapi.Message, user *db.User, args string, isSuperAdmin bool) {
	if !isSuperAdmin {
		b.replyText(msg, "❌ Only the Super Admin can remove bot admin status.")
		return
	}
	targetUser := b.resolveUserFromArgsOrReply(msg, args)
	if targetUser == nil {
		b.replyText(msg, "Usage: `/demote <user_id|@username>` or reply to user message.")
		return
	}
	if targetUser.UserID == b.cfg.SuperAdminID {
		b.replyText(msg, "❌ Cannot demote the Super Admin.")
		return
	}

	err := b.DemoteUserInBot(targetUser.UserID)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Failed to demote user in GoGCBot: %v", err))
		return
	}
	_ = b.db.SetReputation(targetUser.UserID, b.cfg.Reputation.DefaultInitial, "Demoted from Bot Admin by Super Admin", user.UserID)
	targetUser.IsAdmin = false
	targetUser.Reputation = b.cfg.Reputation.DefaultInitial

	b.replyText(msg, fmt.Sprintf("🔻 Demoted @%s (ID: `%d`) from Bot Administrator to Regular User in GoGCBot. Reputation reset to %d.", targetUser.Username, targetUser.UserID, b.cfg.Reputation.DefaultInitial))
}

func (b *Bot) cmdListUsers(msg *tgbotapi.Message, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}

	users, err := b.db.GetAllUsers(50)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Error fetching users: %v", err))
		return
	}
	if len(users) == 0 {
		b.replyText(msg, "No tracked users in database.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👥 **Known Users & Reputation List** (Total: %d):\n\n", len(users)))

	for i, u := range users {
		var flags []string
		if b.cfg.SuperAdminID != 0 && u.UserID == b.cfg.SuperAdminID {
			flags = append(flags, "SuperAdmin 👑")
		} else if u.IsAdmin {
			flags = append(flags, "BotAdmin 🛡️")
		} else if b.IsUserAdminInChat(msg.Chat.ID, u.UserID) {
			flags = append(flags, "GroupAdmin 👥")
		}
		if u.IsBanned {
			flags = append(flags, "Banned 🚫")
		}
		if u.WarnCount > 0 {
			flags = append(flags, fmt.Sprintf("Warns:%d ⚠️", u.WarnCount))
		}

		flagStr := "-"
		if len(flags) > 0 {
			flagStr = strings.Join(flags, ", ")
		}

		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = "Unknown"
		}
		userHandle := ""
		if u.Username != "" {
			userHandle = fmt.Sprintf(" (@%s)", escapeMarkdown(u.Username))
		}

		sb.WriteString(fmt.Sprintf(
			"%d. **%s**%s\n   • ID: `%d` | Rep: `%d` | Flags: `%s`\n",
			i+1, escapeMarkdown(name), userHandle, u.UserID, u.Reputation, flagStr,
		))
	}

	b.replyText(msg, sb.String())
}

func (b *Bot) cmdListUnknownUsers(msg *tgbotapi.Message, args string, isAuthorized bool) {
	if !isAuthorized {
		b.replyText(msg, "❌ Permission denied.")
		return
	}

	parts := strings.Fields(args)
	shouldBan := false
	var kwParts []string
	for _, p := range parts {
		if strings.EqualFold(p, "ban") || strings.EqualFold(p, "--ban") || strings.EqualFold(p, "-b") {
			shouldBan = true
		} else {
			kwParts = append(kwParts, p)
		}
	}
	keyword := strings.TrimSpace(strings.Join(kwParts, " "))

	limit := 30
	if shouldBan {
		limit = 100
	}

	opts := db.UnknownUserOptions{
		Keyword:            keyword,
		ConfiguredKeywords: b.cfg.AutoFlag.BlockedKeywords,
		MaxPosts:           5,
		MaxReputation:      20,
		Limit:              limit,
	}

	items, err := b.db.GetUnbannedUnknownUsers(opts)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Error querying unknown users: %v", err))
		return
	}

	if len(items) == 0 {
		filterMsg := ""
		if keyword != "" {
			filterMsg = fmt.Sprintf(" matching %q", keyword)
		}
		b.replyText(msg, fmt.Sprintf("✅ No unbanned unknown new users found%s.", filterMsg))
		return
	}

	if shouldBan {
		var matchingUsers []db.UnknownUserItem
		for _, u := range items {
			if u.IsSpamMatch || len(u.MatchedKeywords) > 0 {
				matchingUsers = append(matchingUsers, u)
			}
		}

		if len(matchingUsers) == 0 {
			b.replyText(msg, "✅ No unbanned users matching the spam filter to ban.")
			return
		}

		// Send immediate confirmation reply
		b.replyText(msg, fmt.Sprintf("🔨 **Batch Spam Ban Initiated**\nFound `%d` unbanned user(s) matching spam filter. Banning across all monitored groups in background...", len(matchingUsers)))

		// Execute in background
		go func(senderChatID int64) {
			var successCount int
			var failCount int
			var actionLogs []string

			for i, u := range matchingUsers {
				displayName := strings.TrimSpace(u.FirstName + " " + u.LastName)
				if displayName == "" {
					displayName = "Unknown"
				}
				userHandle := ""
				if u.Username != "" {
					userHandle = " (@" + escapeMarkdown(u.Username) + ")"
				}
				kwStr := truncateText(strings.Join(u.MatchedKeywords, ", "), 40)

				err := b.BanUserAcrossAllGroups(u.UserID, senderChatID)
				if err != nil {
					failCount++
					log.Printf("[BatchBan] Failed to ban user %d across groups: %v", u.UserID, err)
					actionLogs = append(actionLogs, fmt.Sprintf("%d. ❌ **%s**%s (`%d`) - Failed: `%v`", i+1, escapeMarkdown(displayName), userHandle, u.UserID, err))
				} else {
					successCount++
					log.Printf("[BatchBan] Successfully banned user %d across groups", u.UserID)
					actionLogs = append(actionLogs, fmt.Sprintf("%d. ✅ **%s**%s (`%d`) - Banned [Matched: `%s`]", i+1, escapeMarkdown(displayName), userHandle, u.UserID, escapeMarkdown(kwStr)))
				}
			}

			// Build summary report
			var sb strings.Builder
			sb.WriteString("🔨 **Batch Spam Ban Summary Report**\n\n")
			sb.WriteString(fmt.Sprintf("• **Total Evaluated**: `%d`\n", len(matchingUsers)))
			sb.WriteString(fmt.Sprintf("• **Successfully Banned**: `%d`\n", successCount))
			sb.WriteString(fmt.Sprintf("• **Failed Actions**: `%d`\n\n", failCount))
			sb.WriteString("**Action Details**:\n")

			for _, logLine := range actionLogs {
				sb.WriteString(logLine + "\n")
			}

			summaryMsg := sb.String()

			// Send summary back to sender chat
			b.sendChunkedText(senderChatID, summaryMsg)

			// Send summary to Private Moderation Group if configured and different from sender chat
			if b.cfg.ModerationGroupID != 0 && b.cfg.ModerationGroupID != senderChatID {
				b.sendChunkedText(b.cfg.ModerationGroupID, summaryMsg)
			}
		}(msg.Chat.ID)
		return
	}

	output := formatUnknownUsersTable(items, keyword)
	b.replyText(msg, output)
}

// cmdListSpamBios is a backwards-compatible alias for cmdListUnknownUsers.
func (b *Bot) cmdListSpamBios(msg *tgbotapi.Message, args string, isAuthorized bool) {
	b.cmdListUnknownUsers(msg, args, isAuthorized)
}

// formatUnknownUsersTable formats the slice of UnknownUserItem into a compact monospace table for Telegram output.
func formatUnknownUsersTable(items []db.UnknownUserItem, keyword string) string {
	var sb strings.Builder
	filterHeader := ""
	if keyword != "" {
		filterHeader = fmt.Sprintf(" [Filter: `%s`]", escapeMarkdown(keyword))
	}
	sb.WriteString(fmt.Sprintf("🚨 **Unbanned Unknown / New Users** (Found: %d)%s:\n\n", len(items), filterHeader))

	sb.WriteString("```\n")
	sb.WriteString(" # | User ID    | User         | Msgs | Match      | Bio / Profile Snippet\n")
	sb.WriteString("---+------------+--------------+------+------------+------------------------------\n")

	for i, u := range items {
		idxStr := fmt.Sprintf("%2d", i+1)
		idStr := padRightVisual(fmt.Sprintf("%d", u.UserID), 10)

		var userDisplay string
		if u.Username != "" {
			userDisplay = "@" + u.Username
		} else {
			userDisplay = strings.TrimSpace(u.FirstName + " " + u.LastName)
			if userDisplay == "" {
				userDisplay = "Unknown"
			}
		}
		userStr := padRightVisual(truncateVisual(userDisplay, 12), 12)
		msgsStr := fmt.Sprintf("%4d", u.MessageCount)

		var matchDisplay string
		if len(u.MatchedKeywords) > 0 {
			matchDisplay = strings.Join(u.MatchedKeywords, ",")
		} else if u.IsSpamMatch {
			matchDisplay = "Spam"
		} else {
			matchDisplay = "-"
		}
		matchStr := padRightVisual(truncateVisual(matchDisplay, 10), 10)

		// Sanitize bio / snippet: flatten newlines and replace backticks
		profileText := u.Bio
		if profileText == "" && u.PersonalChatTitle != "" {
			profileText = "[Chan] " + u.PersonalChatTitle
		} else if profileText == "" && u.BusinessIntro != "" {
			profileText = "[Biz] " + u.BusinessIntro
		}

		cleanSnippet := strings.ReplaceAll(profileText, "\r\n", " ")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "\n", " ")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "\r", " ")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "\t", " ")
		cleanSnippet = strings.ReplaceAll(cleanSnippet, "`", "'")
		cleanSnippet = strings.Join(strings.Fields(cleanSnippet), " ")
		if cleanSnippet == "" {
			cleanSnippet = "-"
		}
		bioSnippet := truncateVisual(cleanSnippet, 30)

		sb.WriteString(fmt.Sprintf("%s | %s | %s | %s | %s | %s\n", idxStr, idStr, userStr, msgsStr, matchStr, bioSnippet))
	}
	sb.WriteString("```\n\n")
	sb.WriteString("💡 **Actions**: `/listunknownusers ban` to ban all matching • `/ban <id>` to ban individual user")

	return sb.String()
}

// formatSpamBioTable is an alias for formatUnknownUsersTable for backwards compatibility.
func formatSpamBioTable(items []db.SpamBioUserItem, keyword string) string {
	return formatUnknownUsersTable(items, keyword)
}

func runeVisualWidth(r rune) int {
	if r == 0 || (r >= 0x00 && r < 0x20) || (r >= 0x7F && r < 0xA0) {
		return 0
	}
	if (r >= 0x1100 && r <= 0x115F) || // Hangul Jamo
		(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) || // CJK Radicals, Symbols, Chinese, Japanese, Yi
		(r >= 0xAC00 && r <= 0xD7A3) || // Hangul Syllables
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0xFE10 && r <= 0xFE19) || // Vertical forms
		(r >= 0xFE30 && r <= 0xFE6F) || // CJK Compatibility Forms
		(r >= 0xFF01 && r <= 0xFF60) || // Fullwidth Forms
		(r >= 0xFFE0 && r <= 0xFFE6) || // Fullwidth Symbols
		(r >= 0x1F000 && r <= 0x1FAFF) || // Emojis and Pictographs
		(r >= 0x20000 && r <= 0x2FFFD) || // CJK Extension B-F
		(r >= 0x30000 && r <= 0x3FFFD) {
		return 2
	}
	return 1
}

func visualStringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeVisualWidth(r)
	}
	return w
}

func padRightVisual(s string, targetWidth int) string {
	w := visualStringWidth(s)
	if w >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-w)
}

func truncateVisual(s string, maxWidth int) string {
	s = strings.ToValidUTF8(s, "")
	curWidth := visualStringWidth(s)
	if curWidth <= maxWidth {
		return s
	}

	limit := maxWidth - 3
	if limit <= 0 {
		limit = maxWidth
	}

	var sb strings.Builder
	accum := 0
	for _, r := range s {
		rw := runeVisualWidth(r)
		if accum+rw > limit {
			break
		}
		sb.WriteRune(r)
		accum += rw
	}
	if limit < maxWidth {
		sb.WriteString("...")
	}
	return sb.String()
}

func (b *Bot) cmdCleanup(msg *tgbotapi.Message, isAuthorized bool) {
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

func (b *Bot) cmdGetDB(msg *tgbotapi.Message, user *db.User) {
	// Only allow in private direct chats, never in a group/supergroup/channel
	if !msg.Chat.IsPrivate() {
		b.replyText(msg, "❌ For security, database copies can only be requested in a direct private message to the bot.")
		return
	}

	if !b.IsBotAdminUser(user) {
		b.replyText(msg, "❌ Permission denied. You must be a Bot Administrator or member of the Bot Admin group.")
		return
	}

	tmpDir, err := os.MkdirTemp("", "gogcbot-db-backup-*")
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Failed to create temporary directory for database backup: %v", err))
		return
	}
	defer os.RemoveAll(tmpDir)

	backupFilename := fmt.Sprintf("gogcbot-backup-%s.db", time.Now().Format("20060102-150405"))
	backupFilePath := filepath.Join(tmpDir, backupFilename)

	if err := b.db.BackupTo(backupFilePath); err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Failed to generate database backup: %v", err))
		return
	}

	fileInfo, err := os.Stat(backupFilePath)
	if err != nil {
		b.replyText(msg, fmt.Sprintf("❌ Failed to read database backup file: %v", err))
		return
	}

	userDisplayName := user.Username
	if userDisplayName != "" {
		userDisplayName = "@" + userDisplayName
	} else {
		userDisplayName = strings.TrimSpace(user.FirstName + " " + user.LastName)
		if userDisplayName == "" {
			userDisplayName = fmt.Sprintf("User %d", user.UserID)
		}
	}

	doc := tgbotapi.NewDocument(msg.Chat.ID, tgbotapi.FilePath(backupFilePath))
	doc.Caption = fmt.Sprintf("📦 **GoGCBot SQLite3 Database Backup**\n\n"+
		"🕒 **Generated**: `%s`\n"+
		"💾 **Size**: `%.2f KB` (`%d` bytes)\n"+
		"👤 **Recipient**: %s (`%d`)",
		time.Now().Format("2006-01-02 15:04:05 MST"),
		float64(fileInfo.Size())/1024.0,
		fileInfo.Size(),
		escapeMarkdown(userDisplayName),
		user.UserID,
	)
	doc.ParseMode = tgbotapi.ModeMarkdown

	if _, err := b.Send(doc); err != nil {
		doc.ParseMode = ""
		if _, err = b.Send(doc); err != nil {
			b.replyText(msg, fmt.Sprintf("❌ Failed to send database backup file over Telegram: %v", err))
			return
		}
	}
}

func (b *Bot) resolveUserFromArgsOrReply(msg *tgbotapi.Message, args string) *db.User {
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
		u, _, err := b.db.GetOrCreateUser(msg.ReplyToMessage.From.ID, msg.ReplyToMessage.From.UserName, msg.ReplyToMessage.From.FirstName, msg.ReplyToMessage.From.LastName, b.cfg.Reputation.DefaultInitial)
		if err == nil {
			return u
		}
	}

	// Check Telegram Entities for direct user text_mention
	for _, entity := range msg.Entities {
		if entity.Type == "text_mention" && entity.User != nil {
			u, _, err := b.db.GetOrCreateUser(entity.User.ID, entity.User.UserName, entity.User.FirstName, entity.User.LastName, b.cfg.Reputation.DefaultInitial)
			if err == nil {
				return u
			}
		}
		if entity.Type == "mention" && msg.Text != "" {
			runes := []rune(msg.Text)
			if entity.Offset+entity.Length <= len(runes) {
				mentionText := string(runes[entity.Offset : entity.Offset+entity.Length])
				u, err := b.db.GetUserByUsername(mentionText)
				if err == nil {
					return u
				}
			}
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
		if id, err := strconv.ParseInt(p, 10, 64); err == nil && (id > 10000 || id < -10000) {
			u, err := b.db.GetUserByID(id)
			if err == nil {
				return u
			}
			u, _, err = b.db.GetOrCreateUser(id, "", "User", fmt.Sprintf("%d", id), b.cfg.Reputation.DefaultInitial)
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
	cm, err := b.GetChatMember(tgbotapi.GetChatMemberConfig{
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

// IsUserInModGroup checks if a given user ID is a member, administrator, or creator in the configured Moderation Group.
func (b *Bot) IsUserInModGroup(userID int64) bool {
	if b.cfg.ModerationGroupID == 0 || userID == 0 {
		return false
	}
	cm, err := b.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: b.cfg.ModerationGroupID,
			UserID: userID,
		},
	})
	if err != nil {
		return false
	}
	return cm.Status == "administrator" || cm.Status == "creator" || cm.Status == "member"
}

// IsBotAdminUser checks if a given user is a Super Admin, has bot admin privileges in the DB, or is a member of the moderation/admin group.
func (b *Bot) IsBotAdminUser(user *db.User) bool {
	if user == nil {
		return false
	}
	if b.cfg.SuperAdminID != 0 && user.UserID == b.cfg.SuperAdminID {
		return true
	}
	if user.IsAdmin {
		return true
	}
	if b.cfg.ModerationGroupID != 0 && b.IsUserInModGroup(user.UserID) {
		return true
	}
	return false
}

func (b *Bot) replyText(msg *tgbotapi.Message, text string) {
	if msg == nil || text == "" {
		return
	}
	text = strings.ToValidUTF8(text, "")
	runes := []rune(text)
	const maxRunes = 3800
	for len(runes) > maxRunes {
		sub := string(runes[:maxRunes])
		splitIdx := strings.LastIndex(sub, "\n\n")
		if splitIdx == -1 {
			splitIdx = strings.LastIndex(sub, "\n")
		}
		var chunk string
		if splitIdx != -1 {
			chunk = sub[:splitIdx]
			runes = runes[len([]rune(chunk))+1:]
		} else {
			chunk = sub
			runes = runes[maxRunes:]
		}

		reply := tgbotapi.NewMessage(msg.Chat.ID, strings.ToValidUTF8(chunk, ""))
		reply.ReplyToMessageID = msg.MessageID
		reply.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.Send(reply); err != nil {
			log.Printf("[Bot] Markdown send failed (%v), retrying as plain text", err)
			reply.ParseMode = ""
			_, _ = b.Send(reply)
		}
	}

	if len(runes) > 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, strings.ToValidUTF8(string(runes), ""))
		reply.ReplyToMessageID = msg.MessageID
		reply.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.Send(reply); err != nil {
			log.Printf("[Bot] Markdown send failed (%v), retrying as plain text", err)
			reply.ParseMode = ""
			_, _ = b.Send(reply)
		}
	}
}

func (b *Bot) sendChunkedText(chatID int64, text string) {
	if chatID == 0 || text == "" {
		return
	}
	text = strings.ToValidUTF8(text, "")
	runes := []rune(text)
	const maxRunes = 3800
	for len(runes) > maxRunes {
		sub := string(runes[:maxRunes])
		splitIdx := strings.LastIndex(sub, "\n")
		var chunk string
		if splitIdx != -1 {
			chunk = sub[:splitIdx]
			runes = runes[len([]rune(chunk))+1:]
		} else {
			chunk = sub
			runes = runes[maxRunes:]
		}

		msg := tgbotapi.NewMessage(chatID, strings.ToValidUTF8(chunk, ""))
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.Send(msg); err != nil {
			msg.ParseMode = ""
			_, _ = b.Send(msg)
		}
	}

	if len(runes) > 0 {
		msg := tgbotapi.NewMessage(chatID, strings.ToValidUTF8(string(runes), ""))
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.Send(msg); err != nil {
			msg.ParseMode = ""
			_, _ = b.Send(msg)
		}
	}
}

func boolStatus(val bool) string {
	if val {
		return "✅ Allowed"
	}
	return "❌ Missing"
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
		"• `/resetwarns <user|@username>` - Reset warnings for user to 0\n" +
		"• `/mute <user|@username> [hours]` - Mute user in chat\n" +
		"• `/ban <user|@username>` - Ban user across monitored groups\n" +
		"• `/unban <user|@username>` - Unban user\n" +
		"• `/promote <user|@username>` - Promote user to Group Admin & set rep to 100\n" +
		"• `/demote <user|@username>` - Remove admin status & reset rep (Super Admin only)\n" +
		"• `/listusers` - List all known users, reputation scores & admin flags\n" +
		"• `/listunknownusers [kw] [ban]` - List or batch-ban unbanned new users with few messages (with or without bios)\n" +
		"• `/cleanup` - Manually run 7-day logs & 50-post-per-user retention cleanup\n" +
		"• `/getdb` - Download a copy of the current SQLite3 database (Admin direct message only)\n" +
		"• `/fetchprofile <user|@username>` - Fetch fresh Telegram profile (bio & picture) & cache in DB\n" +
		"• `/backfillprofiles [force]` - Backfill bios and profile photos for tracked users in background\n" +
		"• `/rescanusers [force] [dryrun] [maxrep <n>]` - Rescan low-rep users (>24h since last scan) & trigger join ban rules\n" +
		"• `/bancheck [dryrun]` - Verify all banned users across monitored channels/groups (<= 1 req/sec) & enforce missing bans"
}
