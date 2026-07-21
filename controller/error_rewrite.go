package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetErrorRewriteRules(c *gin.Context) {
	localRules, localErr := service.ErrorRewriteRulesFromOptions()
	monitorRules, monitorErr := service.ErrorRewriteAllMonitorRulesFromDB()
	monitorStats, statsErr := service.ErrorRewriteMonitorStatsFromDB()
	if localErr != nil {
		common.ApiErrorMsg(c, localErr.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"enabled":              service.ErrorRewriteEnabled(),
			"fallback_message":     service.ErrorRewriteFallbackMessage(),
			"local_rules":          localRules.Rules,
			"monitor_rules":        monitorRules.Rules,
			"monitor_stats":        monitorStats,
			"monitor_version":      service.ErrorRewriteMonitorVersion(),
			"monitor_last_sync_at": service.ErrorRewriteMonitorLastSyncAt(),
			"monitor_last_pull_at": service.ErrorRewriteMonitorLastPullAt(),
			"monitor_error":        firstErrorString(monitorErr, statsErr),
		},
	})
}

func PullErrorRewriteMonitorRules(c *gin.Context) {
	if err := service.PullErrorRewriteMonitorRulesOnce(); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	allMonitorRules, allMonitorErr := service.ErrorRewriteAllMonitorRulesFromDB()
	monitorStats, statsErr := service.ErrorRewriteMonitorStatsFromDB()
	if allMonitorErr != nil {
		common.ApiErrorMsg(c, allMonitorErr.Error())
		return
	}
	if statsErr != nil {
		common.ApiErrorMsg(c, statsErr.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"monitor_rules":        allMonitorRules.Rules,
			"monitor_stats":        monitorStats,
			"monitor_version":      service.ErrorRewriteMonitorVersion(),
			"monitor_last_sync_at": service.ErrorRewriteMonitorLastSyncAt(),
			"monitor_last_pull_at": service.ErrorRewriteMonitorLastPullAt(),
		},
	})
}

func SyncErrorRewriteMonitorRules(c *gin.Context) {
	expectedToken := strings.TrimSpace(service.ErrorRewriteSyncToken())
	if expectedToken == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "error rewrite sync token is not configured",
		})
		return
	}
	gotToken := strings.TrimSpace(c.GetHeader("X-Error-Rewrite-Sync-Token"))
	if gotToken == "" {
		gotToken = strings.TrimSpace(c.GetHeader("X-Blacklist-Sync-Token"))
	}
	if gotToken != expectedToken {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid sync token",
		})
		return
	}

	var payload service.ErrorRewriteMonitorSyncPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		common.ApiErrorMsg(c, "invalid sync payload")
		return
	}
	rulesJSON, err := service.NormalizeErrorRewriteMonitorRules(payload.Rules)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	currentVersion := service.ErrorRewriteMonitorVersion()
	incomingVersion := payload.Version
	if incomingVersion <= 0 {
		incomingVersion = payload.SentAt
	}
	if incomingVersion > 0 && incomingVersion <= currentVersion {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "ignored stale monitor rules",
			"data": gin.H{
				"updated":         false,
				"current_version": currentVersion,
			},
		})
		return
	}

	version := incomingVersion
	if version <= 0 {
		version = currentVersion + 1
	}
	if err := service.SyncErrorRewriteMonitorRulesToDB(payload.Rules); err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("save monitor rules table failed: %s", err.Error()))
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{
		service.ErrorRewriteMonitorRulesJSONKey: rulesJSON,
		service.ErrorRewriteMonitorVersionKey:   strconv.FormatInt(version, 10),
		service.ErrorRewriteMonitorLastSyncKey:  strconv.FormatInt(time.Now().Unix(), 10),
	}); err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("save monitor rules failed: %s", err.Error()))
		return
	}
	service.ResetErrorRewriteMonitorCache()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"updated":         true,
			"current_version": version,
		},
	})
}

func firstErrorString(errs ...error) string {
	for _, err := range errs {
		if err != nil {
			return err.Error()
		}
	}
	return ""
}
