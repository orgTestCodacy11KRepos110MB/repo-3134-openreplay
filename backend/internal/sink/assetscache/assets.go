package assetscache

import (
	"context"
	"crypto/md5"
	"go.opentelemetry.io/otel/metric/instrument/syncfloat64"
	"io"
	"log"
	"net/url"
	"openreplay/backend/internal/config/sink"
	"openreplay/backend/pkg/messages"
	"openreplay/backend/pkg/monitoring"
	"openreplay/backend/pkg/queue/types"
	"openreplay/backend/pkg/url/assets"
	"sync"
	"time"
)

type CachedAsset struct {
	msg string
	ts  time.Time
}

type AssetsCache struct {
	mutex         sync.RWMutex
	cfg           *sink.Config
	rewriter      *assets.Rewriter
	producer      types.Producer
	cache         map[string]*CachedAsset
	totalAssets   syncfloat64.Counter
	cachedAssets  syncfloat64.Counter
	skippedAssets syncfloat64.Counter
	assetSize     syncfloat64.Histogram
	assetDuration syncfloat64.Histogram
}

func New(cfg *sink.Config, rewriter *assets.Rewriter, producer types.Producer, metrics *monitoring.Metrics) *AssetsCache {
	// Assets metrics
	totalAssets, err := metrics.RegisterCounter("assets_total")
	if err != nil {
		log.Printf("can't create assets_total metric: %s", err)
	}
	cachedAssets, err := metrics.RegisterCounter("assets_cached")
	if err != nil {
		log.Printf("can't create assets_cached metric: %s", err)
	}
	skippedAssets, err := metrics.RegisterCounter("assets_skipped")
	if err != nil {
		log.Printf("can't create assets_skipped metric: %s", err)
	}
	assetSize, err := metrics.RegisterHistogram("asset_size")
	if err != nil {
		log.Printf("can't create asset_size metric: %s", err)
	}
	assetDuration, err := metrics.RegisterHistogram("asset_duration")
	if err != nil {
		log.Printf("can't create asset_duration metric: %s", err)
	}
	assetsCache := &AssetsCache{
		cfg:           cfg,
		rewriter:      rewriter,
		producer:      producer,
		cache:         make(map[string]*CachedAsset, 64),
		totalAssets:   totalAssets,
		cachedAssets:  cachedAssets,
		skippedAssets: skippedAssets,
		assetSize:     assetSize,
		assetDuration: assetDuration,
	}
	go assetsCache.cleaner()
	return assetsCache
}

func (e *AssetsCache) cleaner() {
	cleanTick := time.Tick(time.Minute * 30)
	for {
		select {
		case <-cleanTick:
			e.clearCache()
		}
	}
}

func (e *AssetsCache) clearCache() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	now := time.Now()
	cacheSize := len(e.cache)
	deleted := 0

	for id, cache := range e.cache {
		if int64(now.Sub(cache.ts).Minutes()) > e.cfg.CacheExpiration {
			deleted++
			delete(e.cache, id)
		}
	}
	log.Printf("cache cleaner: deleted %d/%d assets", deleted, cacheSize)
}

func (e *AssetsCache) ParseAssets(msg messages.Message) messages.Message {
	switch m := msg.(type) {
	case *messages.SetNodeAttributeURLBased:
		if m.Name == "src" || m.Name == "href" {
			newMsg := &messages.SetNodeAttribute{
				ID:    m.ID,
				Name:  m.Name,
				Value: e.handleURL(m.SessionID(), m.BaseURL, m.Value),
			}
			newMsg.SetMeta(msg.Meta())
			return newMsg
		} else if m.Name == "style" {
			newMsg := &messages.SetNodeAttribute{
				ID:    m.ID,
				Name:  m.Name,
				Value: e.handleCSS(m.SessionID(), m.BaseURL, m.Value),
			}
			newMsg.SetMeta(msg.Meta())
			return newMsg
		}
	case *messages.SetCSSDataURLBased:
		newMsg := &messages.SetCSSData{
			ID:   m.ID,
			Data: e.handleCSS(m.SessionID(), m.BaseURL, m.Data),
		}
		newMsg.SetMeta(msg.Meta())
		return newMsg
	case *messages.CSSInsertRuleURLBased:
		newMsg := &messages.CSSInsertRule{
			ID:    m.ID,
			Index: m.Index,
			Rule:  e.handleCSS(m.SessionID(), m.BaseURL, m.Rule),
		}
		newMsg.SetMeta(msg.Meta())
		return newMsg
	case *messages.AdoptedSSReplaceURLBased:
		newMsg := &messages.AdoptedSSReplace{
			SheetID: m.SheetID,
			Text:    e.handleCSS(m.SessionID(), m.BaseURL, m.Text),
		}
		newMsg.SetMeta(msg.Meta())
		return newMsg
	case *messages.AdoptedSSInsertRuleURLBased:
		newMsg := &messages.AdoptedSSInsertRule{
			SheetID: m.SheetID,
			Index:   m.Index,
			Rule:    e.handleCSS(m.SessionID(), m.BaseURL, m.Rule),
		}
		newMsg.SetMeta(msg.Meta())
		return newMsg
	}
	return msg
}

