
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/Armin-kho/persian-currency-bot/internal/items"
)

type DB struct {
	sql *sql.DB
}

func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Reasonable defaults
	sqldb.SetMaxOpenConns(1) // SQLite best practice for embedded use
	sqldb.SetConnMaxLifetime(0)

	db := &DB{sql: sqldb}
	if err := db.migrate(context.Background()); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	if err := db.seedBuiltins(context.Background()); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error {
	return d.sql.Close()
}

func (d *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS admins (user_id INTEGER PRIMARY KEY, is_super INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS chats (
			chat_id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			type TEXT NOT NULL,
			approved INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS chat_settings (
			chat_id INTEGER PRIMARY KEY REFERENCES chats(chat_id) ON DELETE CASCADE,
			source_provider TEXT NOT NULL DEFAULT 'bonbast',
			source_method TEXT NOT NULL DEFAULT 'scrape',
			interval_minutes INTEGER NOT NULL DEFAULT 5,
			downtime_enabled INTEGER NOT NULL DEFAULT 0,
			downtime_start TEXT NOT NULL DEFAULT '20:00',
			downtime_end TEXT NOT NULL DEFAULT '10:00',
			trigger_items TEXT NOT NULL DEFAULT '[]',
			trigger_threshold_type TEXT NOT NULL DEFAULT 'abs',
			trigger_threshold_value REAL NOT NULL DEFAULT 0,
			post_mode TEXT NOT NULL DEFAULT 'edit',
			price_mode TEXT NOT NULL DEFAULT 'sell',
			digits TEXT NOT NULL DEFAULT 'en',
			show_same_arrow INTEGER NOT NULL DEFAULT 0,
			template_id TEXT NOT NULL DEFAULT 'tmpl_default',
			last_post_message_id INTEGER,
			last_post_time INTEGER,
			last_fetch_time INTEGER,
			last_error TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS chat_items (
			chat_id INTEGER NOT NULL REFERENCES chats(chat_id) ON DELETE CASCADE,
			item_id TEXT NOT NULL,
			position INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (chat_id, item_id)
		);`,
		`CREATE TABLE IF NOT EXISTS templates (
			template_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			body TEXT NOT NULL,
			media_type TEXT NOT NULL DEFAULT '',
			media_file_id TEXT NOT NULL DEFAULT '',
			is_builtin INTEGER NOT NULL DEFAULT 0,
			created_by INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS chat_last_values (
			chat_id INTEGER NOT NULL REFERENCES chats(chat_id) ON DELETE CASCADE,
			item_id TEXT NOT NULL,
			last_value REAL NOT NULL,
			last_updated_at INTEGER NOT NULL,
			PRIMARY KEY(chat_id, item_id)
		);`,
		`CREATE TABLE IF NOT EXISTS global_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_items_chat_position ON chat_items(chat_id, position);`,
	}
	for _, s := range stmts {
		if _, err := d.sql.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) seedBuiltins(ctx context.Context) error {
	// Built-in templates
	type tmpl struct {
		ID, Name, Desc, Body string
	}
	builtins := []tmpl{
		{
			ID:   "tmpl_default",
			Name: "پیش‌فرض (کلاسیک)",
			Desc: "همان قالب نمونه شما با جداکننده‌ها و تاریخ پایین",
			Body: defaultTemplateBody(),
		},
		{
			ID:   "tmpl_compact",
			Name: "فشرده",
			Desc: "کم‌خط و بدون خطوط جداکننده زیاد",
			Body: "{CURRENCIES}\n—\n{COINS}\n—\n{GOLD}\n\n{DATETIME}",
		},
		{
			ID:   "tmpl_boxed",
			Name: "کادری",
			Desc: "با کادر ASCII برای کانال‌های شلوغ",
			Body: "┌──────────────┐\n{CURRENCIES}\n├──────────────┤\n{COINS}\n├──────────────┤\n{GOLD}\n└──────────────┘\n{DATETIME}",
		},
		{
			ID:   "tmpl_channel",
			Name: "مخصوص کانال",
			Desc: "سرتیتر کوتاه + بخش‌بندی",
			Body: "📌 نرخ لحظه‌ای ارز و طلا\n\n{CURRENCIES}\n_______________________\n{COINS}\n_______________________\n{GOLD}\n\n🕒 {DATETIME}",
		},
		{
			ID:   "tmpl_group",
			Name: "مخصوص گروه",
			Desc: "سبک دوستانه‌تر",
			Body: "سلام 👋\n\n{CURRENCIES}\n_______________________\n{COINS}\n_______________________\n{GOLD}\n\n🕒 {DATETIME}",
		},
		{
			ID:   "tmpl_minimal",
			Name: "مینیمال",
			Desc: "فقط خطوط داده + تاریخ",
			Body: "{CURRENCIES}\n{COINS}\n{GOLD}\n\n{DATETIME}",
		},
		{
			ID:   "tmpl_bold_headers",
			Name: "سرتیتر پررنگ",
			Desc: "سه سرفصل با ایموجی",
			Body: "💱 ارز\n{CURRENCIES}\n\n🪙 سکه\n{COINS}\n\n⚜️ طلا/جهانی\n{GOLD}\n\n{DATETIME}",
		},
		{
			ID:   "tmpl_two_cols",
			Name: "دو ستونی (با نقطه‌چین)",
			Desc: "برای خوانایی بیشتر",
			Body: "{CURRENCIES}\n........................\n{COINS}\n........................\n{GOLD}\n\n{DATETIME}",
		},
		{
			ID:   "tmpl_news",
			Name: "خبرنامه‌ای",
			Desc: "عنوان + بدنه + امضا",
			Body: "📰 بروزرسانی بازار\n\n{CURRENCIES}\n_______________________\n{COINS}\n_______________________\n{GOLD}\n\n—\n{DATETIME}\n@YourChannel",
		},
		{
			ID:   "tmpl_cards",
			Name: "کارت‌ها",
			Desc: "هر بخش شبیه کارت",
			Body: "💱 ارز\n{CURRENCIES}\n\n🪙 سکه\n{COINS}\n\n⚜️ طلا\n{GOLD}\n\n{DATETIME}",
		},
		{
			ID:   "tmpl_ultra",
			Name: "خیلی فشرده",
			Desc: "برای محدودیت طول پیام",
			Body: "{CURRENCIES}\n{COINS}\n{GOLD}\n{DATETIME}",
		},
	}
	now := time.Now().Unix()
	for _, t := range builtins {
		_, err := d.sql.ExecContext(ctx,
			`INSERT OR IGNORE INTO templates(template_id,name,description,body,is_builtin,created_by,created_at) VALUES(?,?,?,?,1,0,?)`,
			t.ID, t.Name, t.Desc, t.Body, now)
		if err != nil {
			return err
		}
	}

	return nil
}

func defaultTemplateBody() string {
	// Important: Keep format order: emoji + name + price + arrow, and date at the bottom.
	return "{CURRENCIES}\n_______________________\n{COINS}\n_______________________\n{GOLD}\n\n{DATETIME}"
}

func (d *DB) SeedFromConfig(ctx context.Context, bonbastUser, bonbastHash, navasanKey string, initialAdmins []int64) error {
	// Seed global settings if missing
	if bonbastUser != "" {
		_ = d.SetGlobalSetting(ctx, "bonbast_api_username", bonbastUser)
	}
	if bonbastHash != "" {
		_ = d.SetGlobalSetting(ctx, "bonbast_api_hash", bonbastHash)
	}
	if navasanKey != "" {
		_ = d.SetGlobalSetting(ctx, "navasan_api_key", navasanKey)
	}

	// Seed admins only if admins table is empty.
	count, err := d.AdminCount(ctx)
	if err != nil {
		return err
	}
	if count == 0 && len(initialAdmins) > 0 {
		for i, id := range initialAdmins {
			isSuper := 0
			if i == 0 {
				isSuper = 1
			}
			if err := d.AddAdmin(ctx, id, isSuper == 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) AdminCount(ctx context.Context) (int, error) {
	var c int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(1) FROM admins`).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

func (d *DB) IsAdmin(ctx context.Context, userID int64) (bool, bool, error) {
	var isSuper int
	err := d.sql.QueryRowContext(ctx, `SELECT is_super FROM admins WHERE user_id=?`, userID).Scan(&isSuper)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, isSuper == 1, nil
}

func (d *DB) AddAdmin(ctx context.Context, userID int64, super bool) error {
	isSuper := 0
	if super {
		isSuper = 1
	}
	_, err := d.sql.ExecContext(ctx, `INSERT OR REPLACE INTO admins(user_id,is_super,created_at) VALUES(?,?,?)`, userID, isSuper, time.Now().Unix())
	return err
}

func (d *DB) RemoveAdmin(ctx context.Context, userID int64) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM admins WHERE user_id=?`, userID)
	return err
}

type Admin struct {
	UserID  int64
	IsSuper bool
}

func (d *DB) ListAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT user_id,is_super FROM admins ORDER BY is_super DESC, user_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Admin
	for rows.Next() {
		var a Admin
		var isSuper int
		if err := rows.Scan(&a.UserID, &isSuper); err != nil {
			return nil, err
		}
		a.IsSuper = isSuper == 1
		out = append(out, a)
	}
	return out, nil
}

type Chat struct {
	ChatID   int64
	Title    string
	Type     string // group/supergroup/channel
	Approved bool
	Enabled  bool
}

func (d *DB) UpsertChat(ctx context.Context, chatID int64, title, typ string) error {
	now := time.Now().Unix()
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO chats(chat_id,title,type,approved,enabled,created_at,updated_at)
		 VALUES(?,?,?,0,1,?,?)
		 ON CONFLICT(chat_id) DO UPDATE SET title=excluded.title, type=excluded.type, updated_at=excluded.updated_at`,
		chatID, title, typ, now, now)
	if err != nil {
		return err
	}
	// Ensure settings row
	_, _ = d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO chat_settings(chat_id) VALUES(?)`, chatID)
	// Ensure default items order exists
	return d.ensureDefaultChatItems(ctx, chatID)
}

func (d *DB) ensureDefaultChatItems(ctx context.Context, chatID int64) error {
	// If no items yet, insert defaults in order and disable the rest.
	var c int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(1) FROM chat_items WHERE chat_id=?`, chatID).Scan(&c); err != nil {
		return err
	}
	if c > 0 {
		return nil
	}

	defaults := items.Defaults()
	enabledSet := map[string]bool{}
	for _, id := range defaults {
		enabledSet[id] = true
	}
	pos := 0
	for _, it := range items.All {
		pos++
		en := 0
		if enabledSet[it.ID] {
			en = 1
		}
		_, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO chat_items(chat_id,item_id,position,enabled) VALUES(?,?,?,?)`, chatID, it.ID, pos, en)
		if err != nil {
			return err
		}
	}
	// Re-order enabled defaults to be first, in requested order.
	// We'll set positions explicitly: defaults in order, then the rest.
	all := items.All
	pos = 0
	for _, id := range defaults {
		pos++
		_, _ = d.sql.ExecContext(ctx, `UPDATE chat_items SET position=?, enabled=1 WHERE chat_id=? AND item_id=?`, pos, chatID, id)
	}
	for _, it := range all {
		if enabledSet[it.ID] {
			continue
		}
		pos++
		_, _ = d.sql.ExecContext(ctx, `UPDATE chat_items SET position=? WHERE chat_id=? AND item_id=?`, pos, chatID, it.ID)
	}
	return nil
}

func (d *DB) SetChatApproved(ctx context.Context, chatID int64, approved bool) error {
	val := 0
	if approved {
		val = 1
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE chats SET approved=?, updated_at=? WHERE chat_id=?`, val, time.Now().Unix(), chatID)
	return err
}

func (d *DB) SetChatEnabled(ctx context.Context, chatID int64, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE chats SET enabled=?, updated_at=? WHERE chat_id=?`, val, time.Now().Unix(), chatID)
	return err
}

func (d *DB) GetChat(ctx context.Context, chatID int64) (Chat, error) {
	var c Chat
	var approved, enabled int
	err := d.sql.QueryRowContext(ctx, `SELECT chat_id,title,type,approved,enabled FROM chats WHERE chat_id=?`, chatID).
		Scan(&c.ChatID, &c.Title, &c.Type, &approved, &enabled)
	if err != nil {
		return Chat{}, err
	}
	c.Approved = approved == 1
	c.Enabled = enabled == 1
	return c, nil
}

func (d *DB) ListChats(ctx context.Context) ([]Chat, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT chat_id,title,type,approved,enabled FROM chats ORDER BY approved ASC, type DESC, title COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var approved, enabled int
		if err := rows.Scan(&c.ChatID, &c.Title, &c.Type, &approved, &enabled); err != nil {
			return nil, err
		}
		c.Approved = approved == 1
		c.Enabled = enabled == 1
		out = append(out, c)
	}
	return out, nil
}

type ChatSettings struct {
	ChatID int64

	SourceProvider string
	SourceMethod   string

	IntervalMinutes int

	DowntimeEnabled bool
	DowntimeStart   string
	DowntimeEnd     string

	TriggerItems        []string
	TriggerThresholdType  string
	TriggerThresholdValue float64

	PostMode  string // new/edit
	PriceMode string // sell/buy/both
	Digits    string // en/fa

	ShowSameArrow bool

	TemplateID string

	LastPostMessageID sql.NullInt64
	LastPostTime      sql.NullInt64
	LastFetchTime     sql.NullInt64
	LastError         sql.NullString
}

func (d *DB) GetChatSettings(ctx context.Context, chatID int64) (ChatSettings, error) {
	var s ChatSettings
	s.ChatID = chatID
	var downtimeEnabled int
	var showSame int
	var trigJSON string
	err := d.sql.QueryRowContext(ctx, `SELECT source_provider,source_method,interval_minutes,downtime_enabled,downtime_start,downtime_end,
		trigger_items,trigger_threshold_type,trigger_threshold_value,post_mode,price_mode,digits,show_same_arrow,template_id,
		last_post_message_id,last_post_time,last_fetch_time,last_error
		FROM chat_settings WHERE chat_id=?`, chatID).
		Scan(&s.SourceProvider, &s.SourceMethod, &s.IntervalMinutes,
			&downtimeEnabled, &s.DowntimeStart, &s.DowntimeEnd,
			&trigJSON, &s.TriggerThresholdType, &s.TriggerThresholdValue,
			&s.PostMode, &s.PriceMode, &s.Digits, &showSame, &s.TemplateID,
			&s.LastPostMessageID, &s.LastPostTime, &s.LastFetchTime, &s.LastError)
	if err != nil {
		return ChatSettings{}, err
	}
	s.DowntimeEnabled = downtimeEnabled == 1
	s.ShowSameArrow = showSame == 1
	_ = json.Unmarshal([]byte(trigJSON), &s.TriggerItems)
	return s, nil
}

func (d *DB) UpdateChatSetting(ctx context.Context, chatID int64, key string, value any) error {
	allowed := map[string]bool{
		"source_provider": true, "source_method": true, "interval_minutes": true,
		"downtime_enabled": true, "downtime_start": true, "downtime_end": true,
		"trigger_items": true, "trigger_threshold_type": true, "trigger_threshold_value": true,
		"post_mode": true, "price_mode": true, "digits": true, "show_same_arrow": true,
		"template_id": true,
	}
	if !allowed[key] {
		return fmt.Errorf("invalid setting key: %s", key)
	}

	// Special: trigger_items expects []string; store as JSON
	if key == "trigger_items" {
		b, _ := json.Marshal(value)
		value = string(b)
	}
	if key == "downtime_enabled" || key == "show_same_arrow" {
		// accept bool
		if bv, ok := value.(bool); ok {
			if bv {
				value = 1
			} else {
				value = 0
			}
		}
	}
	_, err := d.sql.ExecContext(ctx, fmt.Sprintf(`UPDATE chat_settings SET %s=? WHERE chat_id=?`, key), value, chatID)
	return err
}

type ChatItem struct {
	ItemID   string
	Position int
	Enabled  bool
}

func (d *DB) ListChatItems(ctx context.Context, chatID int64) ([]ChatItem, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT item_id,position,enabled FROM chat_items WHERE chat_id=? ORDER BY position ASC`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatItem
	for rows.Next() {
		var it ChatItem
		var en int
		if err := rows.Scan(&it.ItemID, &it.Position, &en); err != nil {
			return nil, err
		}
		it.Enabled = en == 1
		out = append(out, it)
	}
	return out, nil
}

func (d *DB) EnabledItemIDs(ctx context.Context, chatID int64) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT item_id FROM chat_items WHERE chat_id=? AND enabled=1 ORDER BY position ASC`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (d *DB) ToggleChatItem(ctx context.Context, chatID int64, itemID string) (bool, error) {
	var cur int
	err := d.sql.QueryRowContext(ctx, `SELECT enabled FROM chat_items WHERE chat_id=? AND item_id=?`, chatID, itemID).Scan(&cur)
	if err != nil {
		return false, err
	}
	next := 1
	if cur == 1 {
		next = 0
	}
	_, err = d.sql.ExecContext(ctx, `UPDATE chat_items SET enabled=? WHERE chat_id=? AND item_id=?`, next, chatID, itemID)
	return next == 1, err
}

func (d *DB) MoveChatItem(ctx context.Context, chatID int64, itemID string, dir string) error {
	itemsList, err := d.ListChatItems(ctx, chatID)
	if err != nil {
		return err
	}
	// Find index
	idx := -1
	for i, it := range itemsList {
		if it.ItemID == itemID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("item not found")
	}
	swapWith := -1
	if dir == "up" && idx > 0 {
		swapWith = idx - 1
	} else if dir == "down" && idx < len(itemsList)-1 {
		swapWith = idx + 1
	}
	if swapWith == -1 {
		return nil
	}
	a := itemsList[idx]
	b := itemsList[swapWith]
	// swap positions
	_, err = d.sql.ExecContext(ctx, `UPDATE chat_items SET position=? WHERE chat_id=? AND item_id=?`, b.Position, chatID, a.ItemID)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `UPDATE chat_items SET position=? WHERE chat_id=? AND item_id=?`, a.Position, chatID, b.ItemID)
	return err
}

func (d *DB) GetTemplate(ctx context.Context, templateID string) (Template, error) {
	var t Template
	var isBuiltin int
	err := d.sql.QueryRowContext(ctx, `SELECT template_id,name,description,body,media_type,media_file_id,is_builtin,created_by,created_at FROM templates WHERE template_id=?`, templateID).
		Scan(&t.TemplateID, &t.Name, &t.Description, &t.Body, &t.MediaType, &t.MediaFileID, &isBuiltin, &t.CreatedBy, &t.CreatedAt)
	if err != nil {
		return Template{}, err
	}
	t.IsBuiltin = isBuiltin == 1
	return t, nil
}

type Template struct {
	TemplateID  string
	Name        string
	Description string
	Body        string
	MediaType   string // "", "photo", "video"
	MediaFileID string
	IsBuiltin   bool
	CreatedBy   int64
	CreatedAt   int64
}

func (d *DB) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT template_id,name,description,body,media_type,media_file_id,is_builtin,created_by,created_at FROM templates ORDER BY is_builtin DESC, name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var t Template
		var isBuiltin int
		if err := rows.Scan(&t.TemplateID, &t.Name, &t.Description, &t.Body, &t.MediaType, &t.MediaFileID, &isBuiltin, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.IsBuiltin = isBuiltin == 1
		out = append(out, t)
	}
	return out, nil
}

func (d *DB) CreateTemplate(ctx context.Context, name, desc, body string, createdBy int64) (Template, error) {
	id := "tmpl_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	now := time.Now().Unix()
	_, err := d.sql.ExecContext(ctx, `INSERT INTO templates(template_id,name,description,body,is_builtin,created_by,created_at) VALUES(?,?,?,?,0,?,?)`,
		id, name, desc, body, createdBy, now)
	if err != nil {
		return Template{}, err
	}
	return d.GetTemplate(ctx, id)
}

func (d *DB) UpdateTemplateBody(ctx context.Context, templateID, body string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE templates SET body=? WHERE template_id=?`, body, templateID)
	return err
}

