package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticCaptureChannelEnabledRequiresResolvedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, diagnosticCaptureChannelEnabled(c, 0))
}

func TestDiagnosticCaptureChannelEnabledReturnsFalseWhenChannelLookupFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	require.False(t, diagnosticCaptureChannelEnabled(c, 999999))
}

func TestDiagnosticCaptureChannelEnabledRespectsChannelSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	channelID := 123456

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	channelInfoEnabled := false
	channel := &model.Channel{
		Id: channelID,
		ChannelInfo: model.ChannelInfo{
			DiagnosticCaptureEnabled: &channelInfoEnabled,
		},
	}

	model.InitChannelCache()
	model.CacheUpdateChannel(channel)

	require.False(t, diagnosticCaptureChannelEnabled(c, channelID))

	channelInfoEnabled = true
	channel.ChannelInfo.DiagnosticCaptureEnabled = &channelInfoEnabled
	model.CacheUpdateChannel(channel)

	require.True(t, diagnosticCaptureChannelEnabled(c, channelID))
}
