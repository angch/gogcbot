# GoGCBot - Telegram Group Moderation & Reputation Bot

`gogcbot` is a Telegram group moderation bot written in pure Go (with zero CGO dependencies) using Cobra CLI, Viper configuration, `modernc.org/sqlite` database engine, and Telegram Bot API.

It manages Telegram groups by tracking user reputation scores, keeping a 7-day conversation history log, storing up to 50 posts per user across all monitored groups, and forwarding suspicious or flagged posts to a **Private Moderation Group** equipped with interactive inline action buttons for human moderators.

---

## 🌟 Key Features

1. **Pure Go & CGO-Free SQLite**:
   - Compiles cleanly (`CGO_ENABLED=0`) across Windows, Linux, and macOS without requiring any C compilers.
   - Built on `modernc.org/sqlite` for high-performance embedded storage with WAL mode enabled.

2. **Retention Policy Management**:
   - **7-Day Message Retention**: Automatically purges log messages older than 7 days.
   - **50 Posts Per User Cap**: Keeps a max of 50 recent posts per Telegram user across all monitored groups.
   - Automated background ticker + manual `/cleanup` CLI / Telegram command.

3. **User Reputation System**:
   - New unseen users start with **0 points** by default.
   - Promoted Super Admins and Group Administrators are initialized with **100 points**.
   - Reputation adjusts dynamically based on moderator actions or good behavior:
     - ✅ **Approve Post**: +5 points
     - 🗑️ **Delete Post**: -10 points
     - ⚠️ **Warn User**: -20 points + increments warning count
     - 🔇 **Mute 24h**: -30 points + 24-hour group mute
     - 🚫 **Ban User**: -50 points + ban across all monitored groups
     - ➕ / ➖ **Manual Rep Adjustments**: +/- 10 points or custom deltas

3. **Shieldy Bot Verification & Ban Protection**:
   - Seamlessly integrates with Shieldy captcha bot: when a new user's initial messages match `"I am not a bot"`, they receive **+5 reputation** points.
   - Automatically tracks and remembers Shieldy captcha challenge messages (`"please, press the button below within the time amount specified, otherwise you will be kicked."`).
   - If a user is kick-banned due to their profile (e.g. spam bio, blacklist keyword, anomalous username, or existing ban), the bot automatically retrieves and deletes the corresponding Shieldy challenge message from the group to keep the chat clean.
   - Automatically exempts verified users and `"I am not a bot"` messages from triggering the High-ID CJK spam detection trigger.
   - Automatically schedules a **6-minute delayed re-check** upon issuing bans to detect if Shieldy or Telegram altered a permanent ban into a temporary timed ban, re-issuing permanent bans if necessary.

4. **Auto-Flagging & Moderation Engine**:
   - Automatically detects suspicious messages:
     - Low reputation user (< 50) posting links (aggressively removes telegram message from the group when `delete_on_links: true`, forwarding to moderation group).
     - New account (<= 3 posts) posting links (aggressively removes telegram message from the group when `delete_on_links: true`, forwarding to moderation group).
     - Blocked spam/crypto keywords or custom regexes.
     - Reputation falling below threshold.
   - Dispatches automated ban notifications (`🚫 TRIGGER BAN EXECUTED`) directly to the Private Moderation Group / management monitoring channel whenever a ban is triggered by detection rules.
   - Dispatches silent notifications (`ℹ️ FIRST MESSAGE SEEN (EMPTY)`) to the Private Moderation Group without flagging or penalizing when a new user's first message is empty.
   - Manual user/moderator reporting via `/flag [reason]` command.

5. **Private Moderation Group Workflow**:
   - Formats rich **Flagged Post Review Cards** sent directly to the Private Moderation Group.
   - Includes interactive **Inline Keyboard Action Buttons** allowing human moderators to immediately take action without leaving Telegram.
   - Real-time resolution logging on the moderation card displaying who resolved the report and the outcome.

