
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Armin-kho/persian-currency-bot/internal/db"
	"github.com/Armin-kho/persian-currency-bot/internal/render"
	"github.com/Armin-kho/persian-currency-bot/internal/sources"
	"github.com/Armin-kho/persian-currency-bot/internal/utils"
)

type Notifier interface {
	NotifyAdmins(ctx context.Context, text string, kb *tgbotapi.InlineKeyboardMarkup)
}

type Scheduler struct {
	db   *db.DB
	src  *sources.Manager
	bot  *tgbotapi.BotAPI
	notify Notifier

	stopCh chan struct{}
	wg     sync.WaitGroup

	// throttling notifications per chat
	mu sync.Mutex
	lastFailNotify map[int64]time.Time
}

func New(database *db.DB, src *sources.Manager, bot *tgbotapi.BotAPI, notifier Notifier) *Scheduler {
	return &Scheduler{
		db: database,
		src: src,
		bot: bot,
		notify: notifier,
		stopCh: make(chan struct{}),
		lastFailNotify: map[int64]time.Time{},
	}
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop()
	}()
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Scheduler) loop() {
	for {
		// Sleep until the next minute boundary in Tehran time.
		now := utils.NowTehran()
		next := now.Truncate(time.Minute).Add(time.Minute)
		wait := time.Until(next)
		select {
		case <-time.After(wait):
			// tick
		case <-s.stopCh:
			return
		}
		s.runTick()
	}
}

