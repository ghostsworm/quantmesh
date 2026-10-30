package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/mcp"
	"quantmesh/notify"
)

const (
	settingKeyMCPTokens     = "mcp_tokens"
	settingKeyMCPToken      = "mcp_token"
	settingKeyMCPAllowWrite = "mcp_allow_write"
	settingKeyMCPEnabled    = "mcp_enabled"

	mcpTokenPrefix  = "qm_mcp_"
	mcpTokenIDBytes = 6
	mcpTokenBytes   = 24
	mcpTokensMax    = 50

	mcpErrorNameRequired     = "请填写令牌名称"
	mcpErrorModeInvalid      = "权限只能是只读或读写"
	mcpErrorNotifyRequired   = "读写令牌必须指定通知方式"
	mcpErrorNotifyInvalid    = "通知方式无效"
	mcpErrorEmailInvalid     = "通知邮箱格式无效"
	mcpErrorURLInvalid       = "通知地址必须是 http 或 https"
	mcpErrorLimitReached     = "令牌数量已达上限"
	mcpErrorNotFound         = "令牌不存在"
	mcpErrorStoreUnavailable = "设置提供者未初始化"
	mcpErrorLoadFailed       = "读取令牌失败"
	mcpErrorSaveFailed       = "保存令牌失败"
	mcpErrorGenerateFailed   = "生成令牌失败"
)

var (
	mcpTokenCache   = map[string]mcp.Access{}
	mcpTokenCacheMu sync.RWMutex
	mcpNotifier     = notify.SendDirect
	mcpConfigReader = func() *config.Config { return GetConfig() }
)

// mcpTokenRecord 是持久化的令牌。SecretHash 是密钥的 SHA-256，明文只在颁发时返回一次。
type mcpTokenRecord struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Mode       string          `json:"mode"`
	Notify     *mcp.NotifySpec `json:"notify,omitempty"`
	SecretHash string          `json:"secret_hash"`
	CreatedAt  time.Time       `json:"created_at"`
	RevokedAt  *time.Time      `json:"revoked_at,omitempty"`
}

type mcpTokenStore struct {
	Tokens []mcpTokenRecord `json:"tokens"`
}

func (r mcpTokenRecord) active() bool {
	return r.RevokedAt == nil && r.SecretHash != ""
}

func (r mcpTokenRecord) access() mcp.Access {
	mode := r.Mode
	if mode != mcp.AccessReadWrite {
		mode = mcp.AccessReadOnly
	}
	notify := cloneNotify(r.Notify)
	if mode == mcp.AccessReadWrite {
		notify = nil
		if normalized, err := normalizeMCPNotify(r.Notify); err == nil {
			notify = normalized
		}
	}
	return mcp.Access{
		TokenID:    r.ID,
		Name:       r.Name,
		Mode:       mode,
		AllowWrite: mode == mcp.AccessReadWrite && notify != nil,
		Notify:     notify,
	}
}

// MCPResolveToken 给 MCP 服务做鉴权。旧的单个全局令牌继续有效，并沿用全局写开关。
func MCPResolveToken(token string) (mcp.Access, bool) {
	if strings.TrimSpace(token) == "" || systemSettingsProvider == nil {
		return mcp.Access{}, false
	}
	enabled, err := systemSettingsProvider.GetSystemSettingBool(context.Background(), settingKeyMCPEnabled, true)
	if err != nil || !enabled {
		return mcp.Access{}, false
	}

	mcpTokenCacheMu.RLock()
	cached, ok := mcpTokenCache[token]
	mcpTokenCacheMu.RUnlock()
	if ok {
		return cached, true
	}

	access, found := lookupMCPToken(token)
	if !found {
		return mcp.Access{}, false
	}
	mcpTokenCacheMu.Lock()
	mcpTokenCache[token] = access
	mcpTokenCacheMu.Unlock()
	return access, true
}

