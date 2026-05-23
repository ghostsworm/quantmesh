package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

const configMigrationSchemaVersion = 1

type configMigrationBundle struct {
	SchemaVersion     int                              `json:"schema_version"`
	AppVersion        string                           `json:"app_version,omitempty"`
	ExportedAt        string                           `json:"exported_at"`
	RuntimeConfig     *config.Config                   `json:"runtime_config,omitempty"`
	RuntimeConfigYAML string                           `json:"runtime_config_yaml,omitempty"`
	Database          *configMigrationDatabaseSnapshot `json:"database,omitempty"`
}

type configMigrationDatabaseSnapshot struct {
	AppConfig  *configMigrationAppConfigDocument  `json:"app_config,omitempty"`
	BotConfigs []configMigrationBotConfigDocument `json:"bot_configs,omitempty"`
}

type configMigrationAppConfigDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Content       json.RawMessage `json:"content"`
	Revision      int             `json:"revision,omitempty"`
	ContentHash   string          `json:"content_hash,omitempty"`
	UpdatedAt     string          `json:"updated_at,omitempty"`
}

type configMigrationBotConfigDocument struct {
	BotID         string          `json:"bot_id"`
	SchemaVersion int             `json:"schema_version"`
	Content       json.RawMessage `json:"content"`
	Revision      int             `json:"revision,omitempty"`
	ContentHash   string          `json:"content_hash,omitempty"`
	UpdatedAt     string          `json:"updated_at,omitempty"`
}

type configMigrationImportResult struct {
	Message               string   `json:"message"`
	AppConfigRevision     int      `json:"app_config_revision,omitempty"`
	ImportedBotConfigs    int      `json:"imported_bot_configs"`
	WrittenBotConfigFiles int      `json:"written_bot_config_files"`
	RequiresRestart       bool     `json:"requires_restart"`
	HotUpdated            []string `json:"hot_updated,omitempty"`
}

func exportConfigMigrationHandler(c *gin.Context) {
	bundle, err := buildConfigMigrationBundle(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化迁移包失败: " + err.Error()})
		return
	}
	filename := "quantmesh_config_migration_" + time.Now().Format("20060102_150405") + ".json"
	serveExport(c, data, "application/json", filename)
}

func importConfigMigrationHandler(c *gin.Context) {
	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取迁移包失败: " + err.Error()})
		return
	}
	var bundle configMigrationBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解析迁移包失败: " + err.Error()})
		return
	}
	result, err := applyConfigMigrationBundle(c.Request.Context(), &bundle)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func buildConfigMigrationBundle(ctx context.Context) (*configMigrationBundle, error) {
	bundle := &configMigrationBundle{
		SchemaVersion: configMigrationSchemaVersion,
		AppVersion:    normalizedAppVersion(),
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Database:      &configMigrationDatabaseSnapshot{},
	}
	if fileConfigManager != nil {
		if cfg, err := fileConfigManager.GetConfig(); err == nil && cfg != nil {
			bundle.RuntimeConfig = cfg
			if data, err := yaml.Marshal(cfg); err == nil {
				bundle.RuntimeConfigYAML = string(data)
			}
		}
	}
	ss := sqlStorageForBotConfig()
	if ss == nil {
		return bundle, nil
	}
	if err := ss.EnsureAppConfigDocumentTables(); err != nil {
		return nil, err
	}
	appDoc, err := ss.GetAppConfigDocument(ctx)
	if err != nil {
		return nil, err
	}
	if appDoc != nil && strings.TrimSpace(appDoc.Content) != "" {
		raw, err := rawJSONFromString(appDoc.Content)
		if err != nil {
			return nil, fmt.Errorf("app_config 不是有效 JSON: %w", err)
		}
		bundle.Database.AppConfig = &configMigrationAppConfigDocument{
			SchemaVersion: appDoc.SchemaVersion,
			Content:       raw,
			Revision:      appDoc.Revision,
			ContentHash:   appDoc.ContentHash,
			UpdatedAt:     formatMigrationTime(appDoc.UpdatedAt),
		}
	}
	botDocs, err := ss.ListBotConfigDocuments(ctx)
	if err != nil {
		return nil, err
	}
	for _, doc := range botDocs {
		if doc == nil || strings.TrimSpace(doc.BotID) == "" || strings.TrimSpace(doc.Content) == "" {
			continue
		}
		raw, err := rawJSONFromString(doc.Content)
		if err != nil {
			return nil, fmt.Errorf("bot_configs[%s] 不是有效 JSON: %w", doc.BotID, err)
		}
		bundle.Database.BotConfigs = append(bundle.Database.BotConfigs, configMigrationBotConfigDocument{
			BotID:         doc.BotID,
			SchemaVersion: doc.SchemaVersion,
			Content:       raw,
			Revision:      doc.Revision,
			ContentHash:   doc.ContentHash,
			UpdatedAt:     formatMigrationTime(doc.UpdatedAt),
		})
	}
	if bundle.Database.AppConfig == nil && len(bundle.Database.BotConfigs) == 0 {
		bundle.Database = nil
	}
	return bundle, nil
}

