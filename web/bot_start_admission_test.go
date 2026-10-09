package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

type startAdmissionObservation struct {
	id  string
	err error
}

type startAdmissionProbe struct {
	mockBotManagerForDeleteGroupTest
	enableErr error
	onEnable  func()
	enables   atomic.Int32
	starts    chan startAdmissionObservation
	startGate <-chan struct{}
	running   bool
}

func (p *startAdmissionProbe) EnableBot(string) error {
	p.enables.Add(1)
	if p.onEnable != nil {
		p.onEnable()
	}
	return p.enableErr
}

func (p *startAdmissionProbe) StartBot(ctx context.Context, cfg config.BotConfig) error {
	if p.startGate != nil {
		<-p.startGate
	}
	p.starts <- startAdmissionObservation{config.BotIDOrGenerate(cfg), ctx.Err()}
	return nil
}

func (p *startAdmissionProbe) GetBot(id string) (*BotDetailResponse, bool) {
	return &BotDetailResponse{BotResponse: BotResponse{BotID: id, Running: p.running}}, true
}

func callStartAdmission(id string, ctx context.Context) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", nil).WithContext(ctx)
	postBotStart(c)
	return w
}

func TestStartAdmissionEnableFailureDoesNotDispatch(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	probe := &startAdmissionProbe{enableErr: errors.New("private-enable-fixture"), starts: make(chan startAdmissionObservation, 1)}
	RegisterBotManagerProvider(probe)
	w := callStartAdmission(id, t.Context())
	started := false
	select {
	case <-probe.starts:
		started = true
	case <-time.After(100 * time.Millisecond):
	}
	if w.Code != http.StatusServiceUnavailable || started || strings.Contains(w.Body.String(), "private-enable-fixture") {
		t.Fatalf("unverified enable admitted startup: status=%d started=%v", w.Code, started)
	}
}

func TestStartAdmissionPinsProviderBeforeDispatch(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	replacement := &startAdmissionProbe{starts: make(chan startAdmissionObservation, 1)}
	original := &startAdmissionProbe{starts: make(chan startAdmissionObservation, 1), onEnable: func() { RegisterBotManagerProvider(replacement) }}
	RegisterBotManagerProvider(original)
	w := callStartAdmission(id, t.Context())
	if w.Code != http.StatusAccepted {
		t.Fatalf("normal admission failed: %d", w.Code)
	}
	select {
	case got := <-original.starts:
		if got.id != id || got.err != nil {
			t.Fatal("accepted original startup lost identity/context")
		}
	case <-replacement.starts:
		t.Fatal("provider replacement redirected accepted startup")
	case <-time.After(time.Second):
		t.Fatal("accepted startup not dispatched")
	}
}

func TestStartAdmissionCancelledRequestDoesNotEnableOrDispatch(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	probe := &startAdmissionProbe{starts: make(chan startAdmissionObservation, 1)}
	RegisterBotManagerProvider(probe)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := callStartAdmission(id, ctx)
	if w.Code == http.StatusAccepted {
		// Drain the old implementation's unexpected asynchronous call before
		// fixture cleanup replaces the global provider.
		select {
		case <-probe.starts:
		case <-time.After(time.Second):
			t.Fatal("unexpected accepted job did not finish fixture dispatch")
		}
	}
	if w.Code != http.StatusRequestTimeout || probe.enables.Load() != 0 {
		t.Fatalf("cancelled request mutated admission: status=%d enables=%d", w.Code, probe.enables.Load())
	}
}

func TestStartAdmissionCancellationDuringEnableDoesNotDispatch(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &startAdmissionProbe{starts: make(chan startAdmissionObservation, 1), onEnable: cancel}
	RegisterBotManagerProvider(probe)
	w := callStartAdmission(id, ctx)
	if w.Code != http.StatusRequestTimeout || probe.enables.Load() != 1 {
		t.Fatalf("enable cancellation boundary lost: status=%d", w.Code)
	}
	select {
	case <-probe.starts:
		t.Fatal("cancelled enable dispatched startup")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStartAdmissionAcceptedJobSurvivesRequestCompletion(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	gate := make(chan struct{})
	probe := &startAdmissionProbe{starts: make(chan startAdmissionObservation, 1), startGate: gate}
	RegisterBotManagerProvider(probe)
	ctx, cancel := context.WithCancel(t.Context())
	w := callStartAdmission(id, ctx)
	cancel()
	close(gate)
	if w.Code != http.StatusAccepted || probe.enables.Load() != 1 {
		t.Fatalf("normal background admission failed: status=%d", w.Code)
	}
	select {
	case got := <-probe.starts:
		if got.id != id || got.err != nil {
			t.Fatal("request completion cancelled accepted background startup")
		}
	case <-time.After(time.Second):
		t.Fatal("accepted background startup not dispatched")
	}
}

func TestStartAdmissionRunningBotRemainsIdempotent(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	probe := &startAdmissionProbe{starts: make(chan startAdmissionObservation, 1), running: true}
	RegisterBotManagerProvider(probe)
	w := callStartAdmission(id, t.Context())
	if w.Code != http.StatusOK || probe.enables.Load() != 0 {
		t.Fatal("running Bot start repeated enable")
	}
	select {
	case <-probe.starts:
		t.Fatal("running Bot start dispatched duplicate")
	case <-time.After(100 * time.Millisecond):
	}
}
