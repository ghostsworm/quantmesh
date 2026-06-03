package saas

import "testing"

func TestInstanceManagerAllocateResources(t *testing.T) {
	manager := NewInstanceManager(nil)

	tests := []struct {
		plan       string
		wantCPU    float64
		wantMemory int64
		wantDisk   int64
	}{
		{plan: "starter", wantCPU: 1, wantMemory: 1024, wantDisk: 10240},
		{plan: "professional", wantCPU: 2, wantMemory: 2048, wantDisk: 51200},
		{plan: "enterprise", wantCPU: 4, wantMemory: 8192, wantDisk: 204800},
		{plan: "unknown", wantCPU: 1, wantMemory: 1024, wantDisk: 10240},
	}

	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			got := manager.allocateResources(tt.plan)
			if got.CPU != tt.wantCPU || got.Memory != tt.wantMemory || got.Disk != tt.wantDisk {
				t.Fatalf("allocateResources(%q) = {CPU:%v Memory:%v Disk:%v}, want {CPU:%v Memory:%v Disk:%v}",
					tt.plan, got.CPU, got.Memory, got.Disk, tt.wantCPU, tt.wantMemory, tt.wantDisk)
			}
		})
	}
}

func TestInstanceManagerAllocatePortIncrements(t *testing.T) {
	manager := NewInstanceManager(nil)

	first := manager.allocatePort()
	second := manager.allocatePort()

	if first != 8001 {
		t.Fatalf("first port = %d, want 8001", first)
	}
	if second != 8002 {
		t.Fatalf("second port = %d, want 8002", second)
	}
}

func TestBillingServicePlanPricing(t *testing.T) {
	service := NewBillingService(nil, "")

	if got := service.GetPriceID("professional"); got != "price_professional_monthly" {
		t.Fatalf("GetPriceID(professional) = %q", got)
	}
	if got := service.GetPriceID("missing"); got != "" {
		t.Fatalf("GetPriceID(missing) = %q, want empty", got)
	}
	if got := service.GetPlanPrice("enterprise"); got != 999 {
		t.Fatalf("GetPlanPrice(enterprise) = %v, want 999", got)
	}
	if got := service.GetPlanPrice("missing"); got != 0 {
		t.Fatalf("GetPlanPrice(missing) = %v, want 0", got)
	}
}

func TestCryptoPaymentServiceCalculateCryptoAmount(t *testing.T) {
	service := NewCryptoPaymentService(nil, "")

	tests := []struct {
		currency string
		usd      float64
		want     float64
	}{
		{currency: "BTC", usd: 100000, want: 1},
		{currency: "ETH", usd: 1000, want: 0.25},
		{currency: "USDT", usd: 42.5, want: 42.5},
		{currency: "DOGE", usd: 100, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.currency, func(t *testing.T) {
			if got := service.calculateCryptoAmount(tt.usd, tt.currency); got != tt.want {
				t.Fatalf("calculateCryptoAmount(%v, %q) = %v, want %v", tt.usd, tt.currency, got, tt.want)
			}
		})
	}
}

func TestAutoScalerScaleDecisions(t *testing.T) {
	scaler := NewAutoScaler(nil)

	if !scaler.shouldScaleUp(&ResourceUsage{CPU: 0.81, MemoryPct: 0.2}) {
		t.Fatal("expected CPU above scale-up threshold to scale up")
	}
	if !scaler.shouldScaleUp(&ResourceUsage{CPU: 0.2, MemoryPct: 0.81}) {
		t.Fatal("expected memory above scale-up threshold to scale up")
	}
	if scaler.shouldScaleUp(&ResourceUsage{CPU: 0.8, MemoryPct: 0.8}) {
		t.Fatal("expected exact scale-up thresholds to stay stable")
	}
	if !scaler.shouldScaleDown(&ResourceUsage{CPU: 0.29, MemoryPct: 0.29}) {
		t.Fatal("expected CPU and memory below scale-down thresholds to scale down")
	}
	if scaler.shouldScaleDown(&ResourceUsage{CPU: 0.29, MemoryPct: 0.31}) {
		t.Fatal("expected high memory to prevent scale down")
	}
}