6. **Admin-Only Response Policy & Access Control**:
   - The bot ONLY responds to Super Admins, Moderation Group members, and Telegram Group Administrators.
   - All non-admin messages and commands (including `/help`, `/start`, `/status`) are ignored silently without replying, while remaining logged to SQLite database history and system logs.

7. **Super Admin Controls & Config Persistence**:
   - Configurable owner/super admin user ID.
   - Dynamic `/setsuperadmin` and `/setmodgroup` commands automatically persist settings back to `config.yaml`.
   - Full control over bot settings, group monitoring management (`/addgroup`, `/removegroup`, `/listgroups`), and reputation overrides.

8. **Channel & Group Join Moderation**:
   - **Allowed Updates & Real-time Join Ingestion**: Listens for Telegram `chat_member`, `my_chat_member`, and `chat_join_request` updates across broadcast channels, supergroups, and groups.
   - **Profile Bio & Channel Scanning on Join & Message**: Automatically retrieves and evaluates user profile bio, personal channel title/username, and business intro when a user joins a channel/group or sends a message, caching them in `user_profiles`.
   - **Automatic Spam Bio & Channel Kick Rule (`new_user_spam_bio`)**: If a joining or new user's bio, personal channel title, or business intro matches spam/syndicate keywords (such as `点我`, discount cards, etc.) or their personal channel username matches automated syndicate patterns (`letter...digits`), the bot immediately deletes join messages, bans them across all monitored channels and groups, penalizes reputation, and alerts the moderation channel.
   - **Comprehensive Join Logging**: Detailed structured logging for every join event (`[ChatMemberUpdate]`, `[User Joined]`, rule triggers, whitelist bypasses, and clean evaluations).
   - **`spam_snippets` Database Table**: Stores dynamic spam snippets synced from runtime `config.yaml` or added programmatically.
   - **CLI & Bot Commands**: Search and inspect suspicious bios or unknown/new accounts via `gogcbot list-unknownusers` (or `list-spambios`) and `/listunknownusers` (rendered as a compact monospace table with CJK visual alignment).

9. **Red Packet CJK Name & Mixed-Caps Username Detection (`red_packet_name`)**:
   - **Instant Ban on Join & Message**: Instantly triggers automated bans, deletes join/chat messages, penalizes reputation (-20), and alerts the moderation channel when a joining or new account matches:
     1. Name ends with `"🧧"` (Red Packet emoji `U+1F9E7`).
     2. Name itself is mostly CJK Unicode characters (>= 50% CJK ratio).
     3. High user ID (>= 1,000,000,000 / 10^9).
     4. Username consists of mixed-caps letters of at least 5 length (e.g. `@cbzbQFLOuHNkJZ`).

10. **New High-ID User CJK Spam Detection (`new_user_cjk`)**:
    - **Targeted CJK Language Spam Detection**: Detects CJK spam from brand new, high-ID users with low or zero reputation.
    - **Progressive Length-Based Thresholding**:
      - **Short messages ($\le$ 50 runes)**: Catches obfuscated spam (e.g. `六栢o壹天`, `Hello 你好`) with at least 2 CJK characters and a 10% ratio.
      - **Medium messages (51–150 runes)**: Requires at least 5 CJK characters and 15% CJK ratio.
      - **Long messages (> 150 runes)**: Forgiving for long technical posts containing incidental CJK terms, code snippets, or kaomojis (requires $\ge$ 10 CJK chars and 15% ratio, or $\ge$ 25 CJK chars).
    - **Punctuation & Whitespace Safety**: Exempts whitespace (including ideographic full-width spaces `\u3000`), symbols, and punctuation (`【】`, `〜`, etc.) to prevent false-positive bans on emoticons and brackets.

11. **Private Message Logging & Moderation Channel Mirroring**:
    - **Full Visibility & Audit Trail**: Any private direct message (PM/DM) sent to the bot is automatically saved to the SQLite conversation history and logged to the system logs.
    - **Automated Mirroring to Moderation Channel**: Private messages from non-admin users (including text, media, edited messages, and forwards) are immediately formatted and mirrored directly to the Private Moderation Group (`moderation_group_id`), complete with sender information, user ID, reputation score, warning count, message timestamp, and forwarded attachments. Private messages from known bot admins (Super Admin, Bot Admins, and Moderation Group members) are kept private and excluded from mirroring.

