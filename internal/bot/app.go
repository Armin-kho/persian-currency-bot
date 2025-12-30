
package bot

import (
	"context"
		"fmt"
	"io"
	"net/http"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Armin-kho/persian-currency-bot/internal/config"
	"github.com/Armin-kho/persian-currency-bot/internal/db"
	"github.com/Armin-kho/persian-currency-bot/internal/items"
	"github.com/Armin-kho/persian-currency-bot/internal/render"
	"github.com/Armin-kho/persian-currency-bot/internal/scheduler"
	"github.com/Armin-kho/persian-currency-bot/internal/sources"
	"github.com/Armin-kho/persian-currency-bot/internal/utils"
)

type Awaiting string

const (
	AwaitNone Awaiting = ""

	AwaitAddAdmin       Awaiting = "add_admin"
	AwaitSetBonUser     Awaiting = "set_bon_user"
	AwaitSetBonHash     Awaiting = "set_bon_hash"
	AwaitSetNavKey      Awaiting = "set_nav_key"

	AwaitImportSettings Awaiting = "import_settings"

	AwaitAddTemplateName Awaiting = "add_template_name"
	AwaitAddTemplateBody Awaiting = "add_template_body"
	AwaitEditTemplateBody Awaiting = "edit_template_body"
	AwaitSetTemplateMedia Awaiting = "set_template_media"

	AwaitRestoreDB Awaiting = "restore_db"
)

type Session struct {
	Await Awaiting

	// Context for flows
	SelectedChatID int64

	TemplateID string
	TempName   string
}

type App struct {
	cfg config.Config
	db  *db.DB

	bot *tgbotapi.BotAPI

	sources *sources.Manager
	sched   *scheduler.Scheduler

	sessMu sync.Mutex
	sess   map[int64]*Session // by user id

	// Data dir
	dataDir string
	dbPath  string
}

func New(cfg config.Config) (*App, error) {
	dataDir := cfg.DataDir
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "bot.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}

	// Seed from config file
	if err := database.SeedFromConfig(context.Background(), cfg.BonbastAPIUsername, cfg.BonbastAPIHash, cfg.NavasanAPIKey, cfg.InitialAdminIDs); err != nil {
		_ = database.Close()
		return nil, err
	}

	b, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	b.Debug = cfg.Debug

	app := &App{
		cfg: cfg,
		db: database,
		bot: b,
		sources: sources.NewManager(database),
		sess: map[int64]*Session{},
		dataDir: dataDir,
		dbPath: dbPath,
	}

	// Scheduler
	app.sched = scheduler.New(database, app.sources, b, app)
	return app, nil
}

func (a *App) Close() {
	if a.sched != nil {
		a.sched.Stop()
	}
	_ = a.db.Close()
}

func (a *App) Run() error {
	log.Printf("Bot authorized as @%s", a.bot.Self.UserName)

	a.sched.Start()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	// Receive chat member updates for approvals
	u.AllowedUpdates = []string{"message", "callback_query", "my_chat_member", "chat_member"}

	updates := a.bot.GetUpdatesChan(u)

	for upd := range updates {
		a.handleUpdate(upd)
	}
	return nil
}

// Notifier interface for scheduler
func (a *App) NotifyAdmins(ctx context.Context, text string, kb *tgbotapi.InlineKeyboardMarkup) {
	admins, err := a.db.ListAdmins(ctx)
	if err != nil {
		return
	}
	for _, ad := range admins {
		msg := tgbotapi.NewMessage(ad.UserID, text)
		if kb != nil {
			msg.ReplyMarkup = kb
		}
		_, _ = a.bot.Send(msg)
	}
}

func (a *App) handleUpdate(upd tgbotapi.Update) {
	if upd.MyChatMember != nil {
		a.handleMyChatMember(*upd.MyChatMember)
		return
	}
	if upd.Message != nil {
		a.handleMessage(*upd.Message)
		return
	}
	if upd.CallbackQuery != nil {
		a.handleCallback(*upd.CallbackQuery)
		return
	}
}

func (a *App) ensureSession(userID int64) *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	s, ok := a.sess[userID]
	if !ok {
		s = &Session{}
		a.sess[userID] = s
	}
	return s
}

func (a *App) clearAwait(userID int64) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	if s, ok := a.sess[userID]; ok {
		s.Await = AwaitNone
		s.TemplateID = ""
		s.TempName = ""
	}
}

func (a *App) handleMyChatMember(m tgbotapi.ChatMemberUpdated) {
	// Detect bot being added to a chat
	if m.NewChatMember.User == nil || m.NewChatMember.User.ID != a.bot.Self.ID {
		return
	}
	newStatus := m.NewChatMember.Status
	oldStatus := m.OldChatMember.Status

	added := (oldStatus == "left" || oldStatus == "kicked") && (newStatus == "member" || newStatus == "administrator")
	if !added {
		return
	}

	chat := m.Chat
	title := chat.Title
	if title == "" {
		title = chat.UserName
	}
	typ := chat.Type

	_ = a.db.UpsertChat(context.Background(), chat.ID, title, typ)

	// Try to notify in the chat itself
	chatMsg := "✅ ربات اضافه شد.\n\n⏳ این چت هنوز تایید نشده.\nادمین ربات در پیام خصوصی می‌تونه تایید/رد کنه.\n\n(پیکربندی فقط از طریق چت خصوصی با ربات انجام می‌شود.)"
	_, _ = a.bot.Send(tgbotapi.NewMessage(chat.ID, chatMsg))

	// Notify bot admins in private
	ctx := context.Background()
	admins, err := a.db.ListAdmins(ctx)
	if err != nil || len(admins) == 0 {
		// No admins yet: instruct in chat.
		noAdminMsg := "⚠️ هنوز هیچ ادمینی برای ربات تعریف نشده.\nاولین کسی که در پیام خصوصی با ربات صحبت کند، ادمین اصلی می‌شود و سپس می‌تواند این چت را تایید کند."
		_, _ = a.bot.Send(tgbotapi.NewMessage(chat.ID, noAdminMsg))
		return
	}

	fromStr := ""
	if m.From != nil {
		fromStr = fmt.Sprintf("\nاضافه‌کننده: %s (%d)", displayName(*m.From), m.From.ID)
	}

	text := fmt.Sprintf("🆕 ربات به یک %s اضافه شد و نیاز به تایید دارد:\n\nعنوان: %s\nChat ID: %d%s",
		typ, title, chat.ID, fromStr)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ تایید", fmt.Sprintf("approve|%d", chat.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ رد", fmt.Sprintf("deny|%d", chat.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ تنظیمات", fmt.Sprintf("chat|%d", chat.ID)),
		),
	)

	for _, ad := range admins {
		msg := tgbotapi.NewMessage(ad.UserID, text)
		msg.ReplyMarkup = kb
		_, _ = a.bot.Send(msg)
	}
}