func lookupMCPToken(token string) (mcp.Access, bool) {
	store, err := loadMCPTokenStore()
	if err == nil {
		hash := hashMCPSecret(token)
		for _, record := range store.Tokens {
			if record.active() && record.SecretHash == hash {
				return record.access(), true
			}
		}
	}
	legacy, err := systemSettingsProvider.GetSystemSetting(context.Background(), settingKeyMCPToken)
	if err != nil || legacy == nil || legacy.Value == "" || legacy.Value != token {
		return mcp.Access{}, false
	}
	allowWrite, _ := systemSettingsProvider.GetSystemSettingBool(context.Background(), settingKeyMCPAllowWrite, false)
	mode := mcp.AccessReadOnly
	if allowWrite {
		mode = mcp.AccessReadWrite
	}
	return mcp.Access{
		TokenID:    "legacy",
		Name:       "旧版全局令牌",
		Mode:       mode,
		AllowWrite: allowWrite,
		Legacy:     true,
	}, true
}

func invalidateMCPTokenCache() {
	mcpTokenCacheMu.Lock()
	mcpTokenCache = map[string]mcp.Access{}
	mcpTokenCacheMu.Unlock()
}

func loadMCPTokenStore() (mcpTokenStore, error) {
	var store mcpTokenStore
	if systemSettingsProvider == nil {
		return store, errors.New(mcpErrorStoreUnavailable)
	}
	setting, err := systemSettingsProvider.GetSystemSetting(context.Background(), settingKeyMCPTokens)
	if err != nil || setting == nil || strings.TrimSpace(setting.Value) == "" {
		return store, err
	}
	if err := json.Unmarshal([]byte(setting.Value), &store); err != nil {
		return mcpTokenStore{}, fmt.Errorf("%s: %w", mcpErrorLoadFailed, err)
	}
	return store, nil
}

func saveMCPTokenStore(store mcpTokenStore) error {
	body, err := json.Marshal(store)
	if err != nil {
		return err
	}
	if err := systemSettingsProvider.SetSystemSettingString(context.Background(), settingKeyMCPTokens, string(body)); err != nil {
		return err
	}
	invalidateMCPTokenCache()
	return nil
}

type mcpTokenInput struct {
	Name   string
	Mode   string
	Notify *mcp.NotifySpec
}

func issueMCPToken(input mcpTokenInput) (mcpTokenRecord, string, error) {
	record, err := normalizeMCPTokenInput(input)
	if err != nil {
		return mcpTokenRecord{}, "", err
	}
	store, err := loadMCPTokenStore()
	if err != nil {
		return mcpTokenRecord{}, "", err
	}
	if activeMCPTokenCount(store) >= mcpTokensMax {
		return mcpTokenRecord{}, "", errors.New(mcpErrorLimitReached)
	}
	id, secret, err := newMCPCredential()
	if err != nil {
		return mcpTokenRecord{}, "", fmt.Errorf("%s: %w", mcpErrorGenerateFailed, err)
	}
	record.ID = id
	record.SecretHash = hashMCPSecret(secret)
	record.CreatedAt = time.Now().UTC()
	store.Tokens = append(store.Tokens, record)
	if err := saveMCPTokenStore(store); err != nil {
		return mcpTokenRecord{}, "", fmt.Errorf("%s: %w", mcpErrorSaveFailed, err)
	}
	logger.Info("MCP 令牌已颁发: id=%s mode=%s", record.ID, record.Mode)
	return record, secret, nil
}

func normalizeMCPTokenInput(input mcpTokenInput) (mcpTokenRecord, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return mcpTokenRecord{}, errors.New(mcpErrorNameRequired)
	}
	if len([]rune(name)) > 80 {
		return mcpTokenRecord{}, errors.New(mcpErrorNameRequired)
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	switch mode {
	case mcp.AccessReadOnly:
		return mcpTokenRecord{Name: name, Mode: mode}, nil
	case mcp.AccessReadWrite:
		notify, err := normalizeMCPNotify(input.Notify)
		if err != nil {
			return mcpTokenRecord{}, err
		}
		return mcpTokenRecord{Name: name, Mode: mode, Notify: notify}, nil
	default:
		return mcpTokenRecord{}, errors.New(mcpErrorModeInvalid)
	}
}

