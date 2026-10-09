package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// requestHasClaudePromptCache detects cache intent before routing, not cache hits
// reported after a response. Only protocol-defined locations are inspected,
// including inline tool definitions; arbitrary tool inputs are never traversed.
func requestHasClaudePromptCache(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return false
	}
	body, err := storage.Bytes()
	if err != nil || !gjson.ValidBytes(body) {
		return false
	}
	root := gjson.ParseBytes(body)
	if claudeCacheControlEnabled(root.Get("cache_control")) {
		return true
	}
	for _, field := range []string{"tools", "system"} {
		blocks := root.Get(field)
		if !blocks.IsArray() {
			continue
		}
		for _, block := range blocks.Array() {
			// Deferred tool definitions cannot carry explicit cache breakpoints.
			if field == "tools" && block.Get("defer_loading").Type == gjson.True {
				continue
			}
			if claudeCacheControlEnabled(block.Get("cache_control")) {
				return true
			}
		}
	}
	messages := root.Get("messages")
	if !messages.IsArray() {
		return false
	}
	for _, message := range messages.Array() {
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		for _, block := range content.Array() {
			// Thinking can be included in a cached prefix, but cannot itself
			// declare an explicit cache breakpoint. Keep scanning other blocks.
			switch block.Get("type").String() {
			case "thinking", "redacted_thinking":
				continue
			}
			if claudeCacheControlEnabled(block.Get("cache_control")) {
				return true
			}
			// The inline-tools beta also permits a breakpoint on a tool's
			// definition inside a system tool_addition block. Do not recurse
			// through similarly named fields inside user/tool data.
			if message.Get("role").String() == "system" && block.Get("type").String() == "tool_addition" {
				tool := block.Get("tool")
				if tool.Get("type").String() == "tool_definition" &&
					tool.Get("definition.defer_loading").Type != gjson.True &&
					claudeCacheControlEnabled(tool.Get("definition.cache_control")) {
					return true
				}
			}
		}
	}
	return false
}

func claudeCacheControlEnabled(control gjson.Result) bool {
	// Routing depends only on the explicit opt-in. TTL and content eligibility
	// are provider validation concerns, not affinity conditions.
	return control.IsObject() && control.Get("type").String() == "ephemeral"
}