func (a *App) handleMessage(msg tgbotapi.Message) {
	// Only handle private chat for configuration; ignore group chatter.
	if msg.Chat == nil {
		return
	}
	if msg.Chat.Type != "private" {
		// Detect bot being added via message.new_chat_members too (some clients)
		if len(msg.NewChatMembers) > 0 {
			for _, u := range msg.NewChatMembers {
				if u.ID == a.bot.Self.ID {
					_ = a.db.UpsertChat(context.Background(), msg.Chat.ID, msg.Chat.Title, msg.Chat.Type)
				}
			}
		}
		return
	}

	userID := int64(msg.From.ID)

	// If no admins exist, first user becomes super admin (as requested).
	ctx := context.Background()
	adminCount, err := a.db.AdminCount(ctx)
	if err == nil && adminCount == 0 {
		_ = a.db.AddAdmin(ctx, userID, true)
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ شما به عنوان ادمین اصلی ثبت شدید (چون در نصب هیچ User ID‌ای وارد نشده بود)."))
	}

	isAdmin, isSuper, _ := a.db.IsAdmin(ctx, userID)
	if !isAdmin {
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "⛔️ دسترسی ندارید. لطفاً از ادمین اصلی بخواهید شما را به لیست ادمین‌ها اضافه کند."))
		return
	}

	sess := a.ensureSession(userID)

	// Awaiting flows
	switch sess.Await {
	case AwaitAddAdmin:
		a.onAddAdminMessage(ctx, msg, isSuper)
		return
	case AwaitSetBonUser:
		_ = a.db.SetGlobalSetting(ctx, "bonbast_api_username", strings.TrimSpace(msg.Text))
		a.clearAwait(userID)
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ نام‌کاربری Bonbast API ذخیره شد."))
		a.sendGlobalSourceMenu(userID, msg.MessageID)
		return
	case AwaitSetBonHash:
		_ = a.db.SetGlobalSetting(ctx, "bonbast_api_hash", strings.TrimSpace(msg.Text))
		a.clearAwait(userID)
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ Hash Bonbast API ذخیره شد."))
		a.sendGlobalSourceMenu(userID, msg.MessageID)
		return
	case AwaitSetNavKey:
		_ = a.db.SetGlobalSetting(ctx, "navasan_api_key", strings.TrimSpace(msg.Text))
		a.clearAwait(userID)
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ کلید Navasan API ذخیره شد."))
		a.sendGlobalSourceMenu(userID, msg.MessageID)
		return
	case AwaitImportSettings:
		// Import settings JSON for selected chat
		if sess.SelectedChatID == 0 {
			a.clearAwait(userID)
			return
		}
		data := []byte(msg.Text)
		err := a.db.ImportChatSettings(ctx, sess.SelectedChatID, data)
		a.clearAwait(userID)
		if err != nil {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ خطا در Import: "+err.Error()))
		} else {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ تنظیمات وارد شد."))
		}
		a.sendChatMenu(userID, msg.MessageID, sess.SelectedChatID)
		return
	case AwaitAddTemplateName:
		sess.TempName = strings.TrimSpace(msg.Text)
		if sess.TempName == "" {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "نام قالب خالی است. لطفاً یک نام بفرستید."))
			return
		}
		sess.Await = AwaitAddTemplateBody
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "حالا متن قالب را بفرستید.\n\nمی‌توانید از این جایگزین‌ها استفاده کنید:\n{CURRENCIES}\n{COINS}\n{GOLD}\n{DATETIME}\n{DATE}\n{TIME}"))
		return
	case AwaitAddTemplateBody:
		body := msg.Text
		if strings.TrimSpace(body) == "" {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "متن قالب خالی است. لطفاً متن را بفرستید."))
			return
		}
		name := sess.TempName
		sess.TempName = ""
		sess.Await = AwaitNone
		tmpl, err := a.db.CreateTemplate(ctx, name, "قالب سفارشی", body, userID)
		if err != nil {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ ساخت قالب ناموفق: "+err.Error()))
			return
		}
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ قالب ساخته شد: "+tmpl.Name))
		a.sendTemplatesMenu(userID, msg.MessageID, sess.SelectedChatID)
		return
	case AwaitEditTemplateBody:
		if sess.TemplateID == "" {
			a.clearAwait(userID)
			return
		}
		body := msg.Text
		if strings.TrimSpace(body) == "" {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "متن قالب خالی است. لطفاً متن را بفرستید."))
			return
		}
		err := a.db.UpdateTemplateBody(ctx, sess.TemplateID, body)
		a.clearAwait(userID)
		if err != nil {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ ویرایش ناموفق: "+err.Error()))
		} else {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ متن قالب ذخیره شد."))
		}
		a.sendTemplatesMenu(userID, msg.MessageID, sess.SelectedChatID)
		return
	case AwaitSetTemplateMedia:
		if sess.TemplateID == "" {
			a.clearAwait(userID)
			return
		}
		mediaType := ""
		fileID := ""
		if msg.Photo != nil && len(*msg.Photo) > 0 {
			ph := (*msg.Photo)[len(*msg.Photo)-1]
			mediaType = "photo"
			fileID = ph.FileID
		} else if msg.Video != nil {
			mediaType = "video"
			fileID = msg.Video.FileID
		} else if msg.Document != nil {
			// allow sending as file (we'll treat as photo if it's an image)
			mediaType = "document"
			fileID = msg.Document.FileID
		}
		if fileID == "" {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "لطفاً یک عکس یا ویدیو ارسال کنید."))
			return
		}
		// Only photo/video supported for posting. If document, try as photo.
		if mediaType == "document" {
			mediaType = "photo"
		}
		err := a.db.SetTemplateMedia(ctx, sess.TemplateID, mediaType, fileID)
		a.clearAwait(userID)
		if err != nil {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ ذخیره مدیا ناموفق: "+err.Error()))
		} else {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ مدیا به قالب متصل شد."))
		}
		a.sendTemplatesMenu(userID, msg.MessageID, sess.SelectedChatID)
		return
	case AwaitRestoreDB:
		// Accept a document as DB file
		if msg.Document == nil {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "لطفاً فایل بکاپ دیتابیس را به صورت Document ارسال کنید."))
			return
		}
		err := a.restoreDBFromTelegram(ctx, userID, *msg.Document)
		a.clearAwait(userID)
		if err != nil {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ Restore ناموفق: "+err.Error()))
		} else {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "✅ Restore انجام شد. اگر مشکلی دیدید، سرویس را ری‌استارت کنید."))
		}
		a.sendMainMenu(userID, msg.MessageID)
		return
	}

	// Default: show main menu
	a.sendMainMenu(userID, msg.MessageID)
}

