/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'

interface PricingModel {
  model_name: string
  vendor_id?: number
  quota_type: number
  model_ratio: number
  model_price: number
  completion_ratio: number
  enable_groups?: string[]
  supported_endpoint_types?: string[]
}

interface PricingVendor {
  id: number
  name: string
  icon?: string
}

interface PricingData {
  data?: PricingModel[]
  group_ratio?: Record<string, number>
  supported_endpoint?: Record<string, { path: string; method: string }>
  usable_group?: Record<string, string>
  vendors?: PricingVendor[]
}

interface RankingsData {
  data?: {
    models?: Array<{
      rank: number
      model_name: string
      vendor: string
      total_tokens: number
      share: number
    }>
  }
}

interface PerfSummaryData {
  data?: {
    models?: Array<{
      model_name: string
      avg_ttft_ms: number
      avg_latency_ms: number
      success_rate: number
      avg_tps: number
      request_count?: number
      output_tokens?: number
    }>
    series?: Array<{
      ts: number
      request_count: number
      success_rate: number
      avg_ttft_ms: number
      avg_latency_ms: number
      output_tokens?: number
    }>
  }
}

export const KALOS_PUBLIC_ORIGIN = 'https://kalosai.info'
const KALOS_PUBLIC_PRICING_PROXY = '/api/kalos-public/pricing'
const KALOS_PUBLIC_RANKINGS_PROXY = '/api/kalos-public/rankings'
const PERF_30D_SUMMARY_ENDPOINT = '/api/perf-metrics/summary?hours=720'
const PERF_7D_SUMMARY_ENDPOINT = '/api/perf-metrics/summary?hours=168'

const endpointNames: Record<string, string> = {
  anthropic: 'Anthropic Messages',
  gemini: 'Gemini GenerateContent',
  'image-generation': '图像生成',
  openai: 'OpenAI Chat',
  'openai-response': 'OpenAI Responses',
}

function normalizePricing(raw: unknown): PricingData {
  if (typeof raw === 'string') {
    return JSON.parse(raw) as PricingData
  }
  return raw as PricingData
}

function formatShare(share?: number) {
  if (!share || share <= 0) return '暂无调用'
  if (share >= 0.01) return `${(share * 100).toFixed(1)}%`
  return '<0.1%'
}

function getVendorName(model: PricingModel, vendors: PricingVendor[]) {
  return vendors.find((vendor) => vendor.id === model.vendor_id)?.name || '其他'
}

function pickModels(models: PricingModel[]) {
  const preferred = [
    'gpt-5',
    'gpt-5.2',
    'gpt-4o',
    'claude-sonnet-4-5-20250929',
    'claude-opus-4-5-20251101',
    'gemini-3.1-flash-image',
    'deepseek/deepseek-v4-pro',
    'glm-5.2',
  ]

  const picked = preferred
    .map((name) => models.find((model) => model.model_name === name))
    .filter(Boolean) as PricingModel[]

  return [...picked, ...models]
    .filter(
      (model, index, arr) =>
        arr.findIndex((item) => item.model_name === model.model_name) === index
    )
    .slice(0, 6)
}

async function fetchPricing() {
  const response = await fetch(KALOS_PUBLIC_PRICING_PROXY)
  if (!response.ok) {
    throw new Error(`Failed to load pricing: ${response.status}`)
  }
  return normalizePricing(await response.json())
}

async function fetchRankings() {
  const response = await fetch(KALOS_PUBLIC_RANKINGS_PROXY)
  if (!response.ok) {
    return null
  }
  return (await response.json()) as RankingsData
}

async function fetchPerfSummary(endpoint: string) {
  const response = await fetch(endpoint)
  if (!response.ok) {
    return null
  }
  return (await response.json()) as PerfSummaryData
}

