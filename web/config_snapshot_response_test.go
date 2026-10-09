package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func TestGlobalConfigurationCancelledHandlersDoNotSaveOrApply(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
			before, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			next, err := cloneConfigSnapshot(before)
			if err != nil {
				t.Fatal(err)
			}
			botCfgByID(next, id).Name = "cancelled-global-http"
			body, err := yaml.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			if format == "json" {
				var values map[string]interface{}
				if err := yaml.Unmarshal(body, &values); err != nil {
					t.Fatal(err)
				}
				body, err = json.Marshal(values)
				if err != nil {
					t.Fatal(err)
				}
			}
			previous := symbolManagerProvider
			probe := &guardedReportProbe{}
			symbolManagerProvider = probe
			t.Cleanup(func() { symbolManagerProvider = previous })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body)).WithContext(ctx)
			if format == "json" {
				updateConfigHandler(c)
			} else {
				updateConfigYAMLHandler(c)
			}
			current, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Saved bool   `json:"config_saved"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusRequestTimeout || response.Saved || response.Error != "configuration_save_cancelled" || probe.calls != 0 || !reflect.DeepEqual(before, current) {
				t.Fatal("cancelled global handler saved or applied configuration")
			}
		})
	}
}

func TestConfigSnapshotSaveErrorDoesNotExposePrivateDetailsOrAssertRollback(t *testing.T) {
	for _, failure := range []error{errors.New("private-database-detail"), context.Canceled, context.DeadlineExceeded} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		respondConfigSnapshotSaveError(c, failure)
		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		_, hasSaved := response["config_saved"]
		if w.Code != http.StatusInternalServerError || hasSaved || strings.Contains(w.Body.String(), "private-database-detail") {
			t.Fatal("persistence error leaked details or asserted rollback")
		}
	}
}