func (a *App) onAddAdminMessage(ctx context.Context, msg tgbotapi.Message, isSuper bool) {
	userID := int64(msg.From.ID)
	if !isSuper {
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "⛔️ فقط ادمین اصلی می‌تواند ادمین جدید اضافه کند."))
		a.clearAwait(userID)
		return
	}
	// Option 1: forwarded message
	var newID int64 = 0
	if msg.ForwardFrom != nil {
		newID = int64(msg.ForwardFrom.ID)
	} else {
		// Option 2: typed numeric ID
		txt := strings.TrimSpace(msg.Text)
		id, err := strconv.ParseInt(txt, 10, 64)
		if err == nil {
			newID = id
		}
	}
	if newID == 0 {
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "لطفاً پیام را Forward کنید یا User ID عددی را ارسال کنید."))
		return
	}
	_ = a.db.AddAdmin(ctx, newID, false)
	a.clearAwait(userID)
	_, _ = a.bot.Send(tgbotapi.NewMessage(userID, fmt.Sprintf("✅ ادمین جدید اضافه شد: %d", newID)))
	a.sendAdminsMenu(userID, msg.MessageID)
}

func displayName(u tgbotapi.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.UserName
	}
	if name == "" {
		name = strconv.Itoa(u.ID)
	}
	return name
}

