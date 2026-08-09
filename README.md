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

4. **Auto-Flagging & Moderation Engine**:
   - Automatically detects suspicious messages:
     - Low reputation user (< 50) posting links.
     - New account (<= 3 posts) posting links.
     - Blocked spam/crypto keywords or custom regexes.
     - Reputation falling below threshold.
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
| `/userinfo <user\|@user>` | Admin/Mod | View user reputation, warning count, & post history |
| `/setsuperadmin` | First User / Super Admin | Set self as Super Admin (persists to `config.yaml`) |
| `/setmodgroup` | Super Admin | Set current group as Private Moderation Group |
| `/addgroup` | Admin/Mod | Add current group to monitored groups |
| `/removegroup` | Admin/Mod | Remove current group from monitored groups |
| `/listgroups` | Admin/Mod | List all monitored groups |
| `/rep <user> [delta]` | Admin/Mod | Check or adjust user reputation |
| `/warn <user>` | Admin/Mod | Warn user, delete message, deduct rep |
| `/mute <user> [hours]` | Admin/Mod | Mute user in group & deduct rep |
| `/ban <user>` | Admin/Mod | Ban user across all monitored groups |
| `/unban <user>` | Admin/Mod | Unban user across groups |
| `/cleanup` | Admin/Mod | Run retention cleanup on demand |

---

## 🛡️ License

MIT License