func (e *AssetsCache) sendAssetForCache(sessionID uint64, baseURL string, relativeURL string) {
	if fullURL, cacheable := assets.GetFullCachableURL(baseURL, relativeURL); cacheable {
		assetMessage := &messages.AssetCache{URL: fullURL}
		if err := e.producer.Produce(
			e.cfg.TopicCache,
			sessionID,
			assetMessage.Encode(),
		); err != nil {
			log.Printf("can't send asset to cache topic, sessID: %d, err: %s", sessionID, err)
		}
	}
}

func (e *AssetsCache) sendAssetsForCacheFromCSS(sessionID uint64, baseURL string, css string) {
	for _, u := range assets.ExtractURLsFromCSS(css) { // TODO: in one shot with rewriting
		e.sendAssetForCache(sessionID, baseURL, u)
	}
}

func (e *AssetsCache) handleURL(sessionID uint64, baseURL string, urlVal string) string {
	if e.cfg.CacheAssets {
		e.sendAssetForCache(sessionID, baseURL, urlVal)
		return e.rewriter.RewriteURL(sessionID, baseURL, urlVal)
	} else {
		return assets.ResolveURL(baseURL, urlVal)
	}
}

func (e *AssetsCache) handleCSS(sessionID uint64, baseURL string, css string) string {
	ctx := context.Background()
	e.totalAssets.Add(ctx, 1)
	// Try to find asset in cache
	h := md5.New()
	// Cut first part of url (scheme + host)
	u, err := url.Parse(baseURL)
	if err != nil {
		log.Printf("can't parse url: %s, err: %s", baseURL, err)
		if e.cfg.CacheAssets {
			e.sendAssetsForCacheFromCSS(sessionID, baseURL, css)
		}
		return e.getRewrittenCSS(sessionID, baseURL, css)
	}
	justUrl := u.Scheme + "://" + u.Host + "/"
	// Calculate hash sum of url + css
	io.WriteString(h, justUrl)
	io.WriteString(h, css)
	hash := string(h.Sum(nil))
	// Check the resulting hash in cache
	e.mutex.RLock()
	cachedAsset, ok := e.cache[hash]
	e.mutex.RUnlock()
	if ok {
		if int64(time.Now().Sub(cachedAsset.ts).Minutes()) < e.cfg.CacheExpiration {
			e.skippedAssets.Add(ctx, 1)
			return cachedAsset.msg
		}
	}
	// Send asset to download in assets service
	if e.cfg.CacheAssets {
		e.sendAssetsForCacheFromCSS(sessionID, baseURL, css)
	}
	// Rewrite asset
	start := time.Now()
	res := e.getRewrittenCSS(sessionID, baseURL, css)
	duration := time.Now().Sub(start).Milliseconds()
	e.assetSize.Record(ctx, float64(len(res)))
	e.assetDuration.Record(ctx, float64(duration))
	// Save asset to cache if we spent more than threshold
	if duration > e.cfg.CacheThreshold {
		e.mutex.Lock()
		e.cache[hash] = &CachedAsset{
			msg: res,
			ts:  time.Now(),
		}
		e.mutex.Unlock()
		e.cachedAssets.Add(ctx, 1)
	}
	// Return rewritten asset
	return res
}

func (e *AssetsCache) getRewrittenCSS(sessionID uint64, url, css string) string {
	if e.cfg.CacheAssets {
		return e.rewriter.RewriteCSS(sessionID, url, css)
	} else {
		return assets.ResolveCSS(url, css)
	}
}
