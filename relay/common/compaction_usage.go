package common

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/tidwall/gjson"
)

// ClaudeCompactionUsage is the sum of the compaction entries in a Claude
// response's usage.iterations.
//
// Anthropic reports a compaction summarization call only as a "compaction"
// entry of usage.iterations: the top-level usage is the sum of the
// non-compaction iterations and leaves it out, for on-demand compaction
// (top-level zero) and threshold compaction (top-level = the reply) alike.
// Billing every iteration therefore equals the top-level usage plus these
// entries. Entries of other types (message, advisor_message,
// fallback_message) are already in the top-level usage or belong to another
// model's price and are not added here.
type ClaudeCompactionUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
	CacheCreation5m     int `json:"cache_creation_5m_tokens,omitempty"`
	CacheCreation1h     int `json:"cache_creation_1h_tokens,omitempty"`
}

func nonNegativeTokens(value gjson.Result) int {
	return max(int(value.Int()), 0)
}

// ParseClaudeCompactionUsage sums the compaction entries of the usage
// object at usagePath in a Claude response or message_delta event. It reports
// false when the response carries no compaction entry.
func ParseClaudeCompactionUsage(data []byte, usagePath string) (ClaudeCompactionUsage, bool) {
	var total ClaudeCompactionUsage
	found := false
	for _, entry := range gjson.GetBytes(data, usagePath+".iterations").Array() {
		if entry.Get("type").String() != "compaction" {
			continue
		}
		found = true
		total.InputTokens += nonNegativeTokens(entry.Get("input_tokens"))
		total.OutputTokens += nonNegativeTokens(entry.Get("output_tokens"))
		total.CacheReadTokens += nonNegativeTokens(entry.Get("cache_read_input_tokens"))
		total.CacheCreationTokens += nonNegativeTokens(entry.Get("cache_creation_input_tokens"))
		total.CacheCreation5m += nonNegativeTokens(entry.Get("cache_creation.ephemeral_5m_input_tokens"))
		total.CacheCreation1h += nonNegativeTokens(entry.Get("cache_creation.ephemeral_1h_input_tokens"))
	}
	return total, found
}

// ApplyClaudeCompactionUsage adds the compaction entries of a Claude response
// to the billed usage when the channel enables it. It changes only the usage
// used for settlement and the consume log; the response sent to the client
// keeps the upstream usage unchanged. Requests whose client format is not
// Claude Messages are left alone.
func ApplyClaudeCompactionUsage(info *RelayInfo, usage *dto.Usage, data []byte, usagePath string) {
	if info == nil || info.ChannelMeta == nil || usage == nil {
		return
	}
	if !info.ChannelOtherSettings.ClaudeCompactionUsageEnabled || info.RelayFormat != types.RelayFormatClaude {
		return
	}
	compaction, ok := ParseClaudeCompactionUsage(data, usagePath)
	if !ok {
		return
	}
	info.CompactionUsage = &compaction

	usage.PromptTokens += compaction.InputTokens
	usage.CompletionTokens += compaction.OutputTokens
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	usage.PromptTokensDetails.CachedTokens += compaction.CacheReadTokens
	usage.PromptTokensDetails.CachedCreationTokens += compaction.CacheCreationTokens
	usage.ClaudeCacheCreation5mTokens += compaction.CacheCreation5m
	usage.ClaudeCacheCreation1hTokens += compaction.CacheCreation1h

	// Settlement prefers the billing snapshot over the display fields, so the
	// snapshot must carry the same addition.
	if usage.BillingUsage == nil || usage.BillingUsage.ClaudeUsage == nil {
		claudeUsage := &dto.ClaudeUsage{
			InputTokens:              usage.PromptTokens,
			OutputTokens:             usage.CompletionTokens,
			CacheReadInputTokens:     usage.PromptTokensDetails.CachedTokens,
			CacheCreationInputTokens: usage.PromptTokensDetails.CachedCreationTokens,
		}
		if usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0 {
			claudeUsage.CacheCreation = &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: usage.ClaudeCacheCreation5mTokens,
				Ephemeral1hInputTokens: usage.ClaudeCacheCreation1hTokens,
			}
		}
		usage.BillingUsage = dto.NewClaudeMessagesBillingUsage(claudeUsage)
		return
	}
	claudeUsage := usage.BillingUsage.ClaudeUsage
	claudeUsage.InputTokens += compaction.InputTokens
	claudeUsage.OutputTokens += compaction.OutputTokens
	claudeUsage.CacheReadInputTokens += compaction.CacheReadTokens
	claudeUsage.CacheCreationInputTokens += compaction.CacheCreationTokens
	if compaction.CacheCreation5m > 0 || compaction.CacheCreation1h > 0 {
		if claudeUsage.CacheCreation == nil {
			claudeUsage.CacheCreation = &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: claudeUsage.ClaudeCacheCreation5mTokens,
				Ephemeral1hInputTokens: claudeUsage.ClaudeCacheCreation1hTokens,
			}
		}
		claudeUsage.CacheCreation.Ephemeral5mInputTokens += compaction.CacheCreation5m
		claudeUsage.CacheCreation.Ephemeral1hInputTokens += compaction.CacheCreation1h
	}
}