12. **Auto-Generated Username Anomaly Detection (`username_anomaly`)**:
    - **Algorithmic Handle Scoring**: Detects automated/farmed Telegram handles for new high-ID users using mixed-case patterns, interleaved digit sequences (e.g. `nykrr_v9cl2t`), and Shannon entropy gated with low vowel ratios (e.g. `qtdulcljcptk6950`).
    - **Moderator Review & Flagging**: Flags suspicious posts to the Private Moderation Group for human moderator review and action (`flag_only: true` by default). Configurable score thresholds (`min_score: 3`) and optional immediate kickban mode (`flag_only: false`).

13. **Profile Name Spam Keyword Ban (`profile_name_keyword_ban`)**:
    - **Homoglyph Normalization**: Folds visually confusable digits and letters (`o/O/零/〇 -> 0`, `l/L -> 1`) to catch evasive name variations (e.g. `六o0壹天` or `玖Oo壹天` hitting `0壹天`).
    - Evaluates display names against calibrated blocklists for new high-ID users with low reputation.

14. **Primary & Secondary Group Hierarchy & Cross-Group Join Rule (`secondary_group_not_in_primary`)**:
    - **Primary vs. Secondary Groups**: Categorizes monitored Telegram groups into Primary Groups (`is_primary = true`) and Secondary Groups (`is_primary = false`).
    - **Cross-Group Membership Gate on Join, Message & Rescan**: When a user joins any secondary group, sends a message in a secondary group, or is audited during user rescans (`/rescanusers` or `gogcbot rescan-users`), the bot checks if they are currently an active member in the primary group(s). If they are not a member of the primary group, the bot immediately:
      - Issues a permanent ban across **all** monitored channels and groups (`BanUserAcrossAllGroups`).
      - Deletes the join service message and any Shieldy captcha challenges.
      - Deducts reputation points (-20).
      - Dispatches a trigger ban notification to the Private Moderation Group.
    - **Exemptions**: Bot administrators, whitelisted users with maximum reputation (>= 100), and users with reputation exceeding the configured threshold (`max_reputation`, default 5, including rep 50+) are exempt.
    - **Commands**: Configure group roles on the fly via `/setprimarygroup [chat_id]` and `/setsecondarygroup [chat_id]`, with role badges (`[⭐ Primary]` / `[Secondary]`) displayed in `/listgroups`.

15. **Low Reputation User Custom Sticker Kickban (`custom_sticker`) & Sticker Moderation Signals**:
    - **Custom Sticker Exploitation Defense**: Spammers frequently craft custom stickers (or custom sticker packs) associated with standard emojis like thumbs up (`👍`) containing unwanted advertisements, adult content, or scams to circumvent text-based filters (e.g. Message ID `66607`).
    - **Normal Emoji vs. Custom Sticker Detection**:
      - Normal emojis sent from mobile/desktop keyboards arrive strictly as standard Unicode text messages (`msg.Text = "👍"`, `msg.Sticker == nil`).
      - Custom stickers arrive as dedicated Telegram sticker media objects (`msg.Sticker != nil`), containing pack names, dimensions, format, and file IDs.
    - **Automated Immediate Kickban**: Whenever a user with low reputation (`reputation <= max_reputation`, default 5) posts a custom sticker or custom emoji, the bot immediately:
      - Deletes the offending sticker message from the group chat (`ActionDeleteMessage`).
      - Kickbans the spammer across all monitored channels and supergroups (`ActionBanUser`).
      - Deducts reputation points (`-50`).
      - Dispatches a trigger ban notification with full sticker details to the Private Moderation Group (`SendTriggerBanAlert`).
    - **Rich Sticker Metadata in Moderation Cards & Database**: When any message containing a sticker is flagged for review or auto-banned, the moderation alert includes rich sticker signals:
      - **Pack Name**: originating sticker set (e.g. `crypto_rocket_pack` or `(none / custom in-app editor)`).
      - **Dimensions**: exact `Width x Height` resolution (e.g. `512x512` or `512x256`) and aspect ratio.
      - **Format**: Static (WebP), Animated (TGS/Lottie), or Video (WebM).
      - **Emoji & File IDs**: Associated emoji (`👍`), `FileID`, `FileUniqueID`, and file size in bytes.
      - **Persistence**: Persisted in the `messages` table via `sticker_json` for auditing and historical training data.
    - **Preparedness for Future Multimodal LLM Vision Inspection**:
      - Sticker metadata signals are logged with structured prefix `[Sticker Signals]` and `[Sticker Detected]` for log ingestion pipelines.
      - The bot provides `GetStickerDirectURL(fileID)` via Telegram Bot API to download sticker images/frames for automated visual safety classification with multimodal LLMs.
    - **Exemptions**: Bot administrators, whitelisted members (`reputation >= 100`), and established users with reputation exceeding `max_reputation` are exempt. Built-in official Telegram animated emojis can optionally be permitted via `allow_official_animated_emoji`.