func (s *Scheduler) runTick() {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()

	now := utils.NowTehran()
	minuteOfDay := now.Hour()*60 + now.Minute()

	chats, err := s.db.ListChats(ctx)
	if err != nil {
		log.Printf("[scheduler] list chats: %v", err)
		return
	}

	sem := make(chan struct{}, 5) // limit concurrency
	var wg sync.WaitGroup

	for _, c := range chats {
		if !c.Approved || !c.Enabled {
			continue
		}
		settings, err := s.db.GetChatSettings(ctx, c.ChatID)
		if err != nil {
			continue
		}

		// Downtime check
		if settings.DowntimeEnabled {
			start, ok1 := utils.ParseHHMM(settings.DowntimeStart)
			end, ok2 := utils.ParseHHMM(settings.DowntimeEnd)
			if ok1 && ok2 && utils.InDowntime(minuteOfDay, start, end) {
				continue
			}
		}

		interval := settings.IntervalMinutes
		if interval <= 0 {
			interval = 5
		}
		if minuteOfDay%interval != 0 {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(chatID int64, st db.ChatSettings) {
			defer wg.Done()
			defer func() { <-sem }()
			_ = s.postOnce(context.Background(), chatID, st, false)
		}(c.ChatID, settings)
	}
	wg.Wait()
}

func (s *Scheduler) PostNow(chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	settings, err := s.db.GetChatSettings(ctx, chatID)
	if err != nil {
		return
	}
	_ = s.postOnce(ctx, chatID, settings, true)
}

// postOnce fetches, decides trigger gating, then posts/edits.
func (s *Scheduler) postOnce(ctx context.Context, chatID int64, settings db.ChatSettings, forced bool) error {
	enabledIDs, err := s.db.EnabledItemIDs(ctx, chatID)
	if err != nil {
		return err
	}

	snap, err := s.src.Get(ctx, sources.Provider(settings.SourceProvider), sources.Method(settings.SourceMethod))
	if err != nil {
		_ = s.db.UpdateFetchHealth(ctx, chatID, time.Now(), err.Error())
		s.notifySourceFail(ctx, chatID, settings, err)
		return err
	}

	// Load template
	tmpl, err := s.db.GetTemplate(ctx, settings.TemplateID)
	if err != nil {
		return err
	}

	// Last values (for arrows/triggers)
	lastVals, err := s.db.GetLastValues(ctx, chatID, enabledIDs)
	if err != nil {
		return err
	}

	out := render.BuildMessage(ctx, settings, tmpl, enabledIDs, snap, lastVals)

	// Trigger gating (unless forced)
	if !forced && len(settings.TriggerItems) > 0 {
		if !s.anyTriggerChanged(settings, out.UsedValues, lastVals) {
			// Still update fetch health
			_ = s.db.UpdateFetchHealth(ctx, chatID, snap.FetchedAt, "")
			return nil
		}
	}

	// Post or edit
	msgID, err := s.postOrEdit(ctx, chatID, settings, out)
	if err != nil {
		_ = s.db.UpdateFetchHealth(ctx, chatID, snap.FetchedAt, err.Error())
		s.notifySourceFail(ctx, chatID, settings, err)
		return err
	}

	// Save last values for arrows/triggers
	for id, v := range out.UsedValues {
		_ = s.db.SetLastValue(ctx, chatID, id, v)
	}
	_ = s.db.UpdateLastPost(ctx, chatID, msgID, time.Now())
	_ = s.db.UpdateFetchHealth(ctx, chatID, snap.FetchedAt, "")
	return nil
}

func (s *Scheduler) anyTriggerChanged(settings db.ChatSettings, current map[string]float64, last map[string]float64) bool {
	thresholdType := settings.TriggerThresholdType
	thresholdVal := settings.TriggerThresholdValue
	for _, id := range settings.TriggerItems {
		cur, ok := current[id]
		if !ok {
			continue
		}
		prev, okPrev := last[id]
		if !okPrev {
			return true // first time
		}
		delta := cur - prev
		if delta < 0 {
			delta = -delta
		}
		switch thresholdType {
		case "pct":
			if prev == 0 {
				return true
			}
			pct := (delta / prev) * 100.0
			if pct >= thresholdVal {
				return true
			}
		default: // abs
			if delta >= thresholdVal {
				return true
			}
		}
	}
	return false
}

func (s *Scheduler) postOrEdit(ctx context.Context, chatID int64, settings db.ChatSettings, out render.Output) (int, error) {
	postMode := settings.PostMode
	if postMode == "" {
		postMode = "edit"
	}

	// If edit, try to edit last message
	if postMode == "edit" {
		mid, err := s.db.GetLastPostMessageID(ctx, chatID)
		if err == nil && mid.Valid {
			if out.MediaType != "" && out.MediaFileID != "" {
				// Media message: edit caption
				edit := tgbotapi.NewEditMessageCaption(chatID, int(mid.Int64), out.Text)
				_, err := s.bot.Request(edit)
				if err == nil {
					return int(mid.Int64), nil
				}
			} else {
				edit := tgbotapi.NewEditMessageText(chatID, int(mid.Int64), out.Text)
				edit.DisableWebPagePreview = true
				_, err := s.bot.Request(edit)
				if err == nil {
					return int(mid.Int64), nil
				}
			}
			// If edit failed, fall through to new post.
		}
	}

	// Send new
	if out.MediaType != "" && out.MediaFileID != "" {
		switch out.MediaType {
		case "video":
			msg := tgbotapi.NewVideo(chatID, tgbotapi.FileID(out.MediaFileID))
			msg.Caption = out.Text
			msg.ParseMode = ""
			sent, err := s.bot.Send(msg)
			if err != nil {
				return 0, err
			}
			return sent.MessageID, nil
		default: // photo
			msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(out.MediaFileID))
			msg.Caption = out.Text
			msg.ParseMode = ""
			sent, err := s.bot.Send(msg)
			if err != nil {
				return 0, err
			}
			return sent.MessageID, nil
		}
	}

	msg := tgbotapi.NewMessage(chatID, out.Text)
	msg.DisableWebPagePreview = true
	msg.ParseMode = ""
	sent, err := s.bot.Send(msg)
	if err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

func (s *Scheduler) notifySourceFail(ctx context.Context, chatID int64, settings db.ChatSettings, err error) {
	s.mu.Lock()
	last := s.lastFailNotify[chatID]
	if time.Since(last) < 30*time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastFailNotify[chatID] = time.Now()
	s.mu.Unlock()

	text := fmt.Sprintf("⚠️ خطا در دریافت/ارسال برای چت %d\nمنبع فعلی: %s (%s)\nخطا: %v\n\nمی‌تونید از داخل ربات منبع را تغییر دهید.", chatID, settings.SourceProvider, settings.SourceMethod, err)

	// Provide quick buttons to switch provider/method (handled in bot UI via callback data)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔁 تغییر به Bonbast", fmt.Sprintf("swsrc|%d|bonbast", chatID)),
			tgbotapi.NewInlineKeyboardButtonData("🔁 تغییر به Navasan", fmt.Sprintf("swsrc|%d|navasan", chatID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧰 وضعیت", fmt.Sprintf("status|%d", chatID)),
		),
	)

	if s.notify != nil {
		s.notify.NotifyAdmins(ctx, text, &kb)
	}
}
