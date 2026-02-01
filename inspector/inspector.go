package inspector

import (
	"context"
	"sync"
	"time"

	"quantmesh/logger"
)

// NotifyReportFunc 發送報告回調（由 main 注入，調用 notify 或郵件等）
type NotifyReportFunc func(report *InspectorReport)

// SaveReportFunc 持久化報告回調（可選）
type SaveReportFunc func(report *InspectorReport) error

// SophonInspector 智子巡檢主入口
type SophonInspector struct {
	Collector      *Collector
	Analyzer       *Analyzer
	EventMonitor   *EventMonitor
	ReportGen      *ReportGenerator
	Scheduler      *Scheduler
	NotifyReport   NotifyReportFunc
	SaveReport     SaveReportFunc
	GoldAnalyzer   *GoldAnalyzer

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	running bool
}

// NewSophonInspector 創建智子巡檢
func NewSophonInspector(opts *SophonInspectorOptions) *SophonInspector {
	if opts == nil {
		opts = &SophonInspectorOptions{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	insp := &SophonInspector{
		Collector:    opts.Collector,
		Analyzer:     opts.Analyzer,
		EventMonitor: opts.EventMonitor,
		ReportGen:    opts.ReportGen,
		Scheduler:    opts.Scheduler,
		NotifyReport: opts.NotifyReport,
		SaveReport:   opts.SaveReport,
		GoldAnalyzer: opts.GoldAnalyzer,
		ctx:          ctx,
		cancel:       cancel,
	}
	if insp.ReportGen == nil {
		cfg := DefaultReportConfig()
		insp.ReportGen = &ReportGenerator{Config: cfg}
	}
	return insp
}

// SophonInspectorOptions 智子巡檢組件選項
type SophonInspectorOptions struct {
	Collector    *Collector
	Analyzer     *Analyzer
	EventMonitor *EventMonitor
	ReportGen    *ReportGenerator
	Scheduler    *Scheduler
	NotifyReport NotifyReportFunc
	SaveReport   SaveReportFunc
	GoldAnalyzer *GoldAnalyzer
}

// Start 啟動智子巡檢：啟動調度，按間隔執行採集、事件檢測、報告生成與通知
func (s *SophonInspector) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	if s.Scheduler == nil {
		logger.Info("智子巡檢未配置調度器，僅支援手動觸發")
		return
	}
	s.Scheduler.Run(s.onSchedule)
	logger.Info("智子巡檢已啟動")
}

// Stop 停止智子巡檢
func (s *SophonInspector) Stop() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.Scheduler != nil {
		s.Scheduler.Stop()
	}
	logger.Info("智子巡檢已停止")
}

// onSchedule 定時觸發：採集 -> 事件檢測（立即通知）-> AI 分析 -> 定時報告生成與通知
func (s *SophonInspector) onSchedule() {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	if s.Collector == nil {
		return
	}

	snap := s.Collector.Collect(ctx)
	if snap == nil {
		return
	}
	// 黃金專項（採集後補充，若 Collector 未提供則由此處計算）
	if s.GoldAnalyzer != nil && snap.GoldAnalysis == nil {
		snap.GoldAnalysis = s.GoldAnalyzer.Analyze()
	}

	// 事件檢測：若有緊急事件則立即發送
	if s.EventMonitor != nil {
		events := s.EventMonitor.Check(snap)
		for _, ev := range events {
			report := s.ReportGen.GenerateUrgent(ev)
			s.notifyAndSave(report)
		}
	}

	// AI 分析
	var analysis *InspectionAnalysis
	if s.Analyzer != nil {
		var err error
		analysis, err = s.Analyzer.Analyze(ctx, snap)
		if err != nil {
			logger.Warn("智子巡檢 AI 分析失敗: %v", err)
		}
	}
	if analysis == nil {
		analysis = &InspectionAnalysis{GeneratedAt: time.Now()}
	}

	// 定時彙總報告
	report := s.ReportGen.GenerateScheduled(snap, analysis)
	s.notifyAndSave(report)
}

func (s *SophonInspector) notifyAndSave(report *InspectorReport) {
	if report == nil {
		return
	}
	if s.NotifyReport != nil {
		s.NotifyReport(report)
	}
	if s.SaveReport != nil {
		if err := s.SaveReport(report); err != nil {
			logger.Warn("智子巡檢保存報告失敗: %v", err)
		}
	}
}

// RunOnce 手動執行一次採集與報告（不經調度）
func (s *SophonInspector) RunOnce(ctx context.Context) (*InspectorReport, error) {
	if s.Collector == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snap := s.Collector.Collect(ctx)
	if snap == nil {
		return nil, nil
	}
	if s.GoldAnalyzer != nil && snap.GoldAnalysis == nil {
		snap.GoldAnalysis = s.GoldAnalyzer.Analyze()
	}
	var analysis *InspectionAnalysis
	if s.Analyzer != nil {
		var err error
		analysis, err = s.Analyzer.Analyze(ctx, snap)
		if err != nil {
			logger.Warn("智子巡檢 AI 分析失敗: %v", err)
		}
	}
	if analysis == nil {
		analysis = &InspectionAnalysis{GeneratedAt: time.Now()}
	}
	report := s.ReportGen.GenerateScheduled(snap, analysis)
	s.notifyAndSave(report)
	return report, nil
}