16. **Unified Detection Pipeline & Lifecycle Integration**:
    - Centralized modular detection engine evaluating across all lifecycle entrypoints: **Message Processing**, **Chat/Channel Join Events**, and **Offline Rescan Audits** (`/rescanusers`).

17. **Known Good & Known Bad Regression Suite (`known_good_data`, `known_bad_data`)**:
    - **Regression Protection**: Stores persistent datasets of Known Good community members (`reputation >= 50`) and Known Bad banned spammers in dedicated SQLite tables.
    - **Bypassed Rep/Admin Simulation**: When validating rules via `gogcbot validate-rules` or pre-scan checks, tests simulated profiles with reputation (`rep = 0`), admin status (`admin = false`), and banned status (`banned = false`) across join, rescan, message snippet, and secondary group contexts to ensure 0 false-positive regressions.
    - **Automated Kickban Button**: Automated rule hits do NOT automatically pollute known datasets. Instead, kickban alerts in the moderation group include an interactive `🏷️ Add to Known Bad` inline button allowing human moderators to deliberately add confirmed spam patterns to the Known Bad dataset.

18. **Comprehensive Post-Ban Message Purge & Forwarded Message Tracking**:
    - **All Recent User Messages Purged on Ban**: When a user is banned (via moderation card button `mod:ban`, automated trigger kickban `ActionBanUser`, `/ban` command, or `/rescan`), the bot retrieves all recent recorded messages for that user from the SQLite database (`DeleteRecentMessagesForUser`) and explicitly calls `DeleteGroupMessage` across the monitored groups.
    - **Forwarded Message Deletion**: Overcomes Telegram's server-side limitation where `banChatMember(revoke_messages=true)` fails to delete messages forwarded from another bot, channel, or user.
    - **Forwarded Message Tracking & Extraction**: Automatically detects and extracts forward metadata (`ForwardFrom`, `ForwardFromChat`, `ForwardSenderName`, `ForwardDate`) in `extractMessageText` and `handleMessage`, ensuring forwarded messages are always saved to `messages` history and attributed to the forwarding user.
    - **Idempotent & Non-Fatal Delete Error Handling**: Uses `isNonFatalDeleteError` to treat `message to delete not found`, `message can't be deleted`, and `message_id_invalid` as non-fatal successes, ensuring reliable execution even if messages were already removed manually.


18. **Explicit Dry-Run Gating for Mass Actions**:
    - **Enforced Dry-Run Prior to Live Bans**: Mass-banning rescan operations (`gogcbot rescan-users --live` or `/rescan live`) are strictly gated by the `dryrun_gates` table. A live run is refused unless a successful dry-run preview and rule pre-check were completed within the past 1 hour.
    - Consumes the gate immediately after live execution to ensure fresh dry-run verification for every subsequent batch action.

