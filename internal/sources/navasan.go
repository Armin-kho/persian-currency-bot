
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Armin-kho/persian-currency-bot/internal/items"
)

type navasanItem struct {
	Value      any `json:"value"`
	Date       any `json:"date"`
	Change     any `json:"change"`
	Percent    any `json:"percent"`
	DollarRate any `json:"dollar_rate"`
	DirhamRate any `json:"dirham_rate"`
}

// fetchNavasanAPI uses the official Navasan API: /latest/?api_key=...
func fetchNavasanAPI(ctx context.Context, client *http.Client, apiKey string) (Snapshot, error) {
	u := "https://api.navasan.tech/latest/?" + url.Values{"api_key": {apiKey}, "dollar_rate": {"true"}}.Encode()
	body, err := httpGet(ctx, client, u)
	if err != nil {
		// Fallback to http (some environments block https)
		u2 := "http://api.navasan.tech/latest/?" + url.Values{"api_key": {apiKey}, "dollar_rate": {"true"}}.Encode()
		body, err = httpGet(ctx, client, u2)
		if err != nil {
			return Snapshot{}, err
		}
	}

	raw, err := decodeNavasanMap(body)
	if err != nil {
		return Snapshot{}, err
	}

	quotes := buildNavasanQuotes(raw)
	return Snapshot{Provider: ProviderNavasan, Method: MethodAPI, FetchedAt: time.Now(), Quotes: quotes}, nil
}

// fetchNavasanScrape uses the site JSON endpoints used by navasan.net itself.
// This avoids an API key but is unofficial and may change.
func fetchNavasanScrape(ctx context.Context, client *http.Client) (Snapshot, error) {
	// Use cache-busting param similar to their JS (time/10)
	cb := strconv.FormatInt(time.Now().Unix()/10, 10)
	endpoints := []string{
		"https://www.navasan.net/last_currencies.php?_=" + cb,
		"https://www.navasan.net/gold_rates.php?_=" + cb,
		"https://www.navasan.net/aed_based_rates.php?_=" + cb,
	}

	merged := map[string]navasanItem{}
	success := 0
	for _, ep := range endpoints {
		body, err := httpGet(ctx, client, ep)
		if err != nil {
			continue
		}
		part, err := decodeNavasanMap(body)
		if err != nil {
			continue
		}
		for k, v := range part {
			merged[k] = v
		}
		success++
	}

	if len(merged) == 0 {
		return Snapshot{}, fmt.Errorf("navasan scrape failed (no data from endpoints)")
	}

	_ = success
	quotes := buildNavasanQuotes(merged)
	return Snapshot{Provider: ProviderNavasan, Method: MethodScrape, FetchedAt: time.Now(), Quotes: quotes}, nil
}

func buildNavasanQuotes(raw map[string]navasanItem) map[string]Quote {
	quotes := map[string]Quote{}
	for _, it := range items.All {
		if it.NavasanKey == "" {
			continue
		}

		// We try multiple keys because navasan sometimes provides either a base code or *_sell/*_buy.
		sellKeys := []string{}
		if it.NavasanSellKey != "" {
			sellKeys = append(sellKeys, it.NavasanSellKey)
		}
		sellKeys = appendUnique(sellKeys, it.NavasanKey, it.NavasanKey+"_sell")

		buyKeys := []string{}
		if it.NavasanBuyKey != "" {
			buyKeys = append(buyKeys, it.NavasanBuyKey)
		}
		buyKeys = appendUnique(buyKeys, it.NavasanKey+"_buy")

		sellItem, okSellItem := findFirst(raw, sellKeys)
		buyItem, okBuyItem := findFirst(raw, buyKeys)

		var sell *float64
		if okSellItem {
			// Crypto: prefer dollar_rate if available (gives USD price)
			if it.NavasanIsCrypto {
				if dv, ok2 := toFloat(sellItem.DollarRate); ok2 && dv > 0 {
					sell = &dv
					quotes[it.ID] = Quote{Sell: sell, Buy: nil, Unit: items.UnitUSD}
					continue
				}
			}
			if v, ok2 := toFloat(sellItem.Value); ok2 {
				sell = &v
			}
		}

		var buy *float64
		if okBuyItem {
			if v, ok2 := toFloat(buyItem.Value); ok2 {
				buy = &v
			}
		}

		if sell == nil && buy == nil {
			continue
		}
		unit := it.NavasanUnit
		quotes[it.ID] = Quote{Sell: sell, Buy: buy, Unit: unit}
	}
	return quotes
}

func appendUnique(list []string, vals ...string) []string {
	set := map[string]bool{}
	for _, v := range list {
		set[v] = true
	}
	for _, v := range vals {
		if v == "" || set[v] {
			continue
		}
		list = append(list, v)
		set[v] = true
	}
	return list
}

func findFirst(m map[string]navasanItem, keys []string) (navasanItem, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v, true
		}
	}
	return navasanItem{}, false
}

func httpGet(ctx context.Context, client *http.Client, urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PersianCurrencyBot/1.0; +https://github.com/Armin-kho/persian-currency-bot)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func decodeNavasanMap(body []byte) (map[string]navasanItem, error) {
	var raw map[string]navasanItem
	if err := json.Unmarshal(body, &raw); err != nil {
		snip := string(body)
		if len(snip) > 200 {
			snip = snip[:200]
		}
		return nil, fmt.Errorf("navasan decode: %w (%s)", err, snip)
	}
	return raw, nil
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		return t, true
	case int:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f, true
		}
	case string:
		s := strings.TrimSpace(t)
		s = strings.ReplaceAll(s, ",", "")
		if s == "" || s == "-" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