func (d *DB) UpdateTemplateMeta(ctx context.Context, templateID, name, desc string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE templates SET name=?, description=? WHERE template_id=?`, name, desc, templateID)
	return err
}

func (d *DB) SetTemplateMedia(ctx context.Context, templateID, mediaType, fileID string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE templates SET media_type=?, media_file_id=? WHERE template_id=?`, mediaType, fileID, templateID)
	return err
}

func (d *DB) ClearTemplateMedia(ctx context.Context, templateID string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE templates SET media_type='', media_file_id='' WHERE template_id=?`, templateID)
	return err
}

func (d *DB) SetChatTemplate(ctx context.Context, chatID int64, templateID string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE chat_settings SET template_id=? WHERE chat_id=?`, templateID, chatID)
	return err
}

func (d *DB) GetLastValue(ctx context.Context, chatID int64, itemID string) (float64, bool, error) {
	var v float64
	err := d.sql.QueryRowContext(ctx, `SELECT last_value FROM chat_last_values WHERE chat_id=? AND item_id=?`, chatID, itemID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}

func (d *DB) SetLastValue(ctx context.Context, chatID int64, itemID string, value float64) error {
	now := time.Now().Unix()
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO chat_last_values(chat_id,item_id,last_value,last_updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(chat_id,item_id) DO UPDATE SET last_value=excluded.last_value, last_updated_at=excluded.last_updated_at`,
		chatID, itemID, value, now)
	return err
}

func (d *DB) UpdateLastPost(ctx context.Context, chatID int64, messageID int, at time.Time) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE chat_settings SET last_post_message_id=?, last_post_time=? WHERE chat_id=?`, messageID, at.Unix(), chatID)
	return err
}

func (d *DB) UpdateFetchHealth(ctx context.Context, chatID int64, fetchedAt time.Time, errMsg string) error {
	var errVal any = nil
	if errMsg != "" {
		errVal = errMsg
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE chat_settings SET last_fetch_time=?, last_error=? WHERE chat_id=?`, fetchedAt.Unix(), errVal, chatID)
	return err
}

func (d *DB) GetGlobalSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := d.sql.QueryRowContext(ctx, `SELECT value FROM global_settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (d *DB) SetGlobalSetting(ctx context.Context, key, value string) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO global_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ExportChatSettings returns a JSON blob that can be imported into another chat.
func (d *DB) ExportChatSettings(ctx context.Context, chatID int64) ([]byte, error) {
	s, err := d.GetChatSettings(ctx, chatID)
	if err != nil {
		return nil, err
	}
	itemsList, err := d.ListChatItems(ctx, chatID)
	if err != nil {
		return nil, err
	}
	// Keep stable export order
	sort.Slice(itemsList, func(i, j int) bool { return itemsList[i].Position < itemsList[j].Position })

	payload := map[string]any{
		"version": 1,
		"settings": map[string]any{
			"source_provider":          s.SourceProvider,
			"source_method":            s.SourceMethod,
			"interval_minutes":         s.IntervalMinutes,
			"downtime_enabled":         s.DowntimeEnabled,
			"downtime_start":           s.DowntimeStart,
			"downtime_end":             s.DowntimeEnd,
			"trigger_items":            s.TriggerItems,
			"trigger_threshold_type":   s.TriggerThresholdType,
			"trigger_threshold_value":  s.TriggerThresholdValue,
			"post_mode":                s.PostMode,
			"price_mode":               s.PriceMode,
			"digits":                   s.Digits,
			"show_same_arrow":          s.ShowSameArrow,
			"template_id":              s.TemplateID,
		},
		"items": itemsList,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func (d *DB) ImportChatSettings(ctx context.Context, chatID int64, data []byte) error {
	var payload struct {
		Version  int `json:"version"`
		Settings map[string]any `json:"settings"`
		Items    []ChatItem `json:"items"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.Version != 1 {
		return fmt.Errorf("unsupported settings version: %d", payload.Version)
	}
	// Apply settings keys we know
	for k, v := range payload.Settings {
		switch k {
		case "source_provider","source_method","interval_minutes","downtime_start","downtime_end","post_mode","price_mode","digits","template_id","trigger_threshold_type":
			_ = d.UpdateChatSetting(ctx, chatID, k, v)
		case "downtime_enabled","show_same_arrow":
			if b, ok := v.(bool); ok {
				_ = d.UpdateChatSetting(ctx, chatID, k, b)
			}
		case "trigger_items":
			// JSON decode into []string
			b, _ := json.Marshal(v)
			var arr []string
			_ = json.Unmarshal(b, &arr)
			_ = d.UpdateChatSetting(ctx, chatID, "trigger_items", arr)
		case "trigger_threshold_value":
			// could be float64
			switch n := v.(type) {
			case float64:
				_ = d.UpdateChatSetting(ctx, chatID, k, n)
			case int:
				_ = d.UpdateChatSetting(ctx, chatID, k, float64(n))
			}
		}
	}
	// Apply items ordering
	for _, it := range payload.Items {
		en := 0
		if it.Enabled {
			en = 1
		}
		_, _ = d.sql.ExecContext(ctx, `INSERT INTO chat_items(chat_id,item_id,position,enabled) VALUES(?,?,?,?)
			ON CONFLICT(chat_id,item_id) DO UPDATE SET position=excluded.position, enabled=excluded.enabled`,
			chatID, it.ItemID, it.Position, en)
	}
	// normalize positions to avoid duplicates
	return d.normalizePositions(ctx, chatID)
}

func (d *DB) normalizePositions(ctx context.Context, chatID int64) error {
	itemsList, err := d.ListChatItems(ctx, chatID)
	if err != nil {
		return err
	}
	sort.Slice(itemsList, func(i, j int) bool { return itemsList[i].Position < itemsList[j].Position })
	for i, it := range itemsList {
		_, _ = d.sql.ExecContext(ctx, `UPDATE chat_items SET position=? WHERE chat_id=? AND item_id=?`, i+1, chatID, it.ItemID)
	}
	return nil
}