19. **Robust Cross-Group Moderation Execution**:
    - **Private Chat Safety**: Automatically filters out private 1-on-1 chat IDs (`chatID >= 0`) during multi-group bans, unbans, mutes, and ban audits so direct-message admin commands never fail on non-group chats.
    - **Non-Fatal Absence Handling**: Safely handles Telegram `USER_NOT_PARTICIPANT` / `USERNOTPARTICIPANT` responses as non-fatal successes when kicking or auditing users who are already absent from the chat.

20. **Moderation Review Deduplication & Edited Message Guards**:
    - **Single Active Flag per Message**: Enforces pending moderation review uniqueness per message `(group_chat_id, group_message_id)` via DB-level checks (`HasPendingFlag`) and `ErrAlreadyFlagged`. When a message is already pending review in the Private Moderation Group, subsequent edits or duplicate updates cannot generate duplicate review cards.
    - **Edited Message Safety**: Restricts AutoFlag low-reputation threshold checks (`user.Reputation <= FlagThreshold`) and unflagged daily activity bumps to initial messages (`msg.EditDate == 0`). Edits on pending or approved messages will not re-trigger moderation alerts or double-award approval reputation, while content-based additions (new web links or blocked keywords) in edits continue to be intercepted.
    - **Multi-Trigger Pipeline Deduplication**: Trigger rules executing `ActionFlagMessage` mark moderation action as taken to prevent pipeline fall-through from double-flagging with AutoFlag rules.

---

## 🚀 Installation & Usage

### Building from Source

```bash
git clone https://github.com/angch/gogcbot.git
cd gogcbot
CGO_ENABLED=0 go build -o gogcbot main.go
```

---

## 🛠️ CLI Commands & Service Management

### Standard CLI Commands

```bash
# 1. Generate default configuration file
./gogcbot init-config --output config.yaml

# 2. Run the bot interactively in foreground
./gogcbot run --config config.yaml

# 3. Check database statistics & config status offline
./gogcbot status --config config.yaml

# 4. Perform a manual database retention cleanup (7-day logs & 50 posts/user cap)
./gogcbot cleanup --config config.yaml

# 5. List known good and bad users (with manual moderator bans highlighted) in Markdown form
./gogcbot list-users --config config.yaml
./gogcbot list-users --config config.yaml --output user_directory.md
./gogcbot list-users --config config.yaml --manual-bans-only

# 6. Backfill user profiles (bios and profile pictures) from Telegram API into user_profiles table
./gogcbot backfill-profiles --config config.yaml
./gogcbot backfill-profiles --config config.yaml --force --delay-ms 100

# 7. List or batch-ban unbanned new/unknown users with few messages (with or without bios) and low reputation (<= 20)
./gogcbot list-unknownusers --config config.yaml
./gogcbot list-unknownusers --config config.yaml --keyword "沃尔玛" --output unknownusers.md
./gogcbot list-unknownusers --config config.yaml --max-posts 5
./gogcbot list-unknownusers --config config.yaml --max-rep 20
./gogcbot list-unknownusers --config config.yaml --ban

# 8. Manually rescan low-reputation users (>24h since last scan) - dry-run preview unlocks 1-hour live execution gate
./gogcbot rescan-users --config config.yaml
./gogcbot rescan-users --config config.yaml --max-rep 20 --hours 24
./gogcbot rescan-users --config config.yaml --force
# Execute bans live (requires prior dry-run preview):
./gogcbot rescan-users --config config.yaml --live

# 9. Validate all detection rules against Known Good & Known Bad datasets (0 false-positive check)
./gogcbot validate-rules --config config.yaml

# 10. Inspect or synchronize Known Good (rep >= 50) and Known Bad datasets in SQLite
./gogcbot known-data --config config.yaml
./gogcbot known-data --config config.yaml --list
./gogcbot known-data --config config.yaml --sync

# 11. Send a message to a Telegram group/channel as the bot
./gogcbot send -1001234567890 "Important announcement" --config config.yaml
./gogcbot send -1001234567890 -m "*Bold message*" --config config.yaml

# 12. Dump all known database info and profiles for a user by @tag or numeric ID
./gogcbot user @spambot --config config.yaml
./gogcbot user 555666 --config config.yaml
./gogcbot user @spambot --config config.yaml --json
./gogcbot user 555666 --config config.yaml --output user_dossier.md

# 13. Audit all banned users across monitored channels & groups (<= 1 req/sec) and enforce missing kick bans
./gogcbot bancheck --config config.yaml
./gogcbot bancheck --config config.yaml --dry-run
./gogcbot bancheck --config config.yaml --delay-ms 1000

# 14. Seed members from monitored groups via MTProto into users & user_joins tables
./gogcbot seedmembers --config config.yaml --api-id 12345678 --api-hash "your_api_hash"
./gogcbot seedmembers --config config.yaml --dry-run
TG_API_ID=12345678 TG_API_HASH="your_api_hash" ./gogcbot seedmembers --config config.yaml
```