func (a *App) handleCallback(q tgbotapi.CallbackQuery) {
	// Always answer callback to remove spinner
	cb := tgbotapi.NewCallback(q.ID, "")
	_, _ = a.bot.Request(cb)

	userID := int64(q.From.ID)
	ctx := context.Background()

	isAdmin, isSuper, _ := a.db.IsAdmin(ctx, userID)
	if !isAdmin {
		// ignore
		return
	}

	data := q.Data
	parts := strings.Split(data, "|")
	switch parts[0] {
	case "main":
		a.sendMainMenu(userID, q.Message.MessageID)
	case "chats":
		page := 0
		if len(parts) > 1 {
			page, _ = strconv.Atoi(parts[1])
		}
		a.sendChatsMenu(userID, q.Message.MessageID, page)
	case "chat":
		if len(parts) < 2 {
			return
		}
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.ensureSession(userID).SelectedChatID = chatID
		a.sendChatMenu(userID, q.Message.MessageID, chatID)
	case "approve":
		if len(parts) < 2 {
			return
		}
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = a.db.SetChatApproved(ctx, chatID, true)
		_ = a.db.SetChatEnabled(ctx, chatID, true)
		_, _ = a.bot.Send(tgbotapi.NewMessage(chatID, "✅ این چت تایید شد. بروزرسانی‌ها طبق تنظیمات شروع می‌شود."))
		a.sendChatMenu(userID, q.Message.MessageID, chatID)
	case "deny":
		if len(parts) < 2 {
			return
		}
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = a.db.SetChatApproved(ctx, chatID, false)
		_ = a.db.SetChatEnabled(ctx, chatID, false)
		_, _ = a.bot.Send(tgbotapi.NewMessage(chatID, "❌ این چت رد شد و بروزرسانی‌ها متوقف است."))
		a.sendChatsMenu(userID, q.Message.MessageID, 0)
	case "toggle_en":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		ch, err := a.db.GetChat(ctx, chatID)
		if err != nil {
			return
		}
		_ = a.db.SetChatEnabled(ctx, chatID, !ch.Enabled)
		a.sendChatMenu(userID, q.Message.MessageID, chatID)
	case "src":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendSourceMenu(userID, q.Message.MessageID, chatID)
	case "srcset":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		prov := parts[2]
		_ = a.db.UpdateChatSetting(ctx, chatID, "source_provider", prov)
		a.sendSourceMenu(userID, q.Message.MessageID, chatID)
	case "methodset":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		method := parts[2]
		_ = a.db.UpdateChatSetting(ctx, chatID, "source_method", method)
		a.sendSourceMenu(userID, q.Message.MessageID, chatID)
	case "items":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendItemsCategoryMenu(userID, q.Message.MessageID, chatID)
	case "icat":
		// icat|chatID|category|page
		if len(parts) < 4 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		cat := parts[2]
		page, _ := strconv.Atoi(parts[3])
		a.sendItemsListMenu(userID, q.Message.MessageID, chatID, cat, page)
	case "itoggle":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		itemID := parts[2]
		_, _ = a.db.ToggleChatItem(ctx, chatID, itemID)
		// refresh same list page by recomputing from message text? easiest go back to category selection
		a.sendItemsCategoryMenu(userID, q.Message.MessageID, chatID)
	case "order":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendOrderMenu(userID, q.Message.MessageID, chatID)
	case "ord":
		// ord|chatID|up/down|itemID
		if len(parts) < 4 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		dir := parts[2]
		itemID := parts[3]
		_ = a.db.MoveChatItem(ctx, chatID, itemID, dir)
		a.sendOrderMenu(userID, q.Message.MessageID, chatID)
	case "price":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendPriceMenu(userID, q.Message.MessageID, chatID)
	case "priceset":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		mode := parts[2]
		_ = a.db.UpdateChatSetting(ctx, chatID, "price_mode", mode)
		a.sendPriceMenu(userID, q.Message.MessageID, chatID)
	case "postmode":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendPostModeMenu(userID, q.Message.MessageID, chatID)
	case "postset":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		mode := parts[2]
		_ = a.db.UpdateChatSetting(ctx, chatID, "post_mode", mode)
		a.sendPostModeMenu(userID, q.Message.MessageID, chatID)
	case "digits":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendDigitsMenu(userID, q.Message.MessageID, chatID)
	case "digitsset":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		mode := parts[2]
		_ = a.db.UpdateChatSetting(ctx, chatID, "digits", mode)
		a.sendDigitsMenu(userID, q.Message.MessageID, chatID)
	
	case "same":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		st, _ := a.db.GetChatSettings(ctx, chatID)
		_ = a.db.UpdateChatSetting(ctx, chatID, "show_same_arrow", !st.ShowSameArrow)
		a.sendChatMenu(userID, q.Message.MessageID, chatID)

case "interval":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendIntervalMenu(userID, q.Message.MessageID, chatID)
	case "intval":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		mins, _ := strconv.Atoi(parts[2])
		_ = a.db.UpdateChatSetting(ctx, chatID, "interval_minutes", mins)
		a.sendIntervalMenu(userID, q.Message.MessageID, chatID)
	case "downtime":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendDowntimeMenu(userID, q.Message.MessageID, chatID)
	case "dton":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		st, _ := a.db.GetChatSettings(ctx, chatID)
		_ = a.db.UpdateChatSetting(ctx, chatID, "downtime_enabled", !st.DowntimeEnabled)
		a.sendDowntimeMenu(userID, q.Message.MessageID, chatID)
	case "dtadj":
		// dtadj|chatID|start/end|deltaMinutes
		if len(parts) < 4 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		which := parts[2]
		delta, _ := strconv.Atoi(parts[3])
		st, _ := a.db.GetChatSettings(ctx, chatID)
		cur := st.DowntimeStart
		if which == "end" {
			cur = st.DowntimeEnd
		}
		m, ok := utils.ParseHHMM(cur)
		if !ok {
			m = 0
		}
		m = (m + delta) % 1440
		if m < 0 { m += 1440 }
		newVal := utils.FormatHHMM(m)
		if which == "end" {
			_ = a.db.UpdateChatSetting(ctx, chatID, "downtime_end", newVal)
		} else {
			_ = a.db.UpdateChatSetting(ctx, chatID, "downtime_start", newVal)
		}
		a.sendDowntimeMenu(userID, q.Message.MessageID, chatID)
	case "trig":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendTriggerMenu(userID, q.Message.MessageID, chatID)
	case "trigtog":
		// trigtog|chatID|itemID
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		itemID := parts[2]
		st, _ := a.db.GetChatSettings(ctx, chatID)
		st.TriggerItems = toggleInList(st.TriggerItems, itemID)
		_ = a.db.UpdateChatSetting(ctx, chatID, "trigger_items", st.TriggerItems)
		a.sendTriggerMenu(userID, q.Message.MessageID, chatID)
	case "thtype":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		st, _ := a.db.GetChatSettings(ctx, chatID)
		next := "abs"
		if st.TriggerThresholdType == "abs" {
			next = "pct"
		}
		_ = a.db.UpdateChatSetting(ctx, chatID, "trigger_threshold_type", next)
		a.sendThresholdMenu(userID, q.Message.MessageID, chatID)
	case "thadj":
		// thadj|chatID|delta
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		delta, _ := strconv.ParseFloat(parts[2], 64)
		st, _ := a.db.GetChatSettings(ctx, chatID)
		val := st.TriggerThresholdValue + delta
		if val < 0 { val = 0 }
		_ = a.db.UpdateChatSetting(ctx, chatID, "trigger_threshold_value", val)
		a.sendThresholdMenu(userID, q.Message.MessageID, chatID)
	case "threshold":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendThresholdMenu(userID, q.Message.MessageID, chatID)
	case "tmpl":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendTemplatesMenu(userID, q.Message.MessageID, chatID)
	case "tmplset":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		tid := parts[2]
		_ = a.db.SetChatTemplate(ctx, chatID, tid)
		a.sendTemplatesMenu(userID, q.Message.MessageID, chatID)
	case "tmplprev":
		// tmplprev|chatID|templateID
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		tid := parts[2]
		a.previewTemplate(userID, chatID, tid)
	case "tmpladd":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		s := a.ensureSession(userID)
		s.SelectedChatID = chatID
		s.Await = AwaitAddTemplateName
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "نام قالب جدید را بفرستید."))
	case "tmpledit":
		// tmpledit|chatID|templateID
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		tid := parts[2]
		s := a.ensureSession(userID)
		s.SelectedChatID = chatID
		s.TemplateID = tid
		s.Await = AwaitEditTemplateBody
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "متن جدید قالب را بفرستید.\n\nجایگزین‌ها: {CURRENCIES} {COINS} {GOLD} {DATETIME}"))
	case "tmplmedia":
		// tmplmedia|chatID|templateID
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		tid := parts[2]
		s := a.ensureSession(userID)
		s.SelectedChatID = chatID
		s.TemplateID = tid
		s.Await = AwaitSetTemplateMedia
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "لطفاً یک عکس یا ویدیو برای این قالب ارسال کنید (Caption لازم نیست)."))
	case "tmplclear":
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		tid := parts[2]
		_ = a.db.ClearTemplateMedia(ctx, tid)
		a.sendTemplatesMenu(userID, q.Message.MessageID, chatID)
	case "sendnow":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "⏳ در حال ارسال بروزرسانی..."))
		a.sched.PostNow(chatID)
		a.sendChatMenu(userID, q.Message.MessageID, chatID)
	case "export":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.exportSettings(userID, chatID)
	case "import":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		s := a.ensureSession(userID)
		s.SelectedChatID = chatID
		s.Await = AwaitImportSettings
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "تنظیمات را به صورت JSON در همین چت ارسال کنید (Paste)."))
	case "status":
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		a.sendStatusMenu(userID, q.Message.MessageID, chatID)
	case "swsrc":
		// Quick switch from scheduler notification: swsrc|chatID|provider
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		prov := parts[2]
		_ = a.db.UpdateChatSetting(ctx, chatID, "source_provider", prov)
		a.sendChatMenu(userID, q.Message.MessageID, chatID)
	case "admins":
		if !isSuper {
			_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "⛔️ فقط ادمین اصلی می‌تواند لیست ادمین‌ها را مدیریت کند."))
			return
		}
		a.sendAdminsMenu(userID, q.Message.MessageID)
	case "adminadd":
		if !isSuper { return }
		s := a.ensureSession(userID)
		s.Await = AwaitAddAdmin
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "پیام یک کاربر را Forward کنید یا User ID عددی او را بفرستید."))
	case "adminrm":
		if !isSuper { return }
		if len(parts) < 2 { return }
		rmID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = a.db.RemoveAdmin(ctx, rmID)
		a.sendAdminsMenu(userID, q.Message.MessageID)
	case "globalsrc":
		a.sendGlobalSourceMenu(userID, q.Message.MessageID)
	case "setbonuser":
		s := a.ensureSession(userID)
		s.Await = AwaitSetBonUser
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "نام‌کاربری Bonbast API را ارسال کنید (برای روش API)."))
	case "setbonhash":
		s := a.ensureSession(userID)
		s.Await = AwaitSetBonHash
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "Hash / Secret Bonbast API را ارسال کنید (برای روش API)."))
	case "setnavkey":
		s := a.ensureSession(userID)
		s.Await = AwaitSetNavKey
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "کلید Navasan API را ارسال کنید (برای روش API)."))
	case "backup":
		a.sendBackupMenu(userID, q.Message.MessageID)
	case "dbbackup":
		a.sendDBBackup(userID)
	case "dbrestore":
		s := a.ensureSession(userID)
		s.Await = AwaitRestoreDB
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "فایل بکاپ دیتابیس (bot.db) را به صورت Document ارسال کنید."))
	case "help":
		a.sendHelp(userID, q.Message.MessageID)

	case "dtpreset":
		// dtpreset|chatID|night
		if len(parts) < 3 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		preset := parts[2]
		if preset == "night" {
			_ = a.db.UpdateChatSetting(ctx, chatID, "downtime_start", "20:00")
			_ = a.db.UpdateChatSetting(ctx, chatID, "downtime_end", "10:00")
			_ = a.db.UpdateChatSetting(ctx, chatID, "downtime_enabled", true)
		}
		a.sendDowntimeMenu(userID, q.Message.MessageID, chatID)
	case "trigclear":
		if len(parts) < 2 { return }
		chatID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = a.db.UpdateChatSetting(ctx, chatID, "trigger_items", []string{})
		a.sendTriggerMenu(userID, q.Message.MessageID, chatID)
	case "noop":
		// no-op (used for label buttons)
		return

	default:
		// unknown
	}
}

