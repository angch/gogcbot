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

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(update.CallbackQuery)
		return
	}

	if update.Message != nil {
		b.handleMessage(update.Message)
		return
	}

	if update.EditedMessage != nil {
		b.handleMessage(update.EditedMessage)
		return
	}

	if update.ChannelPost != nil {
		b.handleMessage(update.ChannelPost)
		return
	}

	if update.EditedChannelPost != nil {
		b.handleMessage(update.EditedChannelPost)
		return
	}

	if update.ChatMember != nil {
		b.handleChatMemberUpdate(update.ChatMember)
		return
	}
}

func (b *Bot) handleChatMemberUpdate(cmu *tgbotapi.ChatMemberUpdated) {
	if cmu == nil {
		return
	}
	userID := cmu.NewChatMember.User.ID
	chatID := cmu.Chat.ID

	// Check if this user is marked as banned in our DB
	user, err := b.db.GetUserByID(userID)
	if err == nil && user != nil && user.IsBanned {
		// Detect if ban was modified or converted into a timed ban by Shieldy or Telegram:
		// A permanent ban in Telegram has status "kicked" AND UntilDate == 0.
		newCM := cmu.NewChatMember
		banChanged := newCM.Status != "kicked" || newCM.UntilDate != 0
		if banChanged {
			log.Printf("[ChatMemberUpdate Alert] User %d in chat %d ban status was changed by Shieldy/Telegram (Old: %s, New: %s, UntilDate: %d). Scheduling re-ban...",
				userID, chatID, cmu.OldChatMember.Status, newCM.Status, newCM.UntilDate)

			delay := time.Duration(b.cfg.Shieldy.RecheckDelayMinutes) * time.Minute
			if delay <= 0 {
				delay = 6 * time.Minute
			}
			b.ScheduleBanRecheck(chatID, userID, delay)
		}
		return
	}

	// Detect if user joined the group / channel
	isJoin := (cmu.NewChatMember.Status == "member" || cmu.NewChatMember.Status == "restricted") &&
		(cmu.OldChatMember.Status != "member" && cmu.OldChatMember.Status != "administrator" && cmu.OldChatMember.Status != "creator")
	if isJoin {
		b.handleUserJoined(cmu.Chat.ID, cmu.Chat.Title, cmu.NewChatMember.User, nil)
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
	if chat.IsGroup() || chat.IsSuperGroup() || chat.IsChannel() {
		_ = b.db.SaveGroup(chat.ID, chat.Title, chat.Type)
	}

	// Get or create user profile
	user, isNewUser, err := b.db.GetOrCreateUser(
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

	// Ensure SuperAdmin, Group Admins, and Bot Admins have initial 100 reputation score
	isSuperAdmin := b.cfg.SuperAdminID != 0 && user.UserID == b.cfg.SuperAdminID
	isGroupAdmin := (chat.IsGroup() || chat.IsSuperGroup()) && b.IsUserAdminInChat(chat.ID, user.UserID)
	isBotAdmin := user.IsAdmin
	if (isSuperAdmin || isGroupAdmin || isBotAdmin) && user.Reputation < 100 {
		if err := b.db.SetReputation(user.UserID, 100, "Admin/SuperAdmin initial reputation setup", user.UserID); err == nil {
			user.Reputation = 100
		}
	}

	// Users with maximum reputation (>= 100) are whitelisted whether banned or not
	if user.Reputation >= 100 {
		if user.IsBanned {
			log.Printf("[Bot Whitelist] User %d (@%s) has maximum reputation (%d). Clearing DB ban flag.", user.UserID, user.Username, user.Reputation)
			_ = b.db.SetUserBanned(user.UserID, false)
			user.IsBanned = false
		}
	} else if user.IsBanned && (chat.IsGroup() || chat.IsSuperGroup()) {
		// Self-healing check: verify if Telegram user status is active (unbanned by an admin in Telegram UI)
		cm, err := b.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: chat.ID,
				UserID: user.UserID,
			},
		})
		if err == nil && (cm.Status == "member" || cm.Status == "administrator" || cm.Status == "creator" || cm.Status == "left") {
			log.Printf("[Bot] User %d (@%s) was unbanned in Telegram UI (status: %s). Self-healing: clearing DB ban flag.", user.UserID, user.Username, cm.Status)
			_ = b.db.SetUserBanned(user.UserID, false)
			user.IsBanned = false
			if user.Reputation <= b.cfg.Reputation.FlagThreshold {
				targetRep := b.cfg.Reputation.FlagThreshold + 10
				if targetRep < 50 {
					targetRep = 50
				}
				_ = b.db.SetReputation(user.UserID, targetRep, "Unbanned in Telegram UI & reputation restored", user.UserID)
				user.Reputation = targetRep
			}
		} else {
			_ = b.DeleteGroupMessage(chat.ID, msg.MessageID)
			_ = b.BanUserInGroup(chat.ID, user.UserID)
			return
		}
	}

	// Check for joining members in service messages
	if len(msg.NewChatMembers) > 0 {
		for _, newMember := range msg.NewChatMembers {
			nm := newMember
			b.handleUserJoined(chat.ID, chat.Title, &nm, msg)
		}
	}

	// Ignore service messages (user joined, left, pinned message, etc.) from being saved as user chat messages
	if isServiceMessage(msg) {
		return
	}

	// Detect links & media
	hasMedia := msg.Photo != nil || msg.Video != nil || msg.Document != nil || msg.Audio != nil || msg.Animation != nil || msg.Sticker != nil
	hasLinks := containsLinks(msg)

	text := extractMessageText(msg)

	groupName := chat.Title
	if groupName == "" {
		if chat.IsPrivate() {
			groupName = "Private Chat"
		} else {
			groupName = fmt.Sprintf("Chat %d", chat.ID)
		}
	}

	log.Printf("[Received Message] Sender ID: %d (@%s) | Group: '%s' (ID: %d) | Content: %q",
		msg.From.ID, msg.From.UserName, groupName, chat.ID, text)

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

	// Moderation check only for monitored groups/channels (not private chats or moderation group)
	// Skip moderation for whitelisted users with maximum reputation (>= 100)
	if (chat.IsGroup() || chat.IsSuperGroup() || chat.IsChannel()) && chat.ID != b.cfg.ModerationGroupID && user.Reputation < 100 {
		userMsgCount, _ := b.db.GetUserMessageCount(user.UserID)

		hasVerified := false
		if b.cfg.Shieldy.Enabled {
			alreadyVerified, _ := b.db.HasReceivedRepBonus(user.UserID, "Shieldy verification")
			if alreadyVerified {
				hasVerified = true
			} else if detector.IsShieldyVerificationText(text) && userMsgCount <= b.cfg.Shieldy.MaxMessages {
				repBonus := b.cfg.Shieldy.RepBonus
				if repBonus <= 0 {
					repBonus = 5
				}
				newRep, err := b.db.AdjustReputation(user.UserID, repBonus, "Shieldy verification: I am not a bot", user.UserID)
				if err != nil {
					log.Printf("[Bot Rep] Error adjusting reputation for Shieldy verification for user %d: %v", user.UserID, err)
				} else {
					log.Printf("[Shieldy Verification] User %d (@%s) verified with 'I am not a bot'. Added +%d reputation (New Rep: %d)",
						user.UserID, user.Username, repBonus, newRep)
					user.Reputation = newRep
					hasVerified = true
				}
			}
		}

		userBio := ""
		if prof, err := b.db.GetUserProfile(user.UserID); err == nil && prof != nil {
			userBio = prof.Bio
		} else if isNewUser || userMsgCount <= 1 {
			if prof, err := b.FetchUserProfile(user.UserID); err == nil && prof != nil {
				userBio = prof.Bio
			}
		}

		tCtx := &detector.TriggerContext{
			Message:           dbMsg,
			RawMessage:        msg,
			Text:              text,
			User:              user,
			UserBio:           userBio,
			IsNewUser:         isNewUser,
			UserMessageCount:  userMsgCount,
			ChatID:            chat.ID,
			GroupTitle:        chat.Title,
			HasVerifiedNotBot: hasVerified,
		}

		if b.detector != nil {
			results, err := b.detector.Evaluate(tCtx)
			if err != nil {
				log.Printf("[Bot] Error evaluating detection triggers: %v", err)
			} else if len(results) > 0 {
				actionTaken := false
				for _, res := range results {
					log.Printf("[Bot Trigger] Rule '%s' fired for user %d in chat %d: %s", res.TriggerID, user.UserID, chat.ID, res.Reason)
					b.ExecuteActions(chat.ID, user, dbMsg, res.Actions)
					for _, act := range res.Actions {
						if act.Type == detector.ActionDeleteMessage || act.Type == detector.ActionBanUser {
							actionTaken = true
						}
					}
				}
				if actionTaken {
					return
				}
			}
		}

		// First message seen from user in monitored channel/group and message is empty:
		// Don't flag, just send a silent info message to the moderation channel.
		if (isNewUser || userMsgCount <= 1) && strings.TrimSpace(text) == "" {
			log.Printf("[Bot] First message from user %d in chat %d is empty. Sending silent info message to moderation channel.", user.UserID, chat.ID)
			if err := b.SendFirstEmptyMessageInfo(chat.ID, dbMsg, user, chat.Title); err != nil {
				log.Printf("[Bot] Error sending first empty message info: %v", err)
			}
			return
		}

		flagged := b.checkAutoFlagRules(msg, dbMsg, user, chat.Title)
		if !flagged && strings.TrimSpace(text) != "" {
			b.bumpUnflaggedReputation(user)
		}
	}
}

