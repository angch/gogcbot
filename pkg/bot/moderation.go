package bot

import (
	"context"
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
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "[", "\\[")
	return s
}

func truncateText(text string, maxLen int) string {
	text = strings.ToValidUTF8(text, "")
	runes := []rune(text)
	if maxLen <= 0 || len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}

// SendTriggerBanAlert sends a notification message to the management monitoring channel (b.cfg.ModerationGroupID)
// whenever a user ban is triggered by an automated detection trigger.
func (b *Bot) SendTriggerBanAlert(chatID int64, user *db.User, msg *db.Message, reason string) error {
	modGroupID := b.cfg.ModerationGroupID
	if modGroupID == 0 {
		log.Printf("[Bot] Warning: ModerationGroupID not set. Cannot send trigger ban alert for user %d in chat %d", user.UserID, chatID)
		return nil
	}

	groupTitle := fmt.Sprintf("Chat %d", chatID)
	if chatID == 0 {
		groupTitle = "All Monitored Groups"
	} else if group, err := b.db.GetGroup(chatID); err == nil && group != nil && group.Title != "" {
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

	msgID := 0
	msgContent := ""
	if msg != nil {
		msgID = msg.MessageID
		msgContent = msg.Text
	}
	if strings.TrimSpace(msgContent) == "" {
		if msg != nil && msg.HasMedia {
			msgContent = "[Media message]"
		} else {
			msgContent = "(empty message)"
		}
	}

	text := fmt.Sprintf(
		"🚫 **TRIGGER BAN EXECUTED**\n\n"+
			"📌 **Reason**: `%s`\n\n"+
			"👤 **User**: %s (@%s)\n"+
			"🆔 **User ID**: `%d`\n"+
			"⭐ **Reputation**: `%d` | ⚠️ **Warns**: `%d` | 💬 **Logged Posts**: `%d`\n\n"+
			"👥 **Group**: %s (`%d`)\n"+
			"🆔 **Message ID**: `%d`\n"+
			"🕒 **Time**: `%s`\n\n"+
			"📝 **Message Snippet**:\n```\n%s\n```",
		escapeMarkdown(reason),
		escapeMarkdown(userDisplayName),
		escapeMarkdown(usernameStr),
		user.UserID,
		user.Reputation,
		user.WarnCount,
		totalUserMsgs,
		escapeMarkdown(groupTitle),
		chatID,
		msgID,
		time.Now().Format("2006-01-02 15:04:05 MST"),
		msgContent,
	)

	msgConfig := tgbotapi.NewMessage(modGroupID, text)
	msgConfig.ParseMode = tgbotapi.ModeMarkdown

	sentMsg, err := b.Send(msgConfig)
	if err != nil {
		// Fallback without markdown formatting if markdown fails
		msgConfig.ParseMode = ""
		sentMsg, err = b.Send(msgConfig)
		if err != nil {
			log.Printf("[Bot Error] Failed to send trigger ban alert to mod group: %v", err)
			return fmt.Errorf("failed to send trigger ban alert: %w", err)
		}
	}

	// Also log into flagged_posts table as a resolved trigger ban for audit trail in DB
	if msg != nil {
		flag, err := b.db.CreateResolvedFlaggedPost(chatID, msg.MessageID, user.UserID, reason, "banned", 0)
		if err == nil && flag != nil && sentMsg.MessageID != 0 {
			_ = b.db.UpdateFlagModMessageID(flag.ID, sentMsg.MessageID)
		}
	}

	return nil
}

// SendFirstEmptyMessageInfo sends a silent informational message to the moderation channel
// when a brand new user's first recorded message is empty.
func (b *Bot) SendFirstEmptyMessageInfo(chatID int64, msg *db.Message, user *db.User, groupTitle string) error {
	modGroupID := b.cfg.ModerationGroupID
	if modGroupID == 0 {
		return nil
	}

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
			"👤 **User**: %s (@%s)\n"+
			"🆔 **User ID**: `%d`\n"+
			"⭐ **Reputation**: `%d` | ⚠️ **Warns**: `%d`\n\n"+
			"👥 **Group**: %s (`%d`)\n"+
			"🆔 **Message ID**: `%d`\n"+
			"🕒 **Time**: `%s`\n\n"+
			"ℹ️ *User sent an initial empty message. No moderation penalty applied.*",
		escapeMarkdown(userDisplayName),
		escapeMarkdown(usernameStr),
		user.UserID,
		user.Reputation,
		user.WarnCount,
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
func (b *Bot) ExecuteActions(chatID int64, user *db.User, msg *db.Message, actions []detector.Action) {
	if msg == nil {
		return
	}
	messageID := msg.MessageID
	for _, act := range actions {
		switch act.Type {
		case detector.ActionDeleteMessage:
			log.Printf("[Bot Action] Deleting message %d in chat %d (reason: %s)", messageID, chatID, act.Reason)
			if err := b.DeleteGroupMessage(chatID, messageID); err != nil {
				log.Printf("[Bot Action Error] Failed to delete message %d in chat %d: %v", messageID, chatID, err)
			}

		case detector.ActionBanUser:
			log.Printf("[Bot Action] Banning user %d across groups (reason: %s)", user.UserID, act.Reason)
			if err := b.BanUserAcrossAllGroups(user.UserID, chatID); err != nil {
				log.Printf("[Bot Action Error] Failed to ban user %d across groups: %v", user.UserID, err)
			}
			if err := b.SendTriggerBanAlert(chatID, user, msg, act.Reason); err != nil {
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
				groupTitle := fmt.Sprintf("Chat %d", chatID)
				if group, err := b.db.GetGroup(chatID); err == nil && group != nil && group.Title != "" {
					groupTitle = group.Title
				}
				_ = b.SendModAlert(flag, msg, user, groupTitle)
			}
		}
	}
}

// RescanOptions configures parameters for manual or scheduled rescanning of low reputation users.
type RescanOptions struct {
	MaxReputation int           `json:"max_reputation"`
	Hours         int           `json:"hours"`
	Force         bool          `json:"force"`
	Delay         time.Duration `json:"delay"`
	DryRun        bool          `json:"dry_run"`
}

// RescanResult contains summary metrics of a rescan run.
type RescanResult struct {
	TotalCandidates int `json:"total_candidates"`
	ScannedCount    int `json:"scanned_count"`
	BannedCount     int `json:"banned_count"`
	CleanCount      int `json:"clean_count"`
	ErrorCount      int `json:"error_count"`
}

// RescanProgressFunc is called as each user is evaluated during a rescan.
type RescanProgressFunc func(curr, total int, user *db.User, profile *db.UserProfile, triggeredRule string, reason string, err error)

// RescanLowRepUsers rescans low-reputation users whose names or profiles were fetched more than N hours ago (or never fetched).
// It re-evaluates all join rules (e.g. red_packet_cjk_name, new_user_spam_bio) and triggers bans if matched.
func (b *Bot) RescanLowRepUsers(ctx context.Context, opts RescanOptions, progressCb RescanProgressFunc) (*RescanResult, error) {
	maxRep := opts.MaxReputation
	if maxRep <= 0 {
		maxRep = 20
	}
	hours := opts.Hours
	if hours <= 0 && !opts.Force {
		hours = 24
	}
	var cutoff time.Time
	if !opts.Force && hours > 0 {
		cutoff = time.Now().Add(-time.Duration(hours) * time.Hour)
	}
	delay := opts.Delay
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}

	users, err := b.db.GetLowRepUsersForRescan(maxRep, cutoff, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to query users for rescan: %w", err)
	}

	res := &RescanResult{
		TotalCandidates: len(users),
	}

	for i, u := range users {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		userCopy := u

		// 1. Fetch fresh profile from Telegram API (updates user_profiles table)
		profile, errFetch := b.FetchUserProfile(userCopy.UserID)
		if errFetch != nil && profile == nil {
			res.ErrorCount++
			if progressCb != nil {
				progressCb(i+1, len(users), &userCopy, nil, "", "", errFetch)
			}
			continue
		}

		res.ScannedCount++

		// Sync latest names from Telegram profile to users table
		if profile != nil {
			userCopy.Username = profile.Username
			userCopy.FirstName = profile.FirstName
			userCopy.LastName = profile.LastName
			_ = b.db.UpdateUserName(userCopy.UserID, userCopy.Username, userCopy.FirstName, userCopy.LastName)
		}

		// 2. Evaluate Join Rules / Triggers
		triggeredRule := ""
		reason := ""

		// Rule A: Red Packet CJK Name Trigger
		redPacketEnabled := b.cfg.Detector.RedPacketName.Enabled || b.cfg.Detector.NewUserRedPacket.Enabled
		if b.cfg.Detector.Enabled && redPacketEnabled {
			rpCfg := b.cfg.Detector.RedPacketName
			if !rpCfg.Enabled && b.cfg.Detector.NewUserRedPacket.Enabled {
				rpCfg = b.cfg.Detector.NewUserRedPacket
			}
			rTrigger := detector.NewRedPacketNameTrigger(rpCfg)
			msgCount, _ := b.db.GetUserMessageCount(userCopy.UserID)
			tCtx := &detector.TriggerContext{
				User:             &userCopy,
				IsNewUser:        true,
				UserMessageCount: msgCount,
			}
			rpRes, rpErr := rTrigger.Evaluate(tCtx)
			if rpErr == nil && rpRes != nil && rpRes.Triggered {
				triggeredRule = "red_packet_cjk_name"
				reason = rpRes.Reason
			}
		}

		// Rule B: Spam Profile Bio Trigger
		spamBioEnabled := b.cfg.Detector.NewUserSpamBio.Enabled || b.cfg.Detector.Enabled || b.cfg.AutoFlag.Enabled
		if triggeredRule == "" && spamBioEnabled && profile != nil && userCopy.Reputation <= maxRep {
			var customKeywords []string
			customKeywords = append(customKeywords, b.cfg.AutoFlag.BlockedKeywords...)
			customKeywords = append(customKeywords, b.cfg.Detector.NewUserSpamBio.CustomKeywords...)
			dbSnippets, _ := b.db.GetSpamSnippetStrings()
			customKeywords = append(customKeywords, dbSnippets...)

			isSpam, matchedKeywords := db.MatchSpamBioProfile(profile, customKeywords...)
			if isSpam || len(matchedKeywords) > 0 {
				matchedStr := strings.Join(matchedKeywords, ", ")
				if matchedStr == "" {
					matchedStr = "spam keyword match"
				}
				triggeredRule = "new_user_spam_bio"
				reason = fmt.Sprintf("Detection trigger (new_user_spam_bio): Rescanned profile signals matched spam keywords [%s]", matchedStr)
			}
		}

		// 3. Take Action if Triggered
		if triggeredRule != "" {
			res.BannedCount++
			log.Printf("[Rescan Trigger] Rule '%s' fired for user %d (@%s): %s", triggeredRule, userCopy.UserID, userCopy.Username, reason)

			if !opts.DryRun {
				// Ban user across all monitored groups
				if err := b.BanUserAcrossAllGroups(userCopy.UserID); err != nil {
					log.Printf("[Rescan Action Error] Failed to ban user %d across groups: %v", userCopy.UserID, err)
				}

				// Deduct reputation
				repPenalty := 20
				if newRep, err := b.db.AdjustReputation(userCopy.UserID, -repPenalty, reason, 0); err == nil {
					userCopy.Reputation = newRep
				}

				// Send trigger ban alert to moderation group
				snippetText := fmt.Sprintf("[Rescan Trigger]: %s\n[Name]: %s %s\n[Username]: @%s", triggeredRule, userCopy.FirstName, userCopy.LastName, userCopy.Username)
				if profile != nil {
					if profile.Bio != "" {
						snippetText += fmt.Sprintf("\n[Bio]: %s", profile.Bio)
					}
					if profile.PersonalChatTitle != "" || profile.PersonalChatUsername != "" {
						snippetText += fmt.Sprintf("\n[Personal Channel]: %s (@%s)", profile.PersonalChatTitle, profile.PersonalChatUsername)
					}
					if profile.BusinessIntro != "" {
						snippetText += fmt.Sprintf("\n[Business Intro]: %s", profile.BusinessIntro)
					}
				}
				alertMsg := &db.Message{
					ChatID:    0,
					MessageID: 0,
					UserID:    userCopy.UserID,
					Text:      snippetText,
					CreatedAt: time.Now(),
				}
				if err := b.SendTriggerBanAlert(0, &userCopy, alertMsg, reason); err != nil {
					log.Printf("[Rescan Action Error] Failed to send trigger ban alert: %v", err)
				}
			}
		} else {
			res.CleanCount++
			log.Printf("[Rescan Clean] User %d (@%s) evaluated clean.", userCopy.UserID, userCopy.Username)
		}

		if progressCb != nil {
			progressCb(i+1, len(users), &userCopy, profile, triggeredRule, reason, nil)
		}

		if i < len(users)-1 {
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return res, nil
}

// SendPrivateMessageMirror mirrors any private message sent directly to the bot to the moderation channel (b.cfg.ModerationGroupID),
// unless the sender is a known bot admin.
func (b *Bot) SendPrivateMessageMirror(msg *tgbotapi.Message, dbMsg *db.Message, user *db.User) error {
	if user == nil {
		return fmt.Errorf("user cannot be nil")
	}

	if b.IsBotAdminUser(user) {
		log.Printf("[Bot] User %d (@%s) is a known bot admin. Skipping mirror to moderation channel.", user.UserID, user.Username)
		return nil
	}

	modGroupID := b.cfg.ModerationGroupID
	if modGroupID == 0 {
		log.Printf("[Bot] Warning: ModerationGroupID not set. Cannot mirror private message from user %d", user.UserID)
		return nil
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

	msgContent := ""
	if dbMsg != nil && dbMsg.Text != "" {
		msgContent = dbMsg.Text
	} else if msg != nil {
		msgContent = extractMessageText(msg)
	}
	if strings.TrimSpace(msgContent) == "" {
		if (msg != nil && (msg.Photo != nil || msg.Video != nil || msg.Document != nil || msg.Audio != nil || msg.Animation != nil || msg.Sticker != nil || msg.Voice != nil || msg.VideoNote != nil)) || (dbMsg != nil && dbMsg.HasMedia) {
			msgContent = "[Media message]"
		} else {
			msgContent = "(empty message)"
		}
	}

	headerTitle := "📩 **PRIVATE MESSAGE RECEIVED**"
	if msg != nil && msg.EditDate != 0 {
		headerTitle = "📩 **PRIVATE MESSAGE RECEIVED (EDITED)**"
	}

	var extraInfo strings.Builder
	if msg != nil {
		if msg.ForwardFrom != nil {
			fwdName := strings.TrimSpace(msg.ForwardFrom.FirstName + " " + msg.ForwardFrom.LastName)
			fwdUser := msg.ForwardFrom.UserName
			if fwdUser != "" {
				fwdUser = "@" + fwdUser
			} else {
				fwdUser = fmt.Sprintf("ID: %d", msg.ForwardFrom.ID)
			}
			extraInfo.WriteString(fmt.Sprintf("\n↪️ **Forwarded From**: %s (%s)", escapeMarkdown(fwdName), escapeMarkdown(fwdUser)))
		} else if msg.ForwardFromChat != nil {
			extraInfo.WriteString(fmt.Sprintf("\n↪️ **Forwarded From Channel/Chat**: %s (`%d`)", escapeMarkdown(msg.ForwardFromChat.Title), msg.ForwardFromChat.ID))
		} else if msg.ForwardSenderName != "" {
			extraInfo.WriteString(fmt.Sprintf("\n↪️ **Forwarded From**: %s (Hidden Account)", escapeMarkdown(msg.ForwardSenderName)))
		}
	}

	msgTime := time.Now()
	msgID := 0
	if msg != nil {
		msgTime = msg.Time()
		msgID = msg.MessageID
	} else if dbMsg != nil {
		msgTime = dbMsg.CreatedAt
		msgID = dbMsg.MessageID
	}

	text := fmt.Sprintf(
		"%s\n\n"+
			"👤 **Sender**: %s (@%s)\n"+
			"🆔 **User ID**: `%d`\n"+
			"⭐ **Reputation**: `%d` | ⚠️ **Warns**: `%d` | 💬 **Logged Posts**: `%d`%s\n"+
			"🆔 **Message ID**: `%d`\n"+
			"🕒 **Time**: `%s`\n\n"+
			"📝 **Content**:\n```\n%s\n```",
		headerTitle,
		escapeMarkdown(userDisplayName),
		escapeMarkdown(usernameStr),
		user.UserID,
		user.Reputation,
		user.WarnCount,
		totalUserMsgs,
		extraInfo.String(),
		msgID,
		msgTime.Format("2006-01-02 15:04:05 MST"),
		truncateText(msgContent, 2000),
	)

	msgConfig := tgbotapi.NewMessage(modGroupID, text)
	msgConfig.ParseMode = tgbotapi.ModeMarkdown

	sentMsg, err := b.Send(msgConfig)
	if err != nil {
		// Fallback without markdown formatting
		msgConfig.ParseMode = ""
		sentMsg, err = b.Send(msgConfig)
		if err != nil {
			log.Printf("[Bot Error] Failed to send private message mirror alert to mod group: %v", err)
		}
	}

	// Attempt to forward the original message to mod group so media/attachments are visible
	if msg != nil && msg.Chat != nil && msg.MessageID != 0 {
		forward := tgbotapi.NewForward(modGroupID, msg.Chat.ID, msg.MessageID)
		if _, fwdErr := b.Send(forward); fwdErr != nil {
			log.Printf("[Bot] Notice: Could not forward original private message %d to mod group: %v", msg.MessageID, fwdErr)
		}
	}

	_ = sentMsg
	return err
}