func toggleInList(list []string, item string) []string {
	for i, v := range list {
		if v == item {
			return append(list[:i], list[i+1:]...)
		}
	}
	return append(list, item)
}

func (a *App) sendMainMenu(userID int64, msgID int) {
	text := "⚙️ پنل مدیریت ربات نرخ ارز\n\nهمه چیز با دکمه‌ها (Inline) کنترل می‌شود.\n\nیکی را انتخاب کنید:"
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📣 چت‌ها / کانال‌ها", "chats|0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧩 منبع داده (API/اسکرپ)", "globalsrc"),
			tgbotapi.NewInlineKeyboardButtonData("🛟 بکاپ/ریستور", "backup"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 مدیریت ادمین‌ها", "admins"),
			tgbotapi.NewInlineKeyboardButtonData("❓ راهنما", "help"),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendChatsMenu(userID int64, msgID int, page int) {
	ctx := context.Background()
	chats, err := a.db.ListChats(ctx)
	if err != nil {
		return
	}
	const pageSize = 8
	start := page * pageSize
	if start < 0 { start = 0 }
	if start > len(chats) { start = len(chats) }
	end := start + pageSize
	if end > len(chats) { end = len(chats) }

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range chats[start:end] {
		prefix := "⏳"
		if c.Approved {
			prefix = "✅"
		}
		icon := "👥"
		if c.Type == "channel" {
			icon = "📢"
		}
		label := fmt.Sprintf("%s %s %s", prefix, icon, truncate(c.Title, 26))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("chat|%d", c.ChatID)),
		))
	}

	navRow := []tgbotapi.InlineKeyboardButton{}
	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ قبلی", fmt.Sprintf("chats|%d", page-1)))
	}
	if end < len(chats) {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("بعدی ➡️", fmt.Sprintf("chats|%d", page+1)))
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "main"),
	))

	text := "📣 لیست چت‌ها/کانال‌هایی که ربات در آن‌ها عضو است:\n(⏳ یعنی هنوز تایید نشده)"
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

