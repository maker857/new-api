package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdvancedCustomChannelRequiresModelListRouteOnlyWhenUpdateChecksEnabled(t *testing.T) {
	inferenceRoute := dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
		Converter:    "none",
	}

	tests := []struct {
		name          string
		checksEnabled bool
		routes        []dto.AdvancedCustomRoute
		wantErr       string
	}{
		{
			name:   "legacy channel without discovery route remains valid",
			routes: []dto.AdvancedCustomRoute{inferenceRoute},
		},
		{
			name:          "enabled checks require discovery route",
			checksEnabled: true,
			routes:        []dto.AdvancedCustomRoute{inferenceRoute},
			wantErr:       dto.AdvancedCustomModelListPath,
		},
		{
			name:          "enabled checks accept discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
				{
					IncomingPath: dto.AdvancedCustomModelListPath,
					UpstreamPath: dto.AdvancedCustomModelListPath,
					Converter:    "none",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled: tt.checksEnabled,
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: tt.routes,
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestChannelDiagnosticCaptureDefaultsToExplicitlyEnabled(t *testing.T) {
	channel := &Channel{}

	channel.NormalizeDefaults()

	require.NotNil(t, channel.ChannelInfo.ErrorRewriteEnabled)
	assert.False(t, *channel.ChannelInfo.ErrorRewriteEnabled)
	assert.False(t, channel.ChannelInfo.IsErrorRewriteEnabled())
	require.NotNil(t, channel.ChannelInfo.DiagnosticCaptureEnabled)
	assert.True(t, *channel.ChannelInfo.DiagnosticCaptureEnabled)
	assert.True(t, channel.ChannelInfo.IsDiagnosticCaptureEnabled())
}

func TestChannelDiagnosticCaptureExplicitDisableIsPreserved(t *testing.T) {
	disabled := false
	channel := &Channel{ChannelInfo: ChannelInfo{DiagnosticCaptureEnabled: &disabled}}

	channel.NormalizeDefaults()

	require.NotNil(t, channel.ChannelInfo.DiagnosticCaptureEnabled)
	assert.False(t, *channel.ChannelInfo.DiagnosticCaptureEnabled)
	assert.False(t, channel.ChannelInfo.IsDiagnosticCaptureEnabled())
}

func TestChannelOptionalFeatureExplicitEnablesArePreserved(t *testing.T) {
	enabled := true
	channel := &Channel{
		ChannelInfo: ChannelInfo{
			ErrorRewriteEnabled:      &enabled,
			DiagnosticCaptureEnabled: &enabled,
		},
	}

	channel.NormalizeDefaults()

	require.NotNil(t, channel.ChannelInfo.ErrorRewriteEnabled)
	assert.True(t, *channel.ChannelInfo.ErrorRewriteEnabled)
	assert.True(t, channel.ChannelInfo.IsErrorRewriteEnabled())
	require.NotNil(t, channel.ChannelInfo.DiagnosticCaptureEnabled)
	assert.True(t, *channel.ChannelInfo.DiagnosticCaptureEnabled)
	assert.True(t, channel.ChannelInfo.IsDiagnosticCaptureEnabled())
}

func TestChannelValidateSettingsVolcTTS(t *testing.T) {
	tests := []struct {
		name    string
		config  *dto.VolcTTSConfig
		wantErr string
	}{
		{name: "empty settings preserve v1"},
		{name: "explicit v1", config: &dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV1WsBinary}},
		{
			name:    "v3 requires resource id",
			config:  &dto.VolcTTSConfig{Protocol: dto.VolcTTSProtocolV3WsUni},
			wantErr: "resource_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeVolcEngine}
			channel.SetOtherSettings(dto.ChannelOtherSettings{VolcTTS: tt.config})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
