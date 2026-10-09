package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"quantmesh/storage"
)

const (
	constructorWireHeaderBytes    = 4
	constructorWireMaxPacketBytes = 1 << 20
	constructorWireQueryCommand   = byte(3)
	constructorWirePrepareCommand = byte(0x16)
	constructorWireOK             = byte(0)
	constructorWireTimeout        = 5 * time.Second
)

// Credential-free loopback fixture only. Packets are forwarded, never logged.
// Interrupt either before forwarding COMMIT, or after MySQL returned its OK
// but before the real Go driver can observe that acknowledgement.
type constructorMySQLWireFault struct {
	listener         net.Listener
	upstream         string
	mode             string
	armed            atomic.Bool
	hits             atomic.Int32
	commitOK         atomic.Int32
	commitErr        atomic.Int32
	mu               sync.Mutex
	connections      map[net.Conn]net.Conn
	commitConnID     string
	probeConnID      string
	writerReadConnID string
	acceptDone       chan struct{}
	workers          sync.WaitGroup
	closeOnce        sync.Once
	store            *storage.SQLStorage
	afterCommitHook  func() error
	hookResult       chan error
}

func newConstructorMySQLWireFault(t *testing.T, bm *BotManager, mode string) *constructorMySQLWireFault {
	t.Helper()
	constructorMySQLConfig(t)
	cfg, err := mysql.ParseDSN(bm.cfg.Storage.Path)
	if err != nil {
		t.Fatal("wire fixture requires valid isolated DSN")
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || cfg.Net != "tcp" || host != "127.0.0.1" || cfg.User != "root" || cfg.Passwd != "" || cfg.TLSConfig != "" || !strings.HasPrefix(cfg.DBName, "quantmesh_ctor_") {
		t.Fatal("wire fixture requires credential-free loopback constructor schema")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &constructorMySQLWireFault{listener: listener, upstream: cfg.Addr, mode: mode, connections: make(map[net.Conn]net.Conn), acceptDone: make(chan struct{})}
	t.Cleanup(p.close)
	go p.accept()
	cfg.Addr = listener.Addr().String()
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = constructorWireTimeout, constructorWireTimeout, constructorWireTimeout
	p.store, err = storage.NewMySQLStorage(cfg.FormatDSN())
	if err != nil {
		t.Fatal("initialize isolated wire SQL store", err)
	}
	t.Cleanup(func() {
		if err := p.store.Close(); err != nil {
			t.Error("close wire SQL store", err)
		}
	})
	return p
}

func (p *constructorMySQLWireFault) accept() {
	defer close(p.acceptDone)
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		server, err := net.DialTimeout("tcp", p.upstream, constructorWireTimeout)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		p.connections[client] = server
		p.mu.Unlock()
		p.workers.Add(1)
		go func() {
			defer p.workers.Done()
			p.forward(client, server)
			p.mu.Lock()
			delete(p.connections, client)
			p.mu.Unlock()
		}()
	}
}

func (p *constructorMySQLWireFault) forward(client, server net.Conn) {
	var awaitingCommitOK atomic.Bool
	var pumps sync.WaitGroup
	pumps.Add(2)
	go func() {
		defer pumps.Done()
		defer client.Close()
		defer server.Close()
		for {
			packet, err := readConstructorWirePacket(client)
			if err != nil {
				return
			}
			query := ""
			if len(packet) > constructorWireHeaderBytes && (packet[constructorWireHeaderBytes] == constructorWireQueryCommand || packet[constructorWireHeaderBytes] == constructorWirePrepareCommand) {
				query = strings.TrimSpace(string(packet[constructorWireHeaderBytes+1:]))
			}
			if p.commitErr.Load() > 0 && strings.HasPrefix(strings.ToUpper(query), "SELECT G.SCOPE_KEY") {
				p.mu.Lock()
				p.probeConnID = client.RemoteAddr().String()
				p.mu.Unlock()
			}
			if p.commitErr.Load() > 0 && strings.Contains(strings.ToUpper(query), "FROM STRATEGY_RUNTIME_STATES") && !strings.HasPrefix(strings.ToUpper(query), "SELECT G.SCOPE_KEY") {
				p.mu.Lock()
				p.writerReadConnID = client.RemoteAddr().String()
				p.mu.Unlock()
			}
			isCommit := strings.EqualFold(query, "COMMIT")
			if isCommit && p.armed.CompareAndSwap(true, false) {
				p.mu.Lock()
				p.commitConnID = client.RemoteAddr().String()
				p.mu.Unlock()
				if strings.HasSuffix(p.mode, "_before_commit") {
					p.hits.Add(1)
					return
				}
				if strings.HasSuffix(p.mode, "_commit_err_uncommitted") {
					p.hits.Add(1)
					p.commitErr.Add(1)
					if err := writeConstructorMySQLCommitError(client); err != nil {
						return
					}
					continue
				}
				awaitingCommitOK.Store(true)
			}
			if _, err := io.Copy(server, bytes.NewReader(packet)); err != nil {
				return
			}
		}
	}()
	go func() {
		defer pumps.Done()
		defer client.Close()
		defer server.Close()
		for {
			packet, err := readConstructorWirePacket(server)
			if err != nil {
				return
			}
			if awaitingCommitOK.Swap(false) {
				if len(packet) > constructorWireHeaderBytes && packet[constructorWireHeaderBytes] == constructorWireOK {
					p.commitOK.Add(1)
					p.hits.Add(1)
					if p.afterCommitHook != nil {
						p.hookResult <- p.afterCommitHook()
					}
				}
				return
			}
			if _, err := io.Copy(client, bytes.NewReader(packet)); err != nil {
				return
			}
		}
	}()
	pumps.Wait()
}

func writeConstructorMySQLCommitError(conn net.Conn) error {
	message := []byte("injected COMMIT error; upstream transaction intentionally remains open")
	payload := make([]byte, 1+2+1+5+len(message))
	payload[0] = 0xff                   // MySQL ERR packet
	payload[1], payload[2] = 0xb5, 0x04 // ER_LOCK_WAIT_TIMEOUT (1205)
	payload[3] = '#'
	copy(payload[4:9], "HY000")
	copy(payload[9:], message)
	packet := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), 1}
	packet = append(packet, payload...)
	_, err := conn.Write(packet)
	return err
}