func (b *Bot) bumpUnflaggedReputation(user *db.User) {
	if user.Reputation >= 100 {
		return
	}

	alreadyBumped, err := b.db.HasReceivedDailyRepBump(user.UserID, "Daily unflagged message")
	if err != nil {
		log.Printf("[Bot Rep] Error checking daily rep bump for user %d: %v", user.UserID, err)
		return
	}

	if !alreadyBumped {
		newRep, err := b.db.AdjustReputationWithCap(user.UserID, 1, 100, "Daily unflagged message activity", user.UserID)
		if err != nil {
			log.Printf("[Bot Rep] Error bumping reputation for user %d: %v", user.UserID, err)
		} else {
			log.Printf("[Bot Rep] User %d reputation bumped +1 for unflagged message activity (New Rep: %d)", user.UserID, newRep)
			user.Reputation = newRep
		}
	}
}

func (b *Bot) checkAutoFlagRules(msg *tgbotapi.Message, dbMsg *db.Message, user *db.User, groupTitle string) bool {
	if !b.cfg.AutoFlag.Enabled {
		return false
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

	// Rule 3: Blocked Keywords & DB Spam Snippets
	lowerText := strings.ToLower(dbMsg.Text)
	matchedKw := false
	for _, kw := range b.cfg.AutoFlag.BlockedKeywords {
		if kw != "" && strings.Contains(lowerText, strings.ToLower(kw)) {
			reasons = append(reasons, fmt.Sprintf("Contains keyword: '%s'", kw))
			matchedKw = true
			break
		}
	}
	if !matchedKw {
		dbSnippets, _ := b.db.GetSpamSnippetStrings()
		for _, snip := range dbSnippets {
			if snip != "" && strings.Contains(lowerText, strings.ToLower(snip)) {
				reasons = append(reasons, fmt.Sprintf("Contains spam snippet: '%s'", snip))
				break
			}
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
			return true
		}

		if err := b.SendModAlert(flag, dbMsg, user, groupTitle); err != nil {
			log.Printf("[AutoFlag] Error sending mod alert: %v", err)
		}
		return true
	}
	return false
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

func isServiceMessage(msg *tgbotapi.Message) bool {
	if msg == nil {
		return false
	}
	return len(msg.NewChatMembers) > 0 ||
		msg.LeftChatMember != nil ||
		msg.PinnedMessage != nil ||
		msg.GroupChatCreated ||
		msg.SuperGroupChatCreated ||
		msg.ChannelChatCreated ||
		msg.MigrateToChatID != 0 ||
		msg.MigrateFromChatID != 0
}

func extractMessageText(msg *tgbotapi.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Text != "" {
		return msg.Text
	}
	if msg.Caption != "" {
		return msg.Caption
	}
	if msg.Photo != nil {
		return "[Photo]"
	}
	if msg.Sticker != nil {
		if msg.Sticker.Emoji != "" {
			return fmt.Sprintf("[Sticker %s]", msg.Sticker.Emoji)
		}
		return "[Sticker]"
	}
	if msg.Document != nil {
		if msg.Document.FileName != "" {
			return fmt.Sprintf("[Document: %s]", msg.Document.FileName)
		}
		return "[Document]"
	}
	if msg.Video != nil {
		return "[Video]"
	}
	if msg.Voice != nil {
		return "[Voice Message]"
	}
	if msg.Audio != nil {
		return "[Audio]"
	}
	if msg.Animation != nil {
		return "[Animation/GIF]"
	}
	if msg.Poll != nil {
		return fmt.Sprintf("[Poll: %s]", msg.Poll.Question)
	}
	if msg.Contact != nil {
		name := strings.TrimSpace(msg.Contact.FirstName + " " + msg.Contact.LastName)
		if name != "" {
			return fmt.Sprintf("[Contact: %s (%s)]", name, msg.Contact.PhoneNumber)
		}
		return fmt.Sprintf("[Contact: %s]", msg.Contact.PhoneNumber)
	}
	if msg.Location != nil {
		return "[Location]"
	}
	if msg.Venue != nil {
		if msg.Venue.Title != "" {
			return fmt.Sprintf("[Venue: %s (%s)]", msg.Venue.Title, msg.Venue.Address)
		}
		return "[Venue]"
	}
	return ""
}

// handleUserJoined handles a user joining a group, supergroup, or channel.
// It retrieves their profile and bio, checks against spam bio filters, and bans/kicks them if matched.
func (b *Bot) handleUserJoined(chatID int64, groupTitle string, tgUser *tgbotapi.User, joinMsg *tgbotapi.Message) {
	if tgUser == nil || tgUser.IsBot || tgUser.ID == 0 || (b.botUser.ID != 0 && tgUser.ID == b.botUser.ID) {
		return
	}

	// Don't run moderation in private moderation channel
	if b.cfg.ModerationGroupID != 0 && chatID == b.cfg.ModerationGroupID {
		return
	}

	if groupTitle == "" {
		groupTitle = fmt.Sprintf("Chat %d", chatID)
		if group, err := b.db.GetGroup(chatID); err == nil && group != nil && group.Title != "" {
			groupTitle = group.Title
		}
	}

	log.Printf("[User Joined] User %d (@%s - %s %s) joined chat %d (%s)",
		tgUser.ID, tgUser.UserName, tgUser.FirstName, tgUser.LastName, chatID, groupTitle)

	// Get or create user record in DB
	user, _, err := b.db.GetOrCreateUser(
		tgUser.ID,
		tgUser.UserName,
		tgUser.FirstName,
		tgUser.LastName,
		b.cfg.Reputation.DefaultInitial,
	)
	if err != nil {
		log.Printf("[Bot] Error getting/creating user %d on join: %v", tgUser.ID, err)
		return
	}

	// Whitelist: users with reputation >= 100 or SuperAdmin are exempt
	if user.Reputation >= 100 || (b.cfg.SuperAdminID != 0 && user.UserID == b.cfg.SuperAdminID) || user.IsAdmin {
		return
	}

	// If user is already marked banned in DB, enforce ban in this chat
	if user.IsBanned {
		log.Printf("[Bot] User %d joining chat %d is already banned in DB. Enforcing ban...", user.UserID, chatID)
		if joinMsg != nil {
			_ = b.DeleteGroupMessage(chatID, joinMsg.MessageID)
		}
		_ = b.BanUserInGroup(chatID, user.UserID)
		return
	}

	// 1. Grab user profile and bio from Telegram API
	profile, err := b.FetchUserProfile(user.UserID)
	if err != nil {
		log.Printf("[User Joined] Could not fetch profile for user %d (@%s): %v", user.UserID, user.Username, err)
	}

	bio := ""
	if profile != nil {
		bio = profile.Bio
	} else if cachedProf, err := b.db.GetUserProfile(user.UserID); err == nil && cachedProf != nil {
		bio = cachedProf.Bio
	}

	if strings.TrimSpace(bio) == "" {
		// User has no bio, nothing to spam-match
		return
	}

	// 2. Check if spam bio detection is enabled
	spamBioEnabled := b.cfg.Detector.NewUserSpamBio.Enabled || b.cfg.Detector.Enabled || b.cfg.AutoFlag.Enabled
	if !spamBioEnabled {
		return
	}

	// Reputation threshold check (only kick users with reputation <= MaxReputation, default 5)
	maxRep := b.cfg.Detector.NewUserSpamBio.MaxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if user.Reputation > maxRep {
		return
	}

	// 3. Match bio against spam keywords
	var customKeywords []string
	customKeywords = append(customKeywords, b.cfg.AutoFlag.BlockedKeywords...)
	customKeywords = append(customKeywords, b.cfg.Detector.NewUserSpamBio.CustomKeywords...)
	dbSnippets, _ := b.db.GetSpamSnippetStrings()
	customKeywords = append(customKeywords, dbSnippets...)

	isSpam, matchedKeywords := db.MatchSpamBioAll(bio, customKeywords...)
	if !isSpam && len(matchedKeywords) == 0 {
		return
	}

	// 4. User matched spam bio filter! Kick/ban them like CJK users.
	matchedStr := strings.Join(matchedKeywords, ", ")
	if matchedStr == "" {
		matchedStr = "spam keyword match"
	}
	reason := fmt.Sprintf("Detection trigger (new_user_spam_bio): Joining user profile bio matched spam keywords [%s]", matchedStr)
	log.Printf("[Bot Trigger] Rule 'new_user_spam_bio' fired for user %d in chat %d: %s (Bio: %q)", user.UserID, chatID, reason, bio)

	// Delete join service message if present
	if joinMsg != nil {
		_ = b.DeleteGroupMessage(chatID, joinMsg.MessageID)
	}

	// Ban user across all monitored groups
	if err := b.BanUserAcrossAllGroups(user.UserID, chatID); err != nil {
		log.Printf("[Bot Action Error] Failed to ban user %d across groups: %v", user.UserID, err)
	}

	// Deduct reputation
	repPenalty := b.cfg.Detector.NewUserSpamBio.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}
	if newRep, err := b.db.AdjustReputation(user.UserID, -repPenalty, reason, 0); err == nil {
		user.Reputation = newRep
	}

	// Construct dummy message for alert snippet showing user's bio
	msgID := 0
	if joinMsg != nil {
		msgID = joinMsg.MessageID
	}
	alertMsg := &db.Message{
		ChatID:    chatID,
		MessageID: msgID,
		UserID:    user.UserID,
		Text:      fmt.Sprintf("[Bio]: %s", bio),
		CreatedAt: time.Now(),
	}

	// Send trigger ban alert to moderation group
	if err := b.SendTriggerBanAlert(chatID, user, alertMsg, reason); err != nil {
		log.Printf("[Bot Action Error] Failed to send trigger ban alert for spam bio: %v", err)
	}
}