### OS System Service Management (Windows Service / Systemd / launchd)

You can run `gogcbot` as a background system service that starts automatically on boot:

```bash
# Install as an OS service
./gogcbot service install --config C:\path\to\config.yaml

# Start the background service
./gogcbot service start

# Check the background service status
./gogcbot service status

# Stop the background service
./gogcbot service stop

# Uninstall the OS service
./gogcbot service uninstall
```

---

## ⚙️ Configuration (`config.yaml`)

```yaml
telegram_token: "YOUR_TELEGRAM_BOT_TOKEN"
super_admin_id: 123456789             # Super Admin Telegram User ID
moderation_group_id: -1001234567890   # Private Moderation Group Chat ID
db_path: "gogcbot.db"
log_level: "info"
cleanup_interval_hours: 1

auto_flag:
  enabled: true
  low_rep_threshold: 50
  flag_on_links: true
  delete_on_links: true
  new_user_min_posts: 3
  blocked_keywords:
    - "crypto giveaway"
    - "t.me/"
    - "whatsapp.com"
    - "airdrop"

reputation:
  default_initial: 0                  # Default rep for unseen users
  flag_threshold: 40
  approve_bonus: 5
  delete_penalty: 10
  warn_penalty: 20
  mute_penalty: 30
  ban_penalty: 50

detector:
  enabled: true
  new_user_cjk:
    enabled: true
    min_high_user_id: 1000000000       # User ID threshold for newly generated accounts
    max_reputation: 5                  # Maximum reputation score to apply detection
    max_user_posts: 5                  # Post count window for new user evaluation
    min_cjk_chars: 2                   # Minimum CJK characters required (default 2)
    min_cjk_ratio: 0.15                # Minimum CJK character ratio for longer messages (default 0.15)
    rep_penalty: 20                    # Reputation penalty applied upon detection
  new_user_spam_bio:
    enabled: true                      # Kick/ban new users with spam/scam profile bios
    max_reputation: 5                  # Maximum reputation score to apply detection
    max_user_posts: 5                  # Post count window for new user evaluation
    rep_penalty: 20                    # Reputation penalty applied upon detection
  red_packet_name:
    enabled: true                      # Kick/ban accounts with red packet emoji CJK names & mixed-caps usernames
    min_high_user_id: 1000000000
    max_reputation: 5
    max_user_posts: 5
    min_username_length: 5
    min_cjk_ratio: 0.5
    min_cjk_chars: 1
    rep_penalty: 20
  username_anomaly:
    enabled: true
    min_high_user_id: 1000000000
    max_reputation: 5
    max_user_posts: 5
    min_score: 3
    flag_only: true
    rep_penalty: 20
  profile_name_keyword_ban:
    enabled: true
    min_high_user_id: 1000000000
    max_reputation: 5
    max_user_posts: 5
    min_score: 3
    flag_only: true
    rep_penalty: 20
    blocked_keywords: ["0壹天", "每日", "吴压", "吾思", "兼织"]
  nonsense_bio:
    enabled: true
    min_high_user_id: 1000000000
    max_reputation: 5
    max_user_posts: 5
    min_words: 5
    flag_only: true
    rep_penalty: 20
  secondary_group:
    enabled: true
    max_reputation: 5
    rep_penalty: 20
  custom_sticker:
    enabled: true                      # Auto kick/ban low-reputation users sending custom stickers (e.g. spammed thumbs up / crypto stickers)
    max_reputation: 5                  # Maximum reputation score to apply detection
    max_user_posts: 5                  # Post count window for new user evaluation
    rep_penalty: 20                    # Reputation penalty applied upon detection
    allow_official_animated_emoji: false # Allow official Telegram animated emoji pack stickers

shieldy:
  enabled: true                       # Enable Shieldy captcha bot verification
  rep_bonus: 5                        # Rep bonus for typing "I am not a bot"
  max_messages: 5                     # Post count limit for initial user messages
  recheck_delay_minutes: 6            # Delay for verifying & re-issuing permanent bans
```

