package web

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

var errConfigSnapshotChanged = errors.New("configuration snapshot changed before persistence")
var errConfigSaveCancelledBeforePersistence = errors.New("request cancelled before configuration persistence")

func (fcm *FileConfigManager) updateConfigFromSnapshot(ctx context.Context, baseline, next *config.Config, source string) error {
	if fcm == nil || ctx == nil || baseline == nil || next == nil {
		return errors.New("configuration manager, context and snapshots are required")
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errConfigSaveCancelledBeforePersistence, err)
	}
	snapshot, err := cloneConfigSnapshot(next)
	if err != nil {
		return err
	}
	err = withGlobalRecoveryConfiguration(ctx, baseline, snapshot, func() error {
		return fcm.updateConfigUsingWithBotHistorySource(func(current *config.Config) error {
			if err := ctx.Err(); err != nil {
				return errors.Join(errConfigSaveCancelledBeforePersistence, err)
			}
			if !reflect.DeepEqual(current, baseline) {
				return errConfigSnapshotChanged
			}
			*current = *snapshot
			return nil
		}, source, false)
	})
	if err == nil {
		notifyEquityScopeConfigSync(snapshot)
		notifyNewsMonitorRuntimeSync(snapshot)
	}
	return err
}

func respondConfigSnapshotSaveError(c *gin.Context, err error) {
	var admission *globalRecoveryAdmissionError
	if errors.As(err, &admission) {
		c.JSON(admission.status, gin.H{"error": admission.code, "config_saved": false})
		return
	}
	if errors.Is(err, errConfigSnapshotChanged) {
		c.JSON(http.StatusConflict, gin.H{"error": "configuration_changed", "config_saved": false})
		return
	}
	if errors.Is(err, errConfigSaveCancelledBeforePersistence) {
		c.JSON(http.StatusRequestTimeout, gin.H{"error": "configuration_save_cancelled", "config_saved": false})
		return
	}
	// A persistence error may represent partial durable writes; do not assert
	// config_saved=false or expose database/provider details in that case.
	c.JSON(http.StatusInternalServerError, gin.H{"error": "configuration_save_failed"})
}
