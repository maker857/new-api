package service

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func promptCacheContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	c.Request.Header.Set("X-Session-Id", "session")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func TestClaudeCacheOnlyCustomHeaderPreservesBindingTTLAndPassthrough(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	originalSetting := *setting
	originalCache := getChannelAffinityCache()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	channelAffinityCache = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
		Namespace: cachex.Namespace(channelAffinityCacheNamespace),
		Redis:     client, RedisCodec: cachex.IntCodec{},
	})
	t.Cleanup(func() { *setting = originalSetting; channelAffinityCache = originalCache; _ = client.Close() })
	rule := operation_setting.ChannelAffinityRule{
		Name: "custom-session", ModelRegex: []string{"^claude-.*$"}, IncludeRuleName: true,
		KeySources:         []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Custom-Conversation"}},
		SkipRetryOnFailure: true,
		ParamOverrideTemplate: map[string]interface{}{"operations": []map[string]interface{}{
			{"mode": "pass_headers", "value": []string{"X-Custom-Conversation"}, "keep_origin": true},
		}},
	}
	*setting = operation_setting.ChannelAffinitySetting{
		Enabled: true, ClaudeCacheOnly: true, ClaudeCacheModelRegex: "^claude-.*$", DefaultTTLSeconds: 60,
		Rules: []operation_setting.ChannelAffinityRule{
			{Name: "metadata-first", ModelRegex: []string{"^claude-.*$"}, KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "gjson", Path: "metadata.user_id"}}},
			rule,
		},
	}
	key := buildChannelAffinityCacheKeySuffix(rule, "claude-haiku-4-5", "default", "custom-session")
	fullKey := channelAffinityCacheNamespace + ":" + key
	require.NoError(t, channelAffinityCache.SetWithTTL(key, 140, time.Minute))
	server.FastForward(20 * time.Second)
	c := promptCacheContext(t, `{"messages":[{"role":"user","content":"hello"}]}`)
	c.Request.Header.Set("X-Custom-Conversation", "custom-session")
	id, found := GetPreferredChannelByAffinity(c, "claude-haiku-4-5", "default")
	assert.False(t, found)
	assert.Zero(t, id)
	assert.False(t, ShouldSkipRetryAfterChannelAffinityFailure(c))
	RecordChannelAffinity(c, 141)
	assert.Equal(t, 40*time.Second, server.TTL(fullKey), "uncached requests must not renew an existing binding")
	value, err := server.Get(fullKey)
	require.NoError(t, err)
	assert.Equal(t, "140", value)

	// Bypassing binding must still retain the custom header on upstream requests.
	merged, applied := ApplyChannelAffinityOverrideTemplate(c, nil)
	require.True(t, applied)
	info := &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{"X-Custom-Conversation": "custom-session"},
		ChannelMeta:    &relaycommon.ChannelMeta{ParamOverride: merged},
	}
	_, err = relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"claude-haiku-4-5"}`), info)
	require.NoError(t, err)
	assert.Equal(t, "custom-session", info.RuntimeHeadersOverride["x-custom-conversation"])

	next := promptCacheContext(t, `{"cache_control":{"type":"ephemeral"}}`)
	next.Request.Header.Set("X-Custom-Conversation", "custom-session")
	id, found = GetPreferredChannelByAffinity(next, "claude-haiku-4-5", "default")
	assert.True(t, found)
	assert.Equal(t, 140, id)
	RecordChannelAffinity(next, 140)
	assert.Equal(t, time.Minute, server.TTL(fullKey), "cached requests should renew the binding normally")
}

// This tests declared cache intent, not whether the provider accepts or caches the payload.
func TestRequestHasClaudePromptCache(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"absent", `{"metadata":{"user_id":"session"},"system":"plain","messages":[{"role":"user","content":"hello"}]}`, false},
		{"automatic", `{"cache_control":{"type":"ephemeral"}}`, true},
		{"system", `{"system":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral","ttl":"5m"}}]}`, true},
		{"tool", `{"tools":[{"name":"read","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`, true},
		{"message", `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"result","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"thinking marker", `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig","cache_control":{"type":"ephemeral"}}]}]}`, false},
		{"redacted thinking marker", `{"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque","cache_control":{"type":"ephemeral"}}]}]}`, false},
		{"automatic cache with thinking marker", `{"cache_control":{"type":"ephemeral"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"system cache with thinking marker", `{"system":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"tool cache with thinking marker", `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"thinking markers before explicit text cache", `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig","cache_control":{"type":"ephemeral"}},{"type":"redacted_thinking","data":"opaque","cache_control":{"type":"ephemeral"}},{"type":"text","text":"answer","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"empty system text", `{"system":[{"type":"text","text":"","cache_control":{"type":"ephemeral"}}]}`, true},
		{"empty message text", `{"messages":[{"role":"user","content":[{"type":"text","text":"","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"missing block type", `{"messages":[{"role":"user","content":[{"cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"deferred tool", `{"tools":[{"name":"lookup","defer_loading":true,"input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]}`, false},
		{"non-deferred tool", `{"tools":[{"name":"lookup","defer_loading":false,"input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]}`, true},
		{"automatic cache with deferred tool", `{"cache_control":{"type":"ephemeral"},"tools":[{"name":"lookup","defer_loading":true,"cache_control":{"type":"ephemeral"}}]}`, true},
		{"deferred tool before marked non-deferred tool", `{"tools":[{"name":"lookup","defer_loading":true,"cache_control":{"type":"ephemeral"}},{"name":"read","cache_control":{"type":"ephemeral"}}]}`, true},
		{"deferred tool with system cache", `{"tools":[{"name":"lookup","defer_loading":true,"cache_control":{"type":"ephemeral"}}],"system":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral"}}]}`, true},
		{"deferred tool with message cache", `{"tools":[{"name":"lookup","defer_loading":true,"cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"defer loading inside tool schema", `{"tools":[{"name":"lookup","input_schema":{"defer_loading":true},"cache_control":{"type":"ephemeral"}}]}`, true},
		{"inline tool definition", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"lookup","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}}}]}]}`, true},
		{"inline tool block marker", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"lookup","input_schema":{"type":"object"}}},"cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"deferred inline definition", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"lookup","defer_loading":true,"cache_control":{"type":"ephemeral"}}}}]}]}`, false},
		{"deferred inline definition before text cache", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"lookup","defer_loading":true,"cache_control":{"type":"ephemeral"}}}},{"type":"text","text":"instructions","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"inline block cache with deferred definition", `{"messages":[{"role":"system","content":[{"type":"tool_addition","cache_control":{"type":"ephemeral"},"tool":{"type":"tool_definition","definition":{"name":"lookup","defer_loading":true}}}]}]}`, true},
		{"inline definition in user data", `{"messages":[{"role":"user","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"lookup","cache_control":{"type":"ephemeral"}}}}]}]}`, false},
		{"definition inside tool input", `{"messages":[{"role":"assistant","content":[{"type":"tool_use","input":{"type":"tool_definition","definition":{"cache_control":{"type":"ephemeral"}}}}]}]}`, false},
		{"nested tool result text marker", `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":[{"type":"text","text":"result","cache_control":{"type":"ephemeral"}}]}]}]}`, false},
		{"thinking before cacheable text", `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning","signature":"sig"},{"type":"text","text":"answer","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"null", `{"cache_control":null}`, false},
		{"empty", `{"cache_control":{}}`, false},
		{"wrong type", `{"cache_control":{"type":"disabled"}}`, false},
		{"wrong ttl", `{"cache_control":{"type":"ephemeral","ttl":"1d"}}`, true},
		{"null ttl", `{"cache_control":{"type":"ephemeral","ttl":null}}`, true},
		{"nested user data", `{"metadata":{"cache_control":{"type":"ephemeral"}},"tools":[{"input_schema":{"cache_control":{"type":"ephemeral"}}}],"messages":[{"role":"assistant","content":[{"type":"tool_use","input":{"cache_control":{"type":"ephemeral"}}}]}]}`, false},
		{"message wrapper", `{"messages":[{"cache_control":{"type":"ephemeral"},"content":"hello"}]}`, false},
		{"invalid array", `{"system":{"cache_control":{"type":"ephemeral"}}}`, false},
		{"cache marker quoted in text", `{"system":"{\"cache_control\":{\"type\":\"ephemeral\"}}","messages":[{"role":"user","content":[{"type":"text","text":"{\"cache_control\":{\"type\":\"ephemeral\"}}"}]}]}`, false},
		{"document source and citations", `{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"content","content":[{"type":"text","text":"example","cache_control":{"type":"ephemeral"}}]},"citations":{"cache_control":{"type":"ephemeral"}}}]}]}`, false},
		{"toolset nested configuration", `{"tools":[{"type":"mcp_toolset","default_config":{"cache_control":{"type":"ephemeral"}},"configs":{"lookup":{"cache_control":{"type":"ephemeral"}}}}]}`, false},
		{"toolset entry marker", `{"tools":[{"type":"mcp_toolset","mcp_server_name":"calendar","cache_control":{"type":"ephemeral"}}]}`, true},
		{"inline definition wrong tool type", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_reference","definition":{"cache_control":{"type":"ephemeral"}}}}]}]}`, false},
		{"inline definition wrong block type", `{"messages":[{"role":"system","content":[{"type":"text","tool":{"type":"tool_definition","definition":{"cache_control":{"type":"ephemeral"}}}}]}]}`, false},
		{"inline definition schema only", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"input_schema":{"cache_control":{"type":"ephemeral"}}}}}]}]}`, false},
		{"inline tool wrapper marker", `{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"lookup"},"cache_control":{"type":"ephemeral"}}}]}]}`, false},
		{"literal dotted key", `{"cache_control.type":"ephemeral","messages.0.content.0.cache_control":{"type":"ephemeral"}}`, false},
		{"cache control string", `{"cache_control":"{\"type\":\"ephemeral\"}"}`, false},
		{"cache control array", `{"cache_control":[{"type":"ephemeral"}]}`, false},
		{"case sensitive cache type", `{"cache_control":{"type":"Ephemeral"}}`, false},
		{"wrong cache type shape", `{"cache_control":{"type":["ephemeral"]}}`, false},
		{"nested request wrapper", `{"request":{"cache_control":{"type":"ephemeral"},"system":[{"cache_control":{"type":"ephemeral"}}]}}`, false},
		{"nested block arrays", `{"system":[[{"cache_control":{"type":"ephemeral"}}]],"messages":[{"content":[[{"cache_control":{"type":"ephemeral"}}]]}]}`, false},
		{"invalid automatic marker then explicit marker", `{"cache_control":null,"system":[{"type":"text","text":"example","cache_control":{"type":"ephemeral"}}]}`, true},
		{"later message marker", `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":[{"type":"text","text":"second","cache_control":{}},{"type":"text","text":"third","cache_control":{"type":"ephemeral"}}]}]}`, true},
		{"malformed json", `{"cache_control":{"type":"ephemeral"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := promptCacheContext(t, tc.body)
			assert.Equal(t, tc.want, requestHasClaudePromptCache(c))
			// Inspection must leave the full payload available to the relay.
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			body, err := io.ReadAll(storage)
			require.NoError(t, err)
			assert.Equal(t, tc.body, string(body))
		})
	}
}

func TestClaudeCacheOnlyAffinity(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	original := *setting
	originalRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { *setting = original; common.RedisEnabled = originalRedis })
	for _, source := range []operation_setting.ChannelAffinityKeySource{
		{Type: "gjson", Path: "metadata.user_id"},
		{Type: "request_header", Key: "X-Session-Id"},
		{Type: "gjson", Path: "prompt_cache_key"},
	} {
		for _, tc := range []struct {
			name, model, pattern          string
			enabled, cached, wantAffinity bool
		}{
			{"uncached Claude", "claude-haiku-4-5", "^claude-.*$", true, false, false},
			{"cached Claude", "claude-haiku-4-5", "^claude-.*$", true, true, true},
			{"disabled policy", "claude-haiku-4-5", "^claude-.*$", false, false, true},
			{"GPT unchanged", "gpt-5", "^claude-.*$", true, false, true},
			{"Claude alias", "my-claude", "^(claude-.*|my-claude)$", true, false, false},
		} {
			t.Run(source.Type+source.Path+tc.name, func(t *testing.T) {
				rule := operation_setting.ChannelAffinityRule{
					Name: t.Name(), ModelRegex: []string{".*"}, KeySources: []operation_setting.ChannelAffinityKeySource{source},
					IncludeRuleName: true, IncludeModelName: true, SkipRetryOnFailure: true,
					ParamOverrideTemplate: map[string]interface{}{"temperature": 0.2},
				}
				*setting = operation_setting.ChannelAffinitySetting{
					Enabled: true, ClaudeCacheOnly: tc.enabled, ClaudeCacheModelRegex: tc.pattern,
					DefaultTTLSeconds: 60, MaxEntries: 100, Rules: []operation_setting.ChannelAffinityRule{rule},
				}
				body := `{"metadata":{"user_id":"session"},"prompt_cache_key":"session"}`
				if tc.cached {
					body = `{"metadata":{"user_id":"session"},"prompt_cache_key":"session","cache_control":{"type":"ephemeral"}}`
				}
				cache := getChannelAffinityCache()
				key := buildChannelAffinityCacheKeySuffix(rule, tc.model, "default", "session")
				t.Cleanup(func() { _, _ = cache.DeleteMany([]string{key}) })
				// A first request may create a binding only when the policy permits it.
				c := promptCacheContext(t, body)
				_, found := GetPreferredChannelByAffinity(c, tc.model, "default")
				assert.False(t, found)
				RecordChannelAffinity(c, 140)
				_, found, err := cache.Get(key)
				require.NoError(t, err)
				assert.Equal(t, tc.wantAffinity, found)
				// Existing bindings must survive uncached requests without being overwritten.
				require.NoError(t, cache.SetWithTTL(key, 140, time.Minute))
				c = promptCacheContext(t, body)
				id, found := GetPreferredChannelByAffinity(c, tc.model, "default")
				assert.Equal(t, tc.wantAffinity, found)
				if tc.wantAffinity {
					assert.Equal(t, 140, id)
				} else {
					assert.Zero(t, id)
				}
				assert.Equal(t, tc.wantAffinity, ShouldSkipRetryAfterChannelAffinityFailure(c))
				merged, applied := ApplyChannelAffinityOverrideTemplate(c, nil)
				assert.True(t, applied)
				assert.Equal(t, 0.2, merged["temperature"])
				RecordChannelAffinity(c, 141)
				id, found, err = cache.Get(key)
				require.NoError(t, err)
				assert.True(t, found)
				if tc.wantAffinity {
					assert.Equal(t, 141, id)
				} else {
					assert.Equal(t, 140, id)
					_, _, hasWriteContext := getChannelAffinityContext(c)
					assert.False(t, hasWriteContext)
					_, hasStats := GetChannelAffinityStatsContext(c)
					assert.False(t, hasStats)
					assert.False(t, ClearCurrentChannelAffinityCache(c))
					// A later cached request still returns to the original channel.
					next := promptCacheContext(t, `{"metadata":{"user_id":"session"},"prompt_cache_key":"session","cache_control":{"type":"ephemeral"}}`)
					id, found = GetPreferredChannelByAffinity(next, tc.model, "default")
					assert.True(t, found)
					assert.Equal(t, 140, id)
				}
			})
		}
	}
}