---

## 🤖 Telegram Bot Commands

| Command | Permission | Description |
| :--- | :--- | :--- |
| `/start` | Admin/Mod | Welcome message |
| `/help` | Admin/Mod | Command reference |
| `/status` | Admin/Mod | Bot status, metrics, and database stats |
| `/checkperms` | Admin/Mod | Verify bot admin rights in current chat |
| `/flag [reason]` | Admin/Mod | Reply to a message to flag it for moderation review |
| `/userinfo <user\|@user>` | Admin/Mod | View user reputation, warning count, profile & post history |
| `/fetchprofile <user>` | Admin/Mod | Fetch fresh Telegram profile (bio & picture) & cache in DB |
| `/backfillprofiles [force]` | Admin/Mod | Backfill bios and profile photos for tracked users (marks not-found users to skip repeats) |
| `/setsuperadmin` | First User / Super Admin | Set self as Super Admin (persists to `config.yaml`) |
| `/setmodgroup` | Super Admin | Set current group as Private Moderation Group |
| `/addgroup` | Admin/Mod | Add current group to monitored groups |
| `/removegroup` | Admin/Mod | Remove current group from monitored groups |
| `/setprimarygroup [chat_id]` | Admin/Mod | Set group as Primary Group |
| `/setsecondarygroup [chat_id]` | Admin/Mod | Set group as Secondary Group |
| `/listgroups` | Admin/Mod | List all monitored groups with Primary/Secondary badges |
| `/rep <user> [delta]` | Admin/Mod | Check or adjust user reputation |
| `/warn <user>` | Admin/Mod | Warn user, delete message, deduct rep |
| `/resetwarns <user>` | Admin/Mod | Reset warning count for user to 0 |
| `/mute <user> [hours]` | Admin/Mod | Mute user in group & deduct rep |
| `/ban <user>` | Admin/Mod | Ban user across all monitored groups |
| `/unban <user>` | Admin/Mod | Unban user across groups |
| `/promote <user>` | Admin/Mod | Promote user to Bot Admin & set rep to 100 |
| `/demote <user>` | Super Admin | Remove Bot Admin privileges and reset rep |
| `/listusers` | Admin/Mod | List known good/bad users and moderation status |
| `/listunknownusers [kw] [ban]` | Admin/Mod | Compact monospace table list or batch-ban unbanned new users with few messages (with or without bios) |
| `/rescanusers [live] [force]` | Admin/Mod | Manually rescan low-rep users (>24h since last scan, dry-run by default; use 'live' to execute) |
| `/send <group> <text>` | Super Admin / Bot Admin | Send a message to a monitored Telegram group/channel as the bot |
| `/bancheck [dryrun]` | Admin/Mod | Audit all banned users across monitored channels/groups (<= 1 req/sec) & enforce missing kick bans |
| `/cleanup` | Admin/Mod | Run retention cleanup on demand |
| `/getdb` | Bot Admin (Direct PM only) | Download a copy of current SQLite3 database |

---

## 🛡️ License

MIT License
