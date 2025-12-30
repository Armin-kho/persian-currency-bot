
package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Armin-kho/persian-currency-bot/internal/items"
)

var bonbastParamRegex = regexp.MustCompile(`param\s*:\s*"([^"]+)"`)

// fetchBonbastScrape uses the website flow: GET homepage -> extract param -> POST /json.
// This is "scraping/unofficial" but is the same endpoint the site uses.
func fetchBonbastScrape(ctx context.Context, client *http.Client, mu *sync.Mutex, cachedParam *string, cachedAt *time.Time) (Snapshot, error) {
	param, err := getBonbastParam(ctx, client, mu, cachedParam, cachedAt)
	if err != nil {
		return Snapshot{}, err
	}
	body, err := postForm(ctx, client, "https://bonbast.com/json", url.Values{"param": {param}})
	if err != nil {
		// If param expired, refresh once.
		mu.Lock()
		*cachedParam = ""
		*cachedAt = time.Time{}
		mu.Unlock()
		param2, err2 := getBonbastParam(ctx, client, mu, cachedParam, cachedAt)
		if err2 != nil {
			return Snapshot{}, err
		}
		body, err = postForm(ctx, client, "https://bonbast.com/json", url.Values{"param": {param2}})
		if err != nil {
			return Snapshot{}, err
		}
	}

	raw, err := decodeJSONMap(body)
	if err != nil {
		return Snapshot{}, err
	}
	quotes := map[string]Quote{}
	for _, it := range items.All {
		if it.BonbastSellKey == "" {
			continue
		}
		sv, ok := getFloat(raw, it.BonbastSellKey)
		if !ok {
			continue
		}
		var sell *float64
		sell = &sv
		var buy *float64
		if it.BonbastBuyKey != "" {
			if bv, ok := getFloat(raw, it.BonbastBuyKey); ok {
				buy = &bv
			}
		}
		quotes[it.ID] = Quote{Sell: sell, Buy: buy, Unit: it.BonbastUnit}
	}
	return Snapshot{Provider: ProviderBonbast, Method: MethodScrape, FetchedAt: time.Now(), Quotes: quotes}, nil
}

// fetchBonbastAPI calls the official API (paid) documented by Bonbast.
func fetchBonbastAPI(ctx context.Context, client *http.Client, username, hash string) (Snapshot, error) {
	if username == "" || hash == "" {
		return Snapshot{}, errors.New("missing bonbast api username/hash")
	}
	urlStr := fmt.Sprintf("https://bonbast.com/api/%s", url.PathEscape(username))
	body, err := postForm(ctx, client, urlStr, url.Values{"hash": {hash}})
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := decodeJSONMap(body)
	if err != nil {
		return Snapshot{}, err
	}
	quotes := map[string]Quote{}
	for _, it := range items.All {
		if it.BonbastSellKey == "" {
			continue
		}
		sv, ok := getFloat(raw, it.BonbastSellKey)
		if !ok {
			continue
		}
		sell := &sv
		var buy *float64
		if it.BonbastBuyKey != "" {
			if bv, ok := getFloat(raw, it.BonbastBuyKey); ok {
				buy = &bv
			}
		}
		quotes[it.ID] = Quote{Sell: sell, Buy: buy, Unit: it.BonbastUnit}
	}
	return Snapshot{Provider: ProviderBonbast, Method: MethodAPI, FetchedAt: time.Now(), Quotes: quotes}, nil
}

func getBonbastParam(ctx context.Context, client *http.Client, mu *sync.Mutex, cachedParam *string, cachedAt *time.Time) (string, error) {
	mu.Lock()
	if *cachedParam != "" && time.Since(*cachedAt) < 2*time.Minute {
		p := *cachedParam
		mu.Unlock()
		return p, nil
	}
	mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://bonbast.com/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PersianCurrencyBot/1.0; +https://github.com/Armin-kho/persian-currency-bot)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("bonbast homepage status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	matches := bonbastParamRegex.FindSubmatch(b)
	if len(matches) < 2 {
		return "", errors.New("failed to find bonbast param in homepage html")
	}
	param := string(matches[1])

	mu.Lock()
	*cachedParam = param
	*cachedAt = time.Now()
	mu.Unlock()

	return param, nil
}

func postForm(ctx context.Context, client *http.Client, urlStr string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func decodeJSONMap(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func getFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f, true
		}
	case float64:
		return t, true
	case string:
		// remove commas
		s := strings.ReplaceAll(t, ",", "")
		if s == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
