
# Persian Currency Bot (Telegram)

A Telegram bot that periodically posts **Iran currency / coin / gold / BTC** rates into approved **channels and groups**.

✅ **Everything is managed from the bot UI using inline buttons** (no commands required).  
✅ Supports **Bonbast** and **Navasan** as data sources, with **two modes**:
- **API mode** (recommended if you have keys)  
- **Scrape mode** (works without keys; may break if site changes)

---

## Key Features

- **Approval flow**: when the bot is added to a channel/group, it becomes **pending approval**. Admins can approve/deny from private chat.
- **Admin system**:
  - If no initial admin IDs are provided, **the first user who opens the bot in private becomes the super admin**.
  - Super admin can add more bot admins from inside the bot.
- **Per-chat configuration** (only in private chat, only bot admins):
  - Source provider: Bonbast / Navasan
  - Source method: API / Scrape
  - Interval: 1–120 minutes (aligned to Tehran minute boundaries)
  - Downtime window (supports cross‑midnight)
  - Trigger-based posting (only post when selected items change)
  - Adjustable threshold (absolute or percent)
  - Post mode: **Edit latest** or **New message**
  - Price mode: **Sell / Buy / Both**
  - Digits: English or Persian digits
  - Templates: select from built-ins or create/edit custom templates
  - **Template preview**: see output in private chat without posting
  - Template media: attach **photo or video** per template
- **Health / status panel** per chat: last fetch time, last post time, current source, last error.
- **Failure notifications**: if a source fails, admins get a DM with quick buttons to switch providers.
- **Backup/restore DB** from inside the bot UI.

---

## Data Source Options (Pros/Cons)

### Option A — API (recommended)
✅ More stable  
✅ Lower risk of breaking when website changes  
✅ Usually faster and less likely to be rate-limited  
❌ Requires API credentials (may be paid)

### Option B — Scrape / Unofficial endpoints
✅ Works without API keys  
✅ Easy to start  
❌ May break if the website changes internal endpoints or adds restrictions  
❌ Higher risk of temporary blocks / captchas / rate limits

Inside the bot you can switch between API and Scrape at any time.

---

## Run with Docker (recommended)

1) Copy the example config:
```bash
cp config.example.json config.json
```

2) Edit `config.json` and set:
- `bot_token`
- optionally `initial_admin_ids`

3) Run:
```bash
docker compose up -d --build
```

---

## Run with systemd (Linux)

You can build and install the binary yourself:

```bash
go build -o persian-currency-bot ./cmd/bot
sudo install -m 0755 persian-currency-bot /usr/local/bin/persian-currency-bot
```

Create config:
```bash
sudo mkdir -p /etc/persian-currency-bot
sudo cp config.example.json /etc/persian-currency-bot/config.json
sudo nano /etc/persian-currency-bot/config.json
```

Create data dir:
```bash
sudo mkdir -p /var/lib/persian-currency-bot
sudo chown -R $(whoami) /var/lib/persian-currency-bot
```

Run:
```bash
/usr/local/bin/persian-currency-bot -config /etc/persian-currency-bot/config.json
```

---

## Bot Usage

- Add bot to channel/group (make it admin if required).
- Bot posts a message: **pending approval**.
- In private chat, open the bot and go:
  - **Chats / Channels → Select chat → Approve**
- Configure items, templates, interval, trigger, downtime, etc.
- Use **Preview** button in templates before applying changes.

---

## Template Variables

Use these placeholders inside templates:

- `{CURRENCIES}`
- `{COINS}`
- `{GOLD}`  (gold + crypto)
- `{DATETIME}` (Jalali date/time in Tehran)
- `{DATE}`
- `{TIME}`

---

## Notes

- This project uses SQLite (embedded DB).
- Restart policy is handled by Docker or systemd.
