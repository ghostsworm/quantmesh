package web

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// BotConfigurationCoordinator serializes destructive recovery-config changes
// against Bot lifecycle transitions. Missing coordination is not safe admission.
type BotConfigurationCoordinator interface {
	WithBotConfigurationLock(botID string, persist func() error) error
}

var ErrBotConfigRuntimeManaged = errors.New("Bot recovery configuration belongs to a managed runtime")

func protectBotConfigMutation(c *gin.Context, botID, runningErrorKey string, persist func()) {
	provider := botManagerProvider()
	coordinator, ok := provider.(BotConfigurationCoordinator)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bot_config_lifecycle_coordination_unavailable"})
		return
	}
	failureKind := ""
	err := coordinator.WithBotConfigurationLock(botID, func() error {
		// Keep the provider-level check for adapters, but do it inside the lock.
		if bot, found := provider.GetBot(botID); found && bot.Running {
			return ErrBotConfigRuntimeManaged
		}
		reserved, err := botHasAccountWalletCapitalReservation(c.Request.Context(), botID)
		if err != nil {
			failureKind = "reservation_verification"
			return err
		}
		if reserved {
			failureKind = "reservation_retained"
			return errors.New("Bot recovery configuration still has wallet capital reservations")
		}
		if err := verifyBotRecoveryConfiguration(c.Request.Context(), botID); err != nil {
			failureKind = "recovery_configuration"
			return err
		}
		if err := c.Request.Context().Err(); err != nil {
			return err
		}
		persist()
		return nil
	})
	if err == nil {
		return
	}
	if errors.Is(err, ErrBotConfigRuntimeManaged) {
		c.JSON(http.StatusConflict, gin.H{"error": "bot_running", "error_key": runningErrorKey})
		return
	}
	switch failureKind {
	case "recovery_configuration":
		respondRecoveryConfigurationError(c, err)
	case "reservation_retained":
		c.JSON(http.StatusConflict, gin.H{"error": "bot_capital_reservation_not_released"})
	case "reservation_verification":
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bot_capital_reservation_verification_unavailable"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bot_config_lifecycle_coordination_unavailable"})
	}
}