func normalizeMCPNotify(spec *mcp.NotifySpec) (*mcp.NotifySpec, error) {
	if spec == nil {
		return nil, errors.New(mcpErrorNotifyRequired)
	}
	channel := strings.ToLower(strings.TrimSpace(spec.Channel))
	target := strings.TrimSpace(spec.Target)
	switch channel {
	case mcp.NotifyEmail:
		if _, err := mail.ParseAddress(target); err != nil {
			return nil, errors.New(mcpErrorEmailInvalid)
		}
	case mcp.NotifyTelegram:
		if target != "" && !validTelegramChatID(target) {
			return nil, errors.New(mcpErrorNotifyInvalid)
		}
	case mcp.NotifyWebhook, mcp.NotifyFeishu, mcp.NotifyDingTalk, mcp.NotifyWeChatWork, mcp.NotifySlack:
		if channel == mcp.NotifyWebhook && target == "" {
			return nil, errors.New(mcpErrorURLInvalid)
		}
		if target != "" && !validNotifyURL(target) {
			return nil, errors.New(mcpErrorURLInvalid)
		}
	default:
		return nil, errors.New(mcpErrorNotifyInvalid)
	}
	return &mcp.NotifySpec{Channel: channel, Target: target}, nil
}

func validTelegramChatID(v string) bool {
	if v == "" || len(v) > 32 {
		return false
	}
	for i, r := range v {
		if r == '-' && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validNotifyURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func revokeMCPToken(id string) error {
	store, err := loadMCPTokenStore()
	if err != nil {
		return errors.New(mcpErrorNotFound)
	}
	now := time.Now().UTC()
	found := false
	for i := range store.Tokens {
		if store.Tokens[i].ID == id && store.Tokens[i].active() {
			store.Tokens[i].RevokedAt = &now
			found = true
			break
		}
	}
	if !found {
		return errors.New(mcpErrorNotFound)
	}
	if err := saveMCPTokenStore(store); err != nil {
		return fmt.Errorf("%s: %w", mcpErrorSaveFailed, err)
	}
	logger.Info("MCP 令牌已吊销: id=%s", id)
	return nil
}

func activeMCPTokens() ([]mcpTokenRecord, error) {
	store, err := loadMCPTokenStore()
	if err != nil {
		return nil, err
	}
	out := make([]mcpTokenRecord, 0, len(store.Tokens))
	for _, record := range store.Tokens {
		if record.active() {
			out = append(out, record)
		}
	}
	return out, nil
}

func activeMCPTokenCount(store mcpTokenStore) int {
	n := 0
	for _, record := range store.Tokens {
		if record.active() {
			n++
		}
	}
	return n
}

func clearLegacyMCPToken() error {
	if err := systemSettingsProvider.SetSystemSettingString(context.Background(), settingKeyMCPToken, ""); err != nil {
		return err
	}
	invalidateMCPTokenCache()
	return nil
}

func legacyMCPTokenStatus() (bool, string) {
	setting, err := systemSettingsProvider.GetSystemSetting(context.Background(), settingKeyMCPToken)
	if err != nil || setting == nil || setting.Value == "" {
		return false, ""
	}
	return true, maskAPIKey(setting.Value)
}

func newMCPCredential() (string, string, error) {
	idRaw := make([]byte, mcpTokenIDBytes)
	secretRaw := make([]byte, mcpTokenBytes)
	if _, err := rand.Read(idRaw); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(secretRaw); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(idRaw), mcpTokenPrefix + hex.EncodeToString(secretRaw), nil
}

func hashMCPSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func cloneNotify(in *mcp.NotifySpec) *mcp.NotifySpec {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// notifyMCPWrites 把一次请求里成功的写操作发到令牌指定的渠道。
// 只读令牌不会进入这里；通知失败不影响已经完成的修改，但必须留下日志。
func NotifyMCPWrites(_ context.Context, notice mcp.WriteNotice) {
	if notice.Access.Notify == nil || len(notice.Calls) == 0 {
		return
	}
	err := mcpNotifier(mcpConfigReader(), notice.Access.Notify.Channel, notice.Access.Notify.Target,
		"QuantMesh MCP 写操作已完成", notice.Text())
	if err != nil {
		logger.Warn("MCP 写操作通知失败: token=%s channel=%s err=%v", notice.Access.TokenID, notice.Access.Notify.Channel, err)
	}
}
