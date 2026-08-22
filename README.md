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
   - Automatically exempts verified users and `"I am not a bot"` messages from triggering the High-ID CJK spam detection trigger.
   - Automatically schedules a **6-minute delayed re-check** upon issuing bans to detect if Shieldy or Telegram altered a permanent ban into a temporary timed ban, re-issuing permanent bans if necessary.

4. **Auto-Flagging & Moderation Engine**:
   - Automatically detects suspicious messages:
     - Low reputation user (< 50) posting links.
     - New account (<= 3 posts) posting links.
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

10. **Private Message Logging & Moderation Channel Mirroring**:
    - **Full Visibility & Audit Trail**: Any private direct message (PM/DM) sent to the bot is automatically saved to the SQLite conversation history and logged to the system logs.
    - **Automated Mirroring to Moderation Channel**: Private messages from non-admin users (including text, media, edited messages, and forwards) are immediately formatted and mirrored directly to the Private Moderation Group (`moderation_group_id`), complete with sender information, user ID, reputation score, warning count, message timestamp, and forwarded attachments. Private messages from known bot admins (Super Admin, Bot Admins, and Moderation Group members) are kept private and excluded from mirroring.

11. **Auto-Generated Username Anomaly Detection (`username_anomaly`)**:
    - **Algorithmic Handle Scoring**: Detects automated/farmed Telegram handles for new high-ID users using mixed-case patterns, interleaved digit sequences (e.g. `nykrr_v9cl2t`), and Shannon entropy gated with low vowel ratios (e.g. `qtdulcljcptk6950`).
    - Configurable score thresholds (`min_score: 3`) and `flag_only` mode.

12. **Profile Name Spam Keyword Ban (`profile_name_keyword_ban`)**:
    - **Homoglyph Normalization**: Folds visually confusable digits and letters (`o/O/零/〇 -> 0`, `l/L -> 1`) to catch evasive name variations (e.g. `六o0壹天` or `玖Oo壹天` hitting `0壹天`).
    - Evaluates display names against calibrated blocklists for new high-ID users with low reputation.

13. **Unified Detection Pipeline & Lifecycle Integration**:
    - Centralized modular detection engine evaluating across all lifecycle entrypoints: **Message Processing**, **Chat/Channel Join Events**, and **Offline Rescan Audits** (`/rescanusers`).




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

# 8. Manually rescan low-reputation users (>24h since last scan) & trigger join ban rules
./gogcbot rescan-users --config config.yaml
./gogcbot rescan-users --config config.yaml --max-rep 20 --hours 24
./gogcbot rescan-users --config config.yaml --force
./gogcbot rescan-users --config config.yaml --dry-run

# 9. Dump all known database info and profiles for a user by @tag or numeric ID
./gogcbot user @spambot --config config.yaml
./gogcbot user 555666 --config config.yaml
./gogcbot user @spambot --config config.yaml --json
./gogcbot user 555666 --config config.yaml --output user_dossier.md

# 10. Audit all banned users across monitored channels & groups (<= 1 req/sec) and enforce missing kick bans
./gogcbot bancheck --config config.yaml
./gogcbot bancheck --config config.yaml --dry-run
./gogcbot bancheck --config config.yaml --delay-ms 1000
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
| `/listgroups` | Admin/Mod | List all monitored groups |
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
| `/rescanusers [force] [dryrun]` | Admin/Mod | Manually rescan low-rep users (>24h since last scan) & trigger join ban rules |
| `/bancheck [dryrun]` | Admin/Mod | Audit all banned users across monitored channels/groups (<= 1 req/sec) & enforce missing kick bans |
| `/cleanup` | Admin/Mod | Run retention cleanup on demand |
| `/getdb` | Bot Admin (Direct PM only) | Download a copy of current SQLite3 database |

---

## 🛡️ License

MIT License