func TestConstructorMySQLCommitErrorPacketUsesStandardErrFrame(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	writeResult := make(chan error, 1)
	go func() { writeResult <- writeConstructorMySQLCommitError(server) }()
	packet, err := readConstructorWirePacket(client)
	if err != nil {
		t.Fatal("read synthetic MySQL ERR packet", err)
	}
	if err := <-writeResult; err != nil {
		t.Fatal("write synthetic MySQL ERR packet", err)
	}
	if len(packet) < constructorWireHeaderBytes+9 || packet[3] != 1 || packet[4] != 0xff || packet[5] != 0xb5 || packet[6] != 0x04 || packet[7] != '#' || string(packet[8:13]) != "HY000" {
		t.Fatalf("synthetic COMMIT error packet has invalid MySQL ERR framing: %v", packet)
	}
}

func readConstructorWirePacket(conn net.Conn) ([]byte, error) {
	header := make([]byte, constructorWireHeaderBytes)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if length > constructorWireMaxPacketBytes {
		return nil, fmt.Errorf("fixture packet exceeds bounded size")
	}
	packet := make([]byte, constructorWireHeaderBytes+length)
	copy(packet, header)
	_, err := io.ReadFull(conn, packet[constructorWireHeaderBytes:])
	return packet, err
}

func (p *constructorMySQLWireFault) close() {
	p.closeOnce.Do(func() {
		_ = p.listener.Close()
		<-p.acceptDone
		p.mu.Lock()
		for client, server := range p.connections {
			_ = client.Close()
			_ = server.Close()
		}
		p.mu.Unlock()
		p.workers.Wait()
	})
}

func TestMySQLFundingCarryFullConstructorCapitalCommitWireInterruptionRecoversWithoutFinancialReplay(t *testing.T) {
	constructorMySQLConfig(t)
	testFundingCarryConstructorFinalVerificationWithFaults(t, newConstructorMySQLStorage, []string{"capital_wire_before_commit", "capital_wire_after_commit"})
}

