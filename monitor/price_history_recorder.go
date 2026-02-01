package monitor

import (
	"context"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"
)

// PriceHistoryRecorder 定时记录价格历史，用于预测验证
type PriceHistoryRecorder struct {
	cfg        *config.Config
	storage    storage.Storage
	getPrice   PriceGetter
	ctx        context.Context
	cancel     context.CancelFunc
	recordDone chan struct{}
	mu         sync.Mutex
}

// NewPriceHistoryRecorder 创建价格历史记录器
func NewPriceHistoryRecorder(cfg *config.Config, st storage.Storage, getPrice PriceGetter) *PriceHistoryRecorder {
	ctx, cancel := context.WithCancel(context.Background())
	return &PriceHistoryRecorder{
		cfg:      cfg,
		storage:  st,
		getPrice: getPrice,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动定时记录（每5分钟）
func (p *PriceHistoryRecorder) Start() {
	if !p.cfg.NewsMonitor.Enabled || p.storage == nil || p.getPrice == nil {
		logger.Debug("📊 PriceHistoryRecorder: 未启用或未配置，跳过")
		return
	}
	assets := p.cfg.NewsMonitor.Assets
	if len(assets) == 0 {
		assets = []config.AssetConfig{
			{AssetType: "crypto_btc", Symbol: "BTCUSDT", Enabled: true},
			{AssetType: "commodity_gold", Symbol: "PAXGUSDT", Enabled: true},
		}
	}
	p.recordDone = make(chan struct{})
	go p.recordLoop(assets)
	logger.Info("📊 价格历史记录器已启动（每5分钟）")
}

// Stop 停止
func (p *PriceHistoryRecorder) Stop() {
	p.cancel()
	if p.recordDone != nil {
		<-p.recordDone
	}
}

func (p *PriceHistoryRecorder) recordLoop(assets []config.AssetConfig) {
	defer func() {
		if p.recordDone != nil {
			close(p.recordDone)
		}
	}()
	interval := 5 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// 启动时立即记录一次
	p.recordNow(assets)
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.recordNow(assets)
		}
	}
}

func (p *PriceHistoryRecorder) recordNow(assets []config.AssetConfig) {
	now := time.Now()
	for _, a := range assets {
		if !a.Enabled || a.Symbol == "" {
			continue
		}
		price := p.getPrice(a.Symbol)
		if price <= 0 {
			continue
		}
		h := &storage.PriceHistory{
			AssetType:  a.AssetType,
			Symbol:     a.Symbol,
			Price:      price,
			Source:     "exchange",
			RecordedAt: now,
			CreatedAt:  now,
		}
		if err := p.storage.SavePriceHistory(h); err != nil {
			logger.Warn("📊 保存价格历史失败 %s: %v", a.Symbol, err)
		}
	}
}
