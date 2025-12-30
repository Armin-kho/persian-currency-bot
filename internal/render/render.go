
package render

import (
	"context"
	"strings"

	"github.com/Armin-kho/persian-currency-bot/internal/db"
	"github.com/Armin-kho/persian-currency-bot/internal/items"
	"github.com/Armin-kho/persian-currency-bot/internal/sources"
	"github.com/Armin-kho/persian-currency-bot/internal/utils"
)

type Line struct {
	ItemID string
	Text   string
	// UsedValue is the numeric value we compared for arrow/trigger (based on price_mode).
	UsedValue float64
	HasValue  bool
	Delta     float64
	Arrow     string
	Unit      string
	Category  items.Category
}

type Output struct {
	Text      string
	Lines     []Line
	UsedValues map[string]float64
	MediaType string
	MediaFileID string
}

// BuildMessage renders the current template into a final message text.
func BuildMessage(ctx context.Context, settings db.ChatSettings, tmpl db.Template, enabledItemIDs []string, snap sources.Snapshot, lastValues map[string]float64) Output {
	// Build lines in chat order (but we will place them into sections by category placeholders).
	lines := []Line{}
	used := map[string]float64{}

	for _, id := range enabledItemIDs {
		it, ok := items.ByID(id)
		if !ok {
			continue
		}
		q, ok := snap.Quotes[id]
		if !ok {
			continue
		}

		// Determine displayed price string
		priceStr, usedVal, hasVal := formatPrice(settings.PriceMode, settings.Digits, q)
		if !hasVal {
			continue
		}

		// Arrow / delta
		prev, okPrev := lastValues[id]
		delta := 0.0
		arrow := ""
		if okPrev {
			delta = usedVal - prev
			if delta > 0 {
				arrow = " ▲"
			} else if delta < 0 {
				arrow = " 🔻"
			} else if settings.ShowSameArrow {
				arrow = " ▬"
			}
		} else {
			// first time -> no arrow
		}

		lineText := it.Emoji + " " + it.NameFa + " " + priceStr + arrow
		if settings.Digits == "fa" {
			// priceStr already converted, but template static parts and maybe name already Persian.
			// Do nothing extra.
		}

		lines = append(lines, Line{
			ItemID:   id,
			Text:     lineText,
			UsedValue: usedVal,
			HasValue: hasVal,
			Delta:    delta,
			Arrow:    arrow,
			Unit:     q.Unit,
			Category: it.Category,
		})
		used[id] = usedVal
	}

	// Sections
	var currencies, coins, golds []string
	for _, ln := range lines {
		switch ln.Category {
		case items.CategoryCurrency:
			currencies = append(currencies, ln.Text)
		case items.CategoryCoin:
			coins = append(coins, ln.Text)
		case items.CategoryGold, items.CategoryCrypto:
			golds = append(golds, ln.Text)
		}
	}

	body := tmpl.Body
	body = strings.ReplaceAll(body, "{CURRENCIES}", strings.Join(currencies, "\n"))
	body = strings.ReplaceAll(body, "{COINS}", strings.Join(coins, "\n"))
	body = strings.ReplaceAll(body, "{GOLD}", strings.Join(golds, "\n"))

	dt := utils.JalaliDateTime(utils.NowTehran())
	if settings.Digits == "fa" {
		dt = utils.ToPersianDigits(dt)
	}
	body = strings.ReplaceAll(body, "{DATETIME}", dt)
	// Convenience aliases
	body = strings.ReplaceAll(body, "{DATE}", strings.Split(dt, " - ")[0])
	body = strings.ReplaceAll(body, "{TIME}", strings.Split(dt, " - ")[1])

	// If user wanted blank for empty sections, we're already inserting "" and leaving separators as-is.
	// We can trim extra blank lines.
	body = strings.TrimSpace(body)

	return Output{
		Text:        body,
		Lines:       lines,
		UsedValues:  used,
		MediaType:   tmpl.MediaType,
		MediaFileID: tmpl.MediaFileID,
	}
}

func formatPrice(priceMode, digits string, q sources.Quote) (string, float64, bool) {
	// pick usedVal for comparison
	var usedVal float64
	var hasVal bool
	var unit string = q.Unit

	switch priceMode {
	case "buy":
		if q.Buy != nil {
			usedVal = *q.Buy
			hasVal = true
		} else if q.Sell != nil {
			usedVal = *q.Sell
			hasVal = true
		}
	case "both":
		// Use sell as primary if available, else buy
		if q.Sell != nil {
			usedVal = *q.Sell
			hasVal = true
		} else if q.Buy != nil {
			usedVal = *q.Buy
			hasVal = true
		}
	default: // sell
		if q.Sell != nil {
			usedVal = *q.Sell
			hasVal = true
		} else if q.Buy != nil {
			usedVal = *q.Buy
			hasVal = true
		}
	}

	if !hasVal {
		return "", 0, false
	}

	// Display string
	switch priceMode {
	case "both":
		if q.Sell != nil && q.Buy != nil {
			a := utils.FormatNumber(*q.Sell, unit, digits)
			b := utils.FormatNumber(*q.Buy, unit, digits)
			return a + " / " + b, usedVal, true
		}
		return utils.FormatNumber(usedVal, unit, digits), usedVal, true
	case "buy":
		if q.Buy != nil {
			return utils.FormatNumber(*q.Buy, unit, digits), usedVal, true
		}
		return utils.FormatNumber(usedVal, unit, digits), usedVal, true
	default:
		return utils.FormatNumber(usedVal, unit, digits), usedVal, true
	}
}
