package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsInvalidClaudeCacheModelRegex(t *testing.T) {
	for _, value := range []string{"", "[", "(?=claude)"} {
		t.Run(value, func(t *testing.T) {
			payload, err := common.Marshal(OptionUpdateRequest{Key: "channel_affinity_setting.claude_cache_model_regex", Value: value})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("PUT", "/api/option/", strings.NewReader(string(payload)))
			UpdateOption(c)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Contains(t, response.Message, "正则")
		})
	}
}