func TestMySQLFundingCarryRuntimeGenerationAdapterCommitOutcomeRecovery(t *testing.T) {
	constructorMySQLConfig(t)
	for _, mode := range []string{
		"save_before_commit", "save_after_commit_ack_lost", "save_owner_takeover",
		"save_commit_err_uncommitted", "save_after_commit_ack_lost_cancelled", "cas_before_commit", "cas_after_commit_ack_lost", "cas_owner_takeover",
		"cas_commit_err_uncommitted", "cas_after_commit_ack_lost_cancelled",
	} {
		t.Run(mode, func(t *testing.T) {
			bm := newConstructorMySQLStorage(t)
			fault := newConstructorMySQLWireFault(t, bm, mode)
			proxyDSN, err := mysql.ParseDSN(bm.cfg.Storage.Path)
			if err != nil {
				t.Fatal("parse isolated MySQL DSN", err)
			}
			proxyDSN.Addr = fault.listener.Addr().String()
			proxyConfig := *bm.cfg
			proxyConfig.Storage = bm.cfg.Storage
			proxyConfig.Storage.Path = proxyDSN.FormatDSN()
			proxyService, err := storage.NewStorageService(&proxyConfig, t.Context())
			if err != nil {
				t.Fatal("initialize production StorageService through wire proxy", err)
			}
			t.Cleanup(proxyService.Stop)

			directStore, err := storage.NewMySQLStorage(bm.cfg.Storage.Path)
			if err != nil {
				t.Fatal("initialize direct MySQL owner store", err)
			}
			t.Cleanup(func() {
				if err := directStore.Close(); err != nil {
					t.Error("close direct MySQL owner store", err)
				}
			})
			generationStore := storage.FundingCarryRuntimeGenerationStore(directStore)
			scopes := []string{fmt.Sprintf("%064x", 41), fmt.Sprintf("%064x", 42)}
			owner, err := generationStore.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
			if err != nil {
				t.Fatal("claim initial direct DB owner", err)
			}
			adapter := &fundingCarryRuntimeStateAdapter{
				strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: proxyService, botID: "wire-runtime-owner"},
				generation:                  owner,
			}
			initial := `{"state":"initial"}`
			if err := adapter.SaveRuntimeState("funding_carry", 7, initial); err != nil {
				t.Fatal("save initial runtime state through production adapter", err)
			}

			isCAS := strings.HasPrefix(mode, "cas_")
			next := `{"state":"committed"}`
			operationCtx := t.Context()
			var cancelOperation context.CancelFunc
			if mode == "cas_after_commit_ack_lost_cancelled" || mode == "save_after_commit_ack_lost_cancelled" {
				operationCtx, cancelOperation = context.WithCancel(t.Context())
				defer cancelOperation()
				fault.hookResult = make(chan error, 1)
				fault.afterCommitHook = func() error {
					cancelOperation()
					return nil
				}
			}
			if strings.HasSuffix(mode, "owner_takeover") {
				fault.hookResult = make(chan error, 1)
				fault.afterCommitHook = func() error {
					_, claimErr := generationStore.ClaimFundingCarryRuntimeGeneration(t.Context(), scopes)
					return claimErr
				}
			}
			fault.armed.Store(true)
			var operationErr error
			var saved bool
			if isCAS {
				saved, operationErr = adapter.CompareAndSwapRuntimeState(operationCtx, "funding_carry", 7, initial, 8, next)
			} else if mode == "save_after_commit_ack_lost_cancelled" {
				contextWriter, ok := any(adapter).(interface {
					SaveRuntimeStateContext(context.Context, string, int, string) error
				})
				if !ok {
					t.Fatal("production FundingCarry adapter does not expose context-aware runtime writes")
				}
				operationErr = contextWriter.SaveRuntimeStateContext(operationCtx, "funding_carry", 8, next)
			} else {
				operationErr = adapter.SaveRuntimeState("funding_carry", 8, next)
			}
			if hits := fault.hits.Load(); hits != 1 {
				t.Fatalf("wire fault hits = %d, want exactly one COMMIT interruption", hits)
			}
			if strings.HasSuffix(mode, "owner_takeover") {
				if err := <-fault.hookResult; err != nil {
					t.Fatal("direct DB owner takeover after server COMMIT OK", err)
				}
			}
			if strings.HasSuffix(mode, "commit_err_uncommitted") {
				writerState, readErr := proxyService.GetStorage().(storage.StrategyRuntimeStateStore).GetStrategyRuntimeState("wire-runtime-owner", "funding_carry")
				fault.mu.Lock()
				commitConnID, writerReadConnID, probeConnID := fault.commitConnID, fault.writerReadConnID, fault.probeConnID
				fault.mu.Unlock()
				if readErr != nil || writerState == nil || writerState.Payload != next || commitConnID == "" || writerReadConnID != commitConnID || probeConnID == "" || probeConnID == commitConnID {
					t.Fatalf("injected MySQL ERR did not leave the original uncommitted transaction visible only on its writer session: state=%+v err=%v commitConn=%q writerReadConn=%q probeConn=%q", writerState, readErr, commitConnID, writerReadConnID, probeConnID)
				}
			}

			state, err := directStore.GetStrategyRuntimeState("wire-runtime-owner", "funding_carry")
			if err != nil || state == nil {
				t.Fatalf("read committed runtime state: state=%+v err=%v", state, err)
			}
			if strings.HasSuffix(mode, "before_commit") || strings.HasSuffix(mode, "commit_err_uncommitted") {
				if !errors.Is(operationErr, storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown) || state.Payload != initial || (isCAS && saved) || fault.commitOK.Load() != 0 {
					t.Fatalf("non-committed COMMIT fault result saved=%v err=%v state=%q; want uncertainty and unchanged committed state", saved, operationErr, state.Payload)
				}
				if strings.HasSuffix(mode, "commit_err_uncommitted") {
					if fault.commitErr.Load() != 1 {
						t.Fatalf("synthetic MySQL ERR packets = %d, want exactly one", fault.commitErr.Load())
					}
				}
				return
			}
			if fault.commitOK.Load() != 1 {
				t.Fatalf("server COMMIT OK observed %d times, want exactly once", fault.commitOK.Load())
			}
			if strings.HasSuffix(mode, "owner_takeover") {
				if !errors.Is(operationErr, storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown) || state.Payload != next || (isCAS && saved) {
					t.Fatalf("owner takeover result saved=%v err=%v state=%q; want uncertain result and committed payload", saved, operationErr, state.Payload)
				}
				return
			}
			if mode == "cas_after_commit_ack_lost_cancelled" || mode == "save_after_commit_ack_lost_cancelled" {
				if hookErr := <-fault.hookResult; hookErr != nil {
					t.Fatal("cancel caller after server COMMIT OK", hookErr)
				}
				if !errors.Is(operationErr, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) ||
					!errors.Is(operationErr, context.Canceled) || (isCAS && saved) || state.SchemaVersion != 8 || state.Payload != next {
					t.Fatalf("cancelled committed runtime write = saved %v err %v state=%+v; want confirmed-canceled error and durable target", saved, operationErr, state)
				}
				return
			}
			if operationErr != nil || (isCAS && !saved) || state.SchemaVersion != 8 || state.Payload != next {
				t.Fatalf("lost-ACK recovery result saved=%v err=%v state=%+v; want positively confirmed state", saved, operationErr, state)
			}
		})
	}
}

func newConstructorCapitalCommitFailure(t *testing.T, bm *BotManager, mode string) *constructorCapitalCommitFailure {
	t.Helper()
	fault := &constructorCapitalCommitFailure{mode: mode}
	if strings.HasPrefix(mode, "capital_wire_") {
		fault.wire = newConstructorMySQLWireFault(t, bm, mode)
	}
	return fault
}

func (p *constructorMySQLWireFault) release(ctx context.Context, botID string, claims []storage.AccountWalletCapitalClaim) error {
	p.armed.Store(true)
	return p.store.ReleaseAccountWalletCapital(ctx, botID, claims)
}