func (a *App) sendChatMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	ch, err := a.db.GetChat(ctx, chatID)
	if err != nil {
		return
	}
	st, err := a.db.GetChatSettings(ctx, chatID)
	if err != nil {
		return
	}

	status := "⏳ در انتظار تایید"
	if ch.Approved {
		status = "✅ تایید شده"
	}
	en := "✅ روشن"
	if !ch.Enabled {
		en = "⛔️ خاموش"
	}

	text := fmt.Sprintf("⚙️ تنظیمات چت\n\nعنوان: %s\nChat ID: %d\nنوع: %s\nوضعیت: %s\nفعال: %s\n\nمنبع: %s (%s)\nبازه: هر %d دقیقه (مرزبندی تهران)\nDowntime: %v (%s تا %s)\nTrigger: %d مورد | Threshold: %s %.2f\nقیمت: %s\nارسال: %s\nDigits: %s\nقالب: %s",
		ch.Title, ch.ChatID, ch.Type, status, en,
		st.SourceProvider, st.SourceMethod,
		st.IntervalMinutes,
		st.DowntimeEnabled, st.DowntimeStart, st.DowntimeEnd,
		len(st.TriggerItems), st.TriggerThresholdType, st.TriggerThresholdValue,
		st.PriceMode,
		st.PostMode,
		st.Digits,
		st.ShowSameArrow,
		st.TemplateID,
	)

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ تایید", fmt.Sprintf("approve|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ رد", fmt.Sprintf("deny|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔌 روشن/خاموش", fmt.Sprintf("toggle_en|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("🧰 وضعیت", fmt.Sprintf("status|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧩 منبع", fmt.Sprintf("src|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("💱 اقلام و ترتیب", fmt.Sprintf("items|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🕒 بازه", fmt.Sprintf("interval|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("🌙 downtime", fmt.Sprintf("downtime|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎯 Trigger", fmt.Sprintf("trig|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("📏 Threshold", fmt.Sprintf("threshold|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💰 قیمت (Sell/Buy)", fmt.Sprintf("price|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("✉️ نوع ارسال", fmt.Sprintf("postmode|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("▬/▲ نمایش حالت بدون تغییر", fmt.Sprintf("same|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔢 Digits", fmt.Sprintf("digits|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("🧾 قالب‌ها + Preview", fmt.Sprintf("tmpl|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 ارسال الآن", fmt.Sprintf("sendnow|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("📤 Export", fmt.Sprintf("export|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("📥 Import", fmt.Sprintf("import|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "chats|0"),
		),
	}

	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendSourceMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)

	// Check which APIs are configured
	bonUser, _, _ := a.db.GetGlobalSetting(ctx, "bonbast_api_username")
	bonHash, _, _ := a.db.GetGlobalSetting(ctx, "bonbast_api_hash")
	navKey, _, _ := a.db.GetGlobalSetting(ctx, "navasan_api_key")

	bonAPIok := bonUser != "" && bonHash != ""
	navAPIok := navKey != ""

	text := fmt.Sprintf("🧩 منبع داده برای این چت\n\nProvider: %s\nMethod: %s\n\nBonbast API: %v\nNavasan API: %v\n\nنکته: روش API پایدارتر است، روش اسکرپ بدون کلید است ولی ممکن است تغییر کند.", st.SourceProvider, st.SourceMethod, bonAPIok, navAPIok)

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Bonbast", fmt.Sprintf("srcset|%d|bonbast", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("Navasan", fmt.Sprintf("srcset|%d|navasan", chatID)),
		),
	}
	methodRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("API", fmt.Sprintf("methodset|%d|api", chatID)),
		tgbotapi.NewInlineKeyboardButtonData("Scrape", fmt.Sprintf("methodset|%d|scrape", chatID)),
	}
	rows = append(rows, methodRow)
	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧰 تنظیم کلیدهای API", "globalsrc"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendItemsCategoryMenu(userID int64, msgID int, chatID int64) {
	text := "💱 انتخاب اقلام و ترتیب\n\n۱) ابتدا اقلام را انتخاب کنید\n۲) سپس ترتیب را تنظیم کنید"
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅/⬜️ انتخاب ارزها", fmt.Sprintf("icat|%d|currency|0", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅/⬜️ انتخاب سکه‌ها", fmt.Sprintf("icat|%d|coin|0", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("✅/⬜️ طلا/کریپتو", fmt.Sprintf("icat|%d|gold|0", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔀 ترتیب (Order)", fmt.Sprintf("order|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendItemsListMenu(userID int64, msgID int, chatID int64, category string, page int) {
	ctx := context.Background()
	chatItems, _ := a.db.ListChatItems(ctx, chatID)
	enabled := map[string]bool{}
	for _, ci := range chatItems {
		enabled[ci.ItemID] = ci.Enabled
	}

	var list []items.Item
	for _, it := range items.All {
		switch category {
		case "currency":
			if it.Category != items.CategoryCurrency { continue }
		case "coin":
			if it.Category != items.CategoryCoin { continue }
		case "gold":
			if it.Category != items.CategoryGold && it.Category != items.CategoryCrypto { continue }
		}
		list = append(list, it)
	}

	const pageSize = 10
	start := page * pageSize
	if start < 0 { start = 0 }
	if start > len(list) { start = len(list) }
	end := start + pageSize
	if end > len(list) { end = len(list) }

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, it := range list[start:end] {
		mark := "⬜️"
		if enabled[it.ID] {
			mark = "✅"
		}
		label := fmt.Sprintf("%s %s %s", mark, it.Emoji, truncate(it.NameFa, 20))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("itoggle|%d|%s", chatID, it.ID)),
		))
	}

	nav := []tgbotapi.InlineKeyboardButton{}
	if page > 0 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("⬅️ قبلی", fmt.Sprintf("icat|%d|%s|%d", chatID, category, page-1)))
	}
	if end < len(list) {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("بعدی ➡️", fmt.Sprintf("icat|%d|%s|%d", chatID, category, page+1)))
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("items|%d", chatID)),
	))

	text := "✅/⬜️ انتخاب موارد (" + category + ")"
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendOrderMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	ids, _ := a.db.EnabledItemIDs(ctx, chatID)

	// show up to 20 items per page (enough for most)
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, id := range ids {
		it, ok := items.ByID(id)
		if !ok { continue }
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬆️", fmt.Sprintf("ord|%d|up|%s", chatID, id)),
			tgbotapi.NewInlineKeyboardButtonData(it.Emoji+" "+truncate(it.NameFa, 18), fmt.Sprintf("noop|%s", id)),
			tgbotapi.NewInlineKeyboardButtonData("⬇️", fmt.Sprintf("ord|%d|down|%s", chatID, id)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("items|%d", chatID)),
	))
	text := "🔀 ترتیب نمایش (بالا/پایین)"
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendPriceMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)
	text := fmt.Sprintf("💰 نوع قیمت\n\nحالت فعلی: %s\n\nSell: فروش\nBuy: خرید\nBoth: هر دو", st.PriceMode)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Sell", fmt.Sprintf("priceset|%d|sell", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("Buy", fmt.Sprintf("priceset|%d|buy", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("Both", fmt.Sprintf("priceset|%d|both", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendPostModeMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)
	text := fmt.Sprintf("✉️ نوع ارسال\n\nحالت فعلی: %s\n\nNew: پیام جدید هر بار\nEdit: ادیت پیام قبلی (کم‌اسپم)", st.PostMode)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("New message", fmt.Sprintf("postset|%d|new", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("Edit latest", fmt.Sprintf("postset|%d|edit", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendDigitsMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)
	text := fmt.Sprintf("🔢 نمایش اعداد\n\nحالت فعلی: %s", st.Digits)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("English 0-9", fmt.Sprintf("digitsset|%d|en", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("Persian ۰-۹", fmt.Sprintf("digitsset|%d|fa", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendIntervalMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)
	text := fmt.Sprintf("🕒 بازه بروزرسانی\n\nحالت فعلی: هر %d دقیقه\n\nزمان‌بندی روی مرزبندی تهران است (مثلاً 10:00، 10:05، ...).", st.IntervalMinutes)
	presets := []int{1,2,3,5,10,15,30,60,120}
	var rows [][]tgbotapi.InlineKeyboardButton
	row := []tgbotapi.InlineKeyboardButton{}
	for i, p := range presets {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%dm", p), fmt.Sprintf("intval|%d|%d", chatID, p)))
		if (i+1)%4 == 0 {
			rows = append(rows, row)
			row = []tgbotapi.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendDowntimeMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)

	text := fmt.Sprintf("🌙 Downtime (عدم ارسال)\n\nفعال: %v\nاز: %s\nتا: %s\n\nاگر بازه از نیمه‌شب رد شود هم پشتیبانی می‌شود (مثلاً 20:00 تا 10:00).", st.DowntimeEnabled, st.DowntimeStart, st.DowntimeEnd)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("روشن/خاموش", fmt.Sprintf("dton|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Start -1h", fmt.Sprintf("dtadj|%d|start|-60", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("Start +1h", fmt.Sprintf("dtadj|%d|start|60", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("End -1h", fmt.Sprintf("dtadj|%d|end|-60", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("End +1h", fmt.Sprintf("dtadj|%d|end|60", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Preset 20:00-10:00", fmt.Sprintf("dtpreset|%d|night", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	// NOTE: dtpreset handled in callback default? We'll add later.
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendTriggerMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)
	enabledIDs, _ := a.db.EnabledItemIDs(ctx, chatID)


	trigSet := map[string]bool{}
	for _, id := range st.TriggerItems {
		trigSet[id] = true
	}

	// show enabled items only (more relevant)
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, id := range enabledIDs {
		it, ok := items.ByID(id)
		if !ok { continue }
		mark := "⬜️"
		if trigSet[id] {
			mark = "🎯"
		}
		label := fmt.Sprintf("%s %s %s", mark, it.Emoji, truncate(it.NameFa, 18))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("trigtog|%d|%s", chatID, id)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("پاک کردن همه Triggerها", fmt.Sprintf("trigclear|%d", chatID)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
	))
	text := fmt.Sprintf("🎯 Trigger\n\nاگر Triggerها تنظیم شوند، فقط وقتی تغییر کنند پست می‌شود.\nTrigger فعلی: %d مورد", len(st.TriggerItems))
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendThresholdMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)

	unit := "تومان"
	if st.TriggerThresholdType == "pct" {
		unit = "%"
	}
	text := fmt.Sprintf("📏 Threshold\n\nنوع: %s\nمقدار: %.2f %s\n\n(برای Triggerها استفاده می‌شود)", st.TriggerThresholdType, st.TriggerThresholdValue, unit)

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("تغییر نوع (abs/pct)", fmt.Sprintf("thtype|%d", chatID)),
		),
	}
	if st.TriggerThresholdType == "pct" {
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("-0.1", fmt.Sprintf("thadj|%d|-0.1", chatID)),
				tgbotapi.NewInlineKeyboardButtonData("+0.1", fmt.Sprintf("thadj|%d|0.1", chatID)),
				tgbotapi.NewInlineKeyboardButtonData("+1", fmt.Sprintf("thadj|%d|1", chatID)),
			),
		)
	} else {
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("-100", fmt.Sprintf("thadj|%d|-100", chatID)),
				tgbotapi.NewInlineKeyboardButtonData("+100", fmt.Sprintf("thadj|%d|100", chatID)),
				tgbotapi.NewInlineKeyboardButtonData("+1000", fmt.Sprintf("thadj|%d|1000", chatID)),
			),
		)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
	))

	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendTemplatesMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	st, _ := a.db.GetChatSettings(ctx, chatID)
	templates, _ := a.db.ListTemplates(ctx)

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, t := range templates {
		mark := "⬜️"
		if t.TemplateID == st.TemplateID {
			mark = "✅"
		}
		label := mark + " " + truncate(t.Name, 22)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("tmplset|%d|%s", chatID, t.TemplateID)),
			tgbotapi.NewInlineKeyboardButtonData("👁 Preview", fmt.Sprintf("tmplprev|%d|%s", chatID, t.TemplateID)),
		))
		// extra row for media/edit for custom templates
		editBtns := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("✏️ Edit", fmt.Sprintf("tmpledit|%d|%s", chatID, t.TemplateID)),
			tgbotapi.NewInlineKeyboardButtonData("🖼/🎥 Media", fmt.Sprintf("tmplmedia|%d|%s", chatID, t.TemplateID)),
		}
		if t.MediaType != "" {
			editBtns = append(editBtns, tgbotapi.NewInlineKeyboardButtonData("🧹 حذف مدیا", fmt.Sprintf("tmplclear|%d|%s", chatID, t.TemplateID)))
		}
		rows = append(rows, editBtns)
	}

	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ ساخت قالب جدید", fmt.Sprintf("tmpladd|%d", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)

	text := "🧾 قالب‌ها\n\nبا دکمه Preview می‌توانید قبل از ارسال به کانال/گروه ببینید."
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) previewTemplate(userID int64, chatID int64, templateID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	settings, err := a.db.GetChatSettings(ctx, chatID)
	if err != nil {
		return
	}
	tmpl, err := a.db.GetTemplate(ctx, templateID)
	if err != nil {
		return
	}

	enabledIDs, _ := a.db.EnabledItemIDs(ctx, chatID)
	snap, err := a.sources.Get(ctx, sources.Provider(settings.SourceProvider), sources.Method(settings.SourceMethod))
	if err != nil {
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ دریافت دیتا ناموفق: "+err.Error()))
		return
	}
	lastVals, _ := a.db.GetLastValues(ctx, chatID, enabledIDs)

	out := render.BuildMessage(ctx, settings, tmpl, enabledIDs, snap, lastVals)

	// Send preview in private chat (not to channel/group)
	header := "👁 Preview قالب: " + tmpl.Name + "\n(این فقط پیش‌نمایش است و در کانال/گروه پست نمی‌شود.)"
	_, _ = a.bot.Send(tgbotapi.NewMessage(userID, header))

	if out.MediaType != "" && out.MediaFileID != "" {
		if out.MediaType == "video" {
			msg := tgbotapi.NewVideo(userID, tgbotapi.FileID(out.MediaFileID))
			msg.Caption = out.Text
			_, _ = a.bot.Send(msg)
			return
		}
		msg := tgbotapi.NewPhoto(userID, tgbotapi.FileID(out.MediaFileID))
		msg.Caption = out.Text
		_, _ = a.bot.Send(msg)
		return
	}
	_, _ = a.bot.Send(tgbotapi.NewMessage(userID, out.Text))
}

func (a *App) exportSettings(userID int64, chatID int64) {
	ctx := context.Background()
	b, err := a.db.ExportChatSettings(ctx, chatID)
	if err != nil {
		_, _ = a.bot.Send(tgbotapi.NewMessage(userID, "❌ Export ناموفق: "+err.Error()))
		return
	}
	tmp := filepath.Join(a.dataDir, fmt.Sprintf("chat_%d_settings.json", chatID))
	_ = os.WriteFile(tmp, b, 0o600)
	doc := tgbotapi.NewDocument(userID, tgbotapi.FilePath(tmp))
	doc.Caption = "📤 Export تنظیمات"
	_, _ = a.bot.Send(doc)
	_ = os.Remove(tmp)
}

func (a *App) sendStatusMenu(userID int64, msgID int, chatID int64) {
	ctx := context.Background()
	ch, err := a.db.GetChat(ctx, chatID)
	if err != nil {
		return
	}
	st, err := a.db.GetChatSettings(ctx, chatID)
	if err != nil {
		return
	}
	lastFetch := "—"
	if st.LastFetchTime.Valid {
		lastFetch = time.Unix(st.LastFetchTime.Int64, 0).In(utils.TehranLoc()).Format(time.RFC3339)
	}
	lastPost := "—"
	if st.LastPostTime.Valid {
		lastPost = time.Unix(st.LastPostTime.Int64, 0).In(utils.TehranLoc()).Format(time.RFC3339)
	}
	errTxt := "—"
	if st.LastError.Valid {
		errTxt = st.LastError.String
	}
	text := fmt.Sprintf("🧰 Status / Health\n\nچت: %s\nChat ID: %d\nApproved: %v\nEnabled: %v\n\nLast fetch: %s\nLast post: %s\nCurrent source: %s (%s)\nErrors: %s",
		ch.Title, ch.ChatID, ch.Approved, ch.Enabled, lastFetch, lastPost, st.SourceProvider, st.SourceMethod, errTxt)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", fmt.Sprintf("status|%d", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", fmt.Sprintf("chat|%d", chatID)),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendAdminsMenu(userID int64, msgID int) {
	ctx := context.Background()
	admins, _ := a.db.ListAdmins(ctx)

	var b strings.Builder
	b.WriteString("👥 مدیریت ادمین‌ها\n\n")
	for _, ad := range admins {
		tag := ""
		if ad.IsSuper {
			tag = " (super)"
		}
		b.WriteString(fmt.Sprintf("• %d%s\n", ad.UserID, tag))
	}
	text := b.String()

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ افزودن ادمین", "adminadd"),
		),
	}
	for _, ad := range admins {
		if ad.IsSuper {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("❌ حذف %d", ad.UserID), fmt.Sprintf("adminrm|%d", ad.UserID)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "main"),
	))
	kb := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendGlobalSourceMenu(userID int64, msgID int) {
	ctx := context.Background()
	bonUser, _, _ := a.db.GetGlobalSetting(ctx, "bonbast_api_username")
	bonHash, _, _ := a.db.GetGlobalSetting(ctx, "bonbast_api_hash")
	navKey, _, _ := a.db.GetGlobalSetting(ctx, "navasan_api_key")

	text := fmt.Sprintf("🧩 تنظیمات منبع داده (Global)\n\nBonbast API username: %s\nBonbast API hash: %s\nNavasan API key: %s\n\nاگر کلید ندارید، می‌توانید از روش Scrape استفاده کنید.\n\nPros/Cons:\n• API: پایدارتر + کمتر احتمال بلاک، اما نیاز به کلید/هزینه.\n• Scrape: بدون کلید، اما ممکن است تغییر کند یا محدود شود.",
		blankOrValue(bonUser), maskSecret(bonHash), maskSecret(navKey))

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Set Bonbast username", "setbonuser"),
			tgbotapi.NewInlineKeyboardButtonData("Set Bonbast hash", "setbonhash"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Set Navasan key", "setnavkey"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "main"),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func blankOrValue(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	if len(s) <= 6 {
		return "******"
	}
	return s[:3] + "…" + s[len(s)-2:]
}

func (a *App) sendBackupMenu(userID int64, msgID int) {
	text := "🛟 Backup / Restore\n\n• Backup DB: فایل دیتابیس (bot.db) را می‌فرستد.\n• Restore DB: یک فایل bot.db از شما می‌گیرد و جایگزین می‌کند.\n\n(پیشنهاد: قبل از Restore بکاپ بگیرید.)"
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📦 Backup DB", "dbbackup"),
			tgbotapi.NewInlineKeyboardButtonData("♻️ Restore DB", "dbrestore"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "main"),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) sendDBBackup(userID int64) {
	// Create a consistent SQLite snapshot (works with WAL) using VACUUM INTO.
	tmp := filepath.Join(a.dataDir, fmt.Sprintf("backup_%d_bot.db", time.Now().Unix()))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := a.db.BackupTo(ctx, tmp); err != nil {
		// Fallback: best-effort file copy
		_ = copyFile(a.dbPath, tmp)
	}

	doc := tgbotapi.NewDocument(userID, tgbotapi.FilePath(tmp))
	doc.Caption = "📦 Backup DB"
	_, _ = a.bot.Send(doc)
	_ = os.Remove(tmp)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (a *App) restoreDBFromTelegram(ctx context.Context, userID int64, doc tgbotapi.Document) error {
	// Download file from Telegram
	f, err := a.bot.GetFile(tgbotapi.FileConfig{FileID: doc.FileID})
	if err != nil {
		return err
	}
	urlStr := f.Link(a.cfg.BotToken)

	rc, err := httpGetSimple(urlStr)
	if err != nil {
		return err
	}
	defer rc.Close()

	tmp := filepath.Join(a.dataDir, "restore_tmp.db")
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return err
	}
	_ = out.Close()

	// Stop background tasks and close DB before swapping files
	if a.sched != nil {
		a.sched.Stop()
	}
	if a.db != nil {
		_ = a.db.Close()
	}

	// Remove WAL/SHM leftovers (best-effort)
	_ = os.Remove(a.dbPath + "-wal")
	_ = os.Remove(a.dbPath + "-shm")

	backupOld := filepath.Join(a.dataDir, fmt.Sprintf("pre_restore_%d.db", time.Now().Unix()))
	_ = os.Rename(a.dbPath, backupOld)

	if err := os.Rename(tmp, a.dbPath); err != nil {
		// rollback
		_ = os.Rename(backupOld, a.dbPath)
		return err
	}

	newDB, err := db.Open(a.dbPath)
	if err != nil {
		// rollback
		_ = os.Rename(a.dbPath, tmp)
		_ = os.Rename(backupOld, a.dbPath)
		newDB, _ = db.Open(a.dbPath)
		a.db = newDB
		a.sources = sources.NewManager(newDB)
		a.sched = scheduler.New(a.db, a.sources, a.bot, a)
		a.sched.Start()
		return err
	}

	a.db = newDB
	a.sources = sources.NewManager(newDB)
	a.sched = scheduler.New(a.db, a.sources, a.bot, a)
	a.sched.Start()
	return nil
}

func httpGetSimple(urlStr string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp.Body, nil
}

func (a *App) sendHelp(userID int64, msgID int) {
	text := "❓ راهنمای سریع\n\n" +
		"1) ربات را به کانال/گروه اضافه کنید و اگر لازم است ادمینش کنید.\n" +
		"2) ربات در چت پیام می‌دهد: «در انتظار تایید».\n" +
		"3) در همین پنل، روی «چت‌ها/کانال‌ها» بزنید، چت موردنظر را انتخاب کنید و «✅ تایید» را بزنید.\n" +
		"4) تنظیمات فقط از طریق چت خصوصی با ربات است.\n\n" +
		"نکات مهم:\n" +
		"• Interval روی مرزبندی تهران است.\n" +
		"• Trigger اگر فعال باشد، فقط هنگام تغییر آیتم‌های انتخابی پست می‌کند.\n" +
		"• Template Preview پیام را فقط برای شما نشان می‌دهد و در کانال/گروه پست نمی‌کند.\n" +
		"• Source: API پایدارتر است؛ Scrape بدون کلید است اما ممکن است تغییر کند."
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "main"),
		),
	)
	a.editOrSendMenu(userID, msgID, text, kb)
}

func (a *App) editOrSendMenu(userID int64, msgID int, text string, kb tgbotapi.InlineKeyboardMarkup) {
	if msgID != 0 {
		edit := tgbotapi.NewEditMessageText(userID, msgID, text)
		edit.ReplyMarkup = &kb
		edit.DisableWebPagePreview = true
		if _, err := a.bot.Request(edit); err == nil {
			return
		}
	}
	msg := tgbotapi.NewMessage(userID, text)
	msg.ReplyMarkup = kb
	msg.DisableWebPagePreview = true
	_, _ = a.bot.Send(msg)
}