func applyConfigMigrationBundle(ctx context.Context, bundle *configMigrationBundle) (*configMigrationImportResult, error) {
	if bundle == nil {
		return nil, fmt.Errorf("迁移包为空")
	}
	if bundle.SchemaVersion != 0 && bundle.SchemaVersion != configMigrationSchemaVersion {
		return nil, fmt.Errorf("不支持的迁移包版本: %d", bundle.SchemaVersion)
	}
	appJSON, cfg, err := resolveMigrationAppConfig(bundle)
	if err != nil {
		return nil, err
	}
	if cfg == nil || len(appJSON) == 0 {
		return nil, fmt.Errorf("迁移包缺少主配置")
	}

	oldConfig, _ := fileConfigManager.GetConfig()
	diff := config.DiffConfig(oldConfig, cfg)

	if primaryStorageForAppConfig == nil {
		return nil, fmt.Errorf("主库未初始化，无法导入数据库配置")
	}
	appRev, err := storage.SaveAppConfigSnapshotFromJSON(ctx, primaryStorageForAppConfig, appJSON, "web", "config_migration_import")
	if err != nil {
		return nil, fmt.Errorf("写入 app_config 失败: %w", err)
	}

	importedBots, writtenFiles, err := importMigrationBotConfigs(ctx, bundle)
	if err != nil {
		return nil, err
	}

	fileConfigManager.SetRuntimeConfig(cfg)
	notifyNewsMonitorRuntimeSync(cfg)
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(cfg)
	}

	var updatedSymbols []string
	if symbolManagerProvider != nil {
		if updater, ok := symbolManagerProvider.(TradingParamsUpdater); ok {
			updatedSymbols = updater.UpdateTradingParams(cfg)
		}
	}

	return &configMigrationImportResult{
		Message:               "配置迁移导入成功",
		AppConfigRevision:     appRev,
		ImportedBotConfigs:    importedBots,
		WrittenBotConfigFiles: writtenFiles,
		RequiresRestart:       diff.RequiresRestart,
		HotUpdated:            updatedSymbols,
	}, nil
}

func resolveMigrationAppConfig(bundle *configMigrationBundle) ([]byte, *config.Config, error) {
	if bundle.Database != nil && bundle.Database.AppConfig != nil && len(bundle.Database.AppConfig.Content) > 0 {
		raw := []byte(bundle.Database.AppConfig.Content)
		cfg, err := config.LoadConfigFromJSON(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("解析 app_config 失败: %w", err)
		}
		return raw, cfg, nil
	}
	if bundle.RuntimeConfig != nil {
		if err := bundle.RuntimeConfig.Validate(); err != nil {
			return nil, nil, fmt.Errorf("运行时配置验证失败: %w", err)
		}
		raw, err := json.Marshal(bundle.RuntimeConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("序列化运行时配置失败: %w", err)
		}
		return raw, bundle.RuntimeConfig, nil
	}
	if strings.TrimSpace(bundle.RuntimeConfigYAML) != "" {
		cfg, err := config.LoadConfigFromBytes([]byte(bundle.RuntimeConfigYAML))
		if err != nil {
			return nil, nil, fmt.Errorf("解析运行时 YAML 配置失败: %w", err)
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("序列化运行时 YAML 配置失败: %w", err)
		}
		return raw, cfg, nil
	}
	return nil, nil, nil
}

func importMigrationBotConfigs(ctx context.Context, bundle *configMigrationBundle) (int, int, error) {
	if bundle.Database == nil || len(bundle.Database.BotConfigs) == 0 {
		return 0, 0, nil
	}
	imported := 0
	writtenFiles := 0
	for _, doc := range bundle.Database.BotConfigs {
		if strings.TrimSpace(doc.BotID) == "" || len(doc.Content) == 0 {
			continue
		}
		var bf config.BotConfigFile
		if err := json.Unmarshal(doc.Content, &bf); err != nil {
			return imported, writtenFiles, fmt.Errorf("解析 bot_configs[%s] 失败: %w", doc.BotID, err)
		}
		if strings.TrimSpace(bf.BotID) == "" {
			bf.BotID = doc.BotID
		}
		if bf.BotID != doc.BotID {
			return imported, writtenFiles, fmt.Errorf("bot_configs[%s] 内容 bot_id 不一致: %s", doc.BotID, bf.BotID)
		}
		if _, err := storage.SaveBotConfigSnapshot(ctx, primaryStorageForAppConfig, &bf, "web", "config_migration_import"); err != nil {
			return imported, writtenFiles, fmt.Errorf("写入 bot_configs[%s] 失败: %w", doc.BotID, err)
		}
		imported++
		if botConfigManager != nil {
			if err := botConfigManager.SaveBotConfig(&bf); err != nil {
				return imported, writtenFiles, fmt.Errorf("写入 Bot 文件配置[%s] 失败: %w", doc.BotID, err)
			}
			writtenFiles++
		}
	}
	return imported, writtenFiles, nil
}

func rawJSONFromString(s string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, nil
	}
	raw := json.RawMessage([]byte(trimmed))
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid json")
	}
	return raw, nil
}

func formatMigrationTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
