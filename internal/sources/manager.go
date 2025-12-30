
package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Armin-kho/persian-currency-bot/internal/db"
)

type cacheEntry struct {
	snap Snapshot
	err  error
	at   time.Time
}

// Manager provides cached fetches for each provider+method.
type Manager struct {
	db     *db.DB
	client *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry

	// Bonbast scrape param cache
	bonbastParamMu sync.Mutex
	bonbastParam   string
	bonbastParamAt time.Time
}

func NewManager(database *db.DB) *Manager {
	return &Manager{
		db: database,
		client: &http.Client{
			Timeout: 12 * time.Second,
		},
		cache: map[string]cacheEntry{},
	}
}

func cacheKey(p Provider, m Method) string { return string(p) + "|" + string(m) }

func (m *Manager) Get(ctx context.Context, provider Provider, method Method) (Snapshot, error) {
	key := cacheKey(provider, method)

	// Small TTL to avoid hammering sources (especially when multiple chats post at the same minute)
	const ttl = 20 * time.Second

	m.mu.Lock()
	if ce, ok := m.cache[key]; ok && time.Since(ce.at) < ttl {
		m.mu.Unlock()
		if ce.err != nil {
			return Snapshot{}, ce.err
		}
		return ce.snap, nil
	}
	m.mu.Unlock()

	// Fetch fresh
	snap, err := m.fetch(ctx, provider, method)

	m.mu.Lock()
	m.cache[key] = cacheEntry{snap: snap, err: err, at: time.Now()}
	m.mu.Unlock()

	return snap, err
}

func (m *Manager) fetch(ctx context.Context, provider Provider, method Method) (Snapshot, error) {
	switch provider {
	case ProviderBonbast:
		switch method {
		case MethodAPI:
			user, _, err := m.db.GetGlobalSetting(ctx, "bonbast_api_username")
			if err != nil {
				return Snapshot{}, err
			}
			hash, _, err := m.db.GetGlobalSetting(ctx, "bonbast_api_hash")
			if err != nil {
				return Snapshot{}, err
			}
			if user == "" || hash == "" {
				return Snapshot{}, errors.New("Bonbast API is not configured (username/hash)")
			}
			return fetchBonbastAPI(ctx, m.client, user, hash)
		case MethodScrape:
			return fetchBonbastScrape(ctx, m.client, &m.bonbastParamMu, &m.bonbastParam, &m.bonbastParamAt)
		}
	case ProviderNavasan:
		switch method {
		case MethodAPI:
			key, _, err := m.db.GetGlobalSetting(ctx, "navasan_api_key")
			if err != nil {
				return Snapshot{}, err
			}
			if key == "" {
				return Snapshot{}, errors.New("Navasan API key is not configured")
			}
			return fetchNavasanAPI(ctx, m.client, key)
		case MethodScrape:
			return fetchNavasanScrape(ctx, m.client)
		}
	}
	return Snapshot{}, fmt.Errorf("unsupported provider/method: %s/%s", provider, method)
}
