package monitor

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"
)

// timeframeToDuration 將 "2h","4h" 等轉换為 Duration
func timeframeToDuration(tf string) time.Duration {
	tf = strings.TrimSpace(strings.ToLower(tf))
	if strings.HasSuffix(tf, "h") {
		h, _ := strconv.ParseInt(strings.TrimSuffix(tf, "h"), 10, 64)
		return time.Duration(h) * time.Hour
	}
	if strings.HasSuffix(tf, "m") {
		m, _ := strconv.ParseInt(strings.TrimSuffix(tf, "m"), 10, 64)
		return time.Duration(m) * time.Minute
	}
	return 0
}

// PredictionVerifier 預测驗证器：定時检查待驗证預测，對比實際價格
type PredictionVerifier struct {
	cfg      *config.Config
	storage  storage.Storage
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
}

// NewPredictionVerifier 創建預测驗证器
func NewPredictionVerifier(cfg *config.Config, st storage.Storage) *PredictionVerifier {
	ctx, cancel := context.WithCancel(context.Background())
	return &PredictionVerifier{
		cfg:    cfg,
		storage: st,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 啟动定時驗證（每10分钟）
func (p *PredictionVerifier) Start() {
	if !p.cfg.NewsMonitor.Enabled || p.storage == nil {
		return
	}
	p.done = make(chan struct{})
	go p.verifyLoop()
	logger.Info("📊 預测驗证器已啟动（每10分钟）")
}

// Stop 停止
func (p *PredictionVerifier) Stop() {
	p.cancel()
	if p.done != nil {
		<-p.done
	}
}

func (p *PredictionVerifier) verifyLoop() {
	defer close(p.done)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.verifyPending()
		}
	}
}

func (p *PredictionVerifier) verifyPending() {
	list, err := p.storage.GetPredictionVerificationsByStatus("pending", 50)
	if err != nil {
		logger.Warn("📊 獲取待驗证預测失败: %v", err)
		return
	}
	now := time.Now()
	tolerance := 15 * time.Minute // 價格容差：前后15分钟内
	for _, v := range list {
		dur := timeframeToDuration(v.Timeframe)
		if dur <= 0 {
			v.Status = "expired"
			_ = p.storage.UpdatePredictionVerification(v)
			continue
		}
		verifyTime := v.PredictionTime.Add(dur)
		if now.Before(verifyTime) {
			continue // 还未到驗证時间
		}
		// 超過24h未驗证的標記為過期
		if now.Sub(verifyTime) > 24*time.Hour {
			v.Status = "expired"
			_ = p.storage.UpdatePredictionVerification(v)
			continue
		}
		// 獲取驗证時刻價格
		ph, err := p.storage.GetPriceAtTime(v.AssetType, v.Symbol, verifyTime, tolerance)
		if err != nil || ph == nil {
			continue
		}
		v.ActualPriceAtVerify = ph.Price
		v.VerifiedAt = now
		// 计算實際方向和涨跌幅
		change := (ph.Price - v.ActualPriceAtPred) / v.ActualPriceAtPred * 100
		v.ActualChangePct = change
		if math.Abs(change) < 1 {
			v.ActualDirection = "stable"
		} else if change > 0 {
			v.ActualDirection = "up"
		} else {
			v.ActualDirection = "down"
		}
		// 方向是否正确
		v.IsCorrect = (v.PredictedDirection == v.ActualDirection)
		v.Status = "verified"
		if err := p.storage.UpdatePredictionVerification(v); err != nil {
			logger.Warn("📊 更新預测驗证失败 id=%d: %v", v.ID, err)
		}
	}
}

// CreateVerificationRecords 根據分析結果創建預测驗证記錄（在保存分析历史后調用）
func CreateVerificationRecords(st storage.Storage, analysisID int64, assetType, symbol string, predictionTime time.Time, currentPrice float64, predictions []PricePrediction) {
	if st == nil || analysisID <= 0 || len(predictions) == 0 {
		return
	}
	for _, pred := range predictions {
		// 取概率最高的场景作為主預测
		var best *PriceScenario
		for i := range pred.Scenarios {
			s := &pred.Scenarios[i]
			if best == nil || s.Probability > best.Probability {
				best = s
			}
		}
		if best == nil {
			continue
		}
		dir := best.Direction
		if dir == "neutral" {
			dir = "stable"
		}
		v := &storage.PredictionVerification{
			AnalysisID:            analysisID,
			AssetType:             assetType,
			Symbol:                symbol,
			PredictionTime:        predictionTime,
			Timeframe:             pred.Timeframe,
			PredictedDirection:    dir,
			PredictedChangePct:    best.ChangePercent,
			PredictedProbability:  best.Probability,
			ActualPriceAtPred:     currentPrice,
			Status:                "pending",
		}
		if err := st.SavePredictionVerification(v); err != nil {
			logger.Warn("📊 保存預测驗证記錄失败: %v", err)
		}
	}
}