function weightedAverage(
  rows: Array<{ value: number; weight: number }>,
  fallbackWeight = 1
) {
  const validRows = rows.filter(({ value }) => Number.isFinite(value))
  if (validRows.length === 0) return null

  const totalWeight = validRows.reduce(
    (sum, row) => sum + (row.weight > 0 ? row.weight : fallbackWeight),
    0
  )
  if (totalWeight <= 0) return null

  return (
    validRows.reduce(
      (sum, row) =>
        sum +
        valueOrZero(row.value) * (row.weight > 0 ? row.weight : fallbackWeight),
      0
    ) / totalWeight
  )
}

function valueOrZero(value: number) {
  return Number.isFinite(value) ? value : 0
}

export function usePublicSiteData() {
  const { data, isLoading } = useQuery({
    queryKey: ['home-public-site-data'],
    queryFn: async () => {
      const [pricing, rankings, perf30dSummary, perf7dSummary] =
        await Promise.all([
          fetchPricing(),
          fetchRankings(),
          fetchPerfSummary(PERF_30D_SUMMARY_ENDPOINT).catch(() => null),
          fetchPerfSummary(PERF_7D_SUMMARY_ENDPOINT).catch(() => null),
        ])
      return { pricing, rankings, perf30dSummary, perf7dSummary }
    },
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  })

  const pricing = data?.pricing
  const rankingModels = data?.rankings?.data?.models || []
  const perfModels = data?.perf30dSummary?.data?.models || []
  const perfSeries = data?.perf30dSummary?.data?.series || []
  const weeklyTokenModels = data?.perf7dSummary?.data?.models || []
  const models = pricing?.data || []
  const vendors = pricing?.vendors || []
  const groups = Object.keys(
    pricing?.usable_group || pricing?.group_ratio || {}
  )
  const endpoints = Object.entries(pricing?.supported_endpoint || {})
  const pricingByName = new Map(
    models.map((model) => [model.model_name, model])
  )
  const rankByName = new Map(
    rankingModels.map((model) => [model.model_name, model])
  )

  const rankedPricingModels = rankingModels
    .map((ranked) => pricingByName.get(ranked.model_name))
    .filter(Boolean) as PricingModel[]

  const selectedModels = (
    rankedPricingModels.length > 0 ? rankedPricingModels : pickModels(models)
  )
    .slice(0, 6)
    .map((model) => {
      const rank = rankByName.get(model.model_name)
      return {
        name: model.model_name,
        vendor: rank?.vendor || getVendorName(model, vendors),
        endpoint:
          model.supported_endpoint_types
            ?.map((type) => endpointNames[type] || type)
            .join(' / ') || 'OpenAI 兼容',
        rank: rank?.rank,
        share: rank?.share,
        callsLabel: formatShare(rank?.share),
      }
    })

  const vendorRows = vendors.map((vendor) => {
    const vendorModels = models.filter((model) => model.vendor_id === vendor.id)
    const endpointTypes = Array.from(
      new Set(
        vendorModels.flatMap((model) => model.supported_endpoint_types || [])
      )
    )

    return {
      name: vendor.name,
      modelCount: vendorModels.length,
      groups: Array.from(
        new Set(vendorModels.flatMap((model) => model.enable_groups || []))
      )
        .slice(0, 3)
        .join(' / '),
      endpoints:
        endpointTypes
          .slice(0, 3)
          .map((type) => endpointNames[type] || type)
          .join(' / ') || 'OpenAI 兼容',
    }
  })

  const perfRows = perfModels.filter((model) => (model.request_count || 0) > 0)
  const perfTotalRequests = perfRows.reduce(
    (sum, model) => sum + (model.request_count || 0),
    0
  )
  const avgLatencyMs = weightedAverage(
    perfRows.map((model) => ({
      value: model.avg_latency_ms,
      weight: model.request_count || 0,
    }))
  )
  const successRate = weightedAverage(
    perfRows.map((model) => ({
      value: model.success_rate,
      weight: model.request_count || 0,
    }))
  )

  return {
    isLoading,
    models,
    vendors,
    groups,
    endpoints,
    selectedModels,
    vendorRows,
    perfModels,
    perfSeries,
    weeklyTokenModels,
    perfTotalRequests,
    avgLatencyMs,
    successRate,
  }
}
