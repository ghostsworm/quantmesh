package saas

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

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

func TestCoinbaseWebhookSignatureRequiredAndVerified(t *testing.T) {
	service := NewCryptoPaymentService(nil, "")
	service.SetCoinbaseWebhookSecret("test-secret")
	payload := []byte(`{"event":{"type":"charge:pending"}}`)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	if !service.verifyCoinbaseSignature(payload, signature) {
		t.Fatal("expected valid signature to pass")
	}
	if service.verifyCoinbaseSignature(payload, signature+"00") {
		t.Fatal("expected malformed signature to fail")
	}
	if service.verifyCoinbaseSignature(payload, "") {
		t.Fatal("expected empty signature to fail")
	}
}

func TestCryptoPaymentMoneyOperationsFailClosed(t *testing.T) {
	service := NewCryptoPaymentService(nil, "")
	if _, err := service.CreateCoinbaseCharge("u", "u@example.com", "starter", 49); err == nil {
		t.Fatal("expected Coinbase creation to remain disabled until fulfillment is configured")
	}
	if _, err := service.CreateDirectPayment("u", "u@example.com", "starter", "BTC", 49); err == nil {
		t.Fatal("expected direct wallet payment to remain disabled")
	}
	if err := service.ConfirmDirectPayment(1, "tx"); err == nil {
		t.Fatal("expected manual confirmation to remain disabled")
	}
	if err := service.HandleCoinbaseWebhook([]byte(`{}`), ""); err == nil {
		t.Fatal("expected webhook without configured database and signature to fail")
	}
}

func TestBillingServiceMutationsFailClosedWithoutProviderFulfillment(t *testing.T) {
	service := NewBillingService(nil, "")
	if _, err := service.CreateSubscription("alice", "alice@example.com", "enterprise"); err == nil {
		t.Fatal("expected subscription creation to require provider fulfillment")
	}
	if err := service.UpdateSubscriptionPlan("alice", "enterprise"); err == nil {
		t.Fatal("expected plan changes to require provider fulfillment")
	}
	if err := service.CancelSubscription("alice", true); err == nil {
		t.Fatal("expected cancellation to require provider synchronization")
	}
	if err := service.RenewSubscription("alice"); err == nil {
		t.Fatal("expected renewal to require provider fulfillment")
	}
	if _, err := service.GetSubscription("alice"); err == nil {
		t.Fatal("expected nil database to fail safely")
	}
}

func TestSubmitDirectTransactionHashRequiresOwnerAndPendingPayment(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE crypto_payments (
		id INTEGER PRIMARY KEY, user_id TEXT, payment_method TEXT, status TEXT,
		transaction_hash TEXT, updated_at DATETIME
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO crypto_payments (id, user_id, payment_method, status, transaction_hash)
		VALUES (1, 'alice', 'direct', 'pending', '')`)
	if err != nil {
		t.Fatal(err)
	}
	service := NewCryptoPaymentService(db, "")
	if err := service.SubmitDirectTransactionHash(1, "bob", "0xclaimed"); err == nil {
		t.Fatal("expected another user's transaction submission to be rejected")
	}
	if err := service.SubmitDirectTransactionHash(1, "alice", "0xclaimed"); err != nil {
		t.Fatalf("submit transaction hash: %v", err)
	}
	var status, transactionHash string
	if err := db.QueryRow(`SELECT status, transaction_hash FROM crypto_payments WHERE id = 1`).Scan(&status, &transactionHash); err != nil {
		t.Fatal(err)
	}
	if status != "pending_confirmation" || transactionHash != "0xclaimed" {
		t.Fatalf("stored payment = (%q, %q), want pending_confirmation and submitted hash", status, transactionHash)
	}
	if err := service.SubmitDirectTransactionHash(1, "alice", "0xsecond"); err == nil {
		t.Fatal("expected duplicate transaction submission to be rejected")
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
