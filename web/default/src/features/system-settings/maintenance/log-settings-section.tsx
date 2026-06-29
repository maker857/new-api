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
import { useCallback, useEffect, useMemo, useState } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import dayjs from '@/lib/dayjs'
import { formatTimestampToDate } from '@/lib/format'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { DateTimePicker } from '@/components/datetime-picker'
import {
  getCurrentLogCleanupTask,
  getSystemTask,
  startLogCleanupTask,
} from '../api'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { LogCleanupTask } from '../types'

const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  DiagnosticCaptureEnabled: z.boolean(),
  DiagnosticCaptureMode: z.enum(['metadata', 'full']),
  DiagnosticCaptureDir: z.string().min(1),
  DiagnosticCaptureMaxBodyMB: z.coerce.number<number>().int().min(1),
  DiagnosticCapturePaths: z.string(),
  ErrorRewriteEnabled: z.boolean(),
  ErrorRewriteSource: z.enum(['local', 'http', 'sql']),
  ErrorRewriteSyncToken: z.string(),
  ErrorRewriteRulesJSON: z.string().superRefine((value, ctx) => {
    try {
      const parsed = JSON.parse(value || '[]')
      if (!Array.isArray(parsed)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'Rules must be a JSON array',
        })
        return
      }
      for (const item of parsed) {
        if (!item || typeof item !== 'object') {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: 'Each rule must be an object',
          })
          return
        }
        const matchContent =
          typeof item.content_contains === 'string'
            ? item.content_contains
            : typeof item.keyword === 'string'
              ? item.keyword
              : ''
        if (matchContent.trim() === '') {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: 'Each rule must include match content',
          })
          return
        }
        if (
          item.status_code !== undefined &&
          (!Number.isInteger(item.status_code) ||
            item.status_code < 100 ||
            item.status_code > 599)
        ) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: 'status_code must be between 100 and 599',
          })
          return
        }
      }
    } catch {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: 'Invalid JSON data',
      })
    }
  }),
  ErrorRewriteRulesURL: z.string(),
  ErrorRewriteFallbackMessage: z.string().min(1),
  ErrorRewriteRefreshSeconds: z.coerce.number<number>().int().min(1),
  ErrorRewriteRequestTimeoutMS: z.coerce.number<number>().int().min(100),
  ErrorRewriteSQLDriver: z.enum(['mysql', 'postgres', 'sqlite']),
  ErrorRewriteSQLQuery: z.string().refine((value) => {
    const trimmed = value.trim().toLowerCase()
    return trimmed === '' || trimmed.startsWith('select')
  }, 'SQL query must be a SELECT statement'),
})

type LogSettingsFormInput = z.input<typeof logSettingsSchema>
type LogSettingsFormValues = z.output<typeof logSettingsSchema>

type LogSettingsSectionProps = {
  defaultEnabled: boolean
  diagnosticDefaults: {
    DiagnosticCaptureEnabled: boolean
    DiagnosticCaptureMode: string
    DiagnosticCaptureDir: string
    DiagnosticCaptureMaxBodyMB: number
    DiagnosticCapturePaths: string
    ErrorRewriteEnabled: boolean
    ErrorRewriteSource: string
    ErrorRewriteRulesJSON: string
    ErrorRewriteMonitorRulesJSON: string
    ErrorRewriteMonitorRulesVersion: string
    ErrorRewriteMonitorLastPullAt: string
    ErrorRewriteSyncToken: string
    ErrorRewriteRulesURL: string
    ErrorRewriteFallbackMessage: string
    ErrorRewriteRefreshSeconds: number
    ErrorRewriteRequestTimeoutMS: number
    ErrorRewriteSQLDriver: string
    ErrorRewriteSQLQuery: string
  }
}

type ServerLogInfo = {
  enabled: boolean
  log_dir: string
  file_count: number
  total_size: number
  oldest_time?: string
  newest_time?: string
}

type ErrorRewriteVisualRule = {
  content_contains: string
  message: string
  error_type?: string
  error_type_mode?: string
  error_code?: string
  error_code_mode?: string
  error_param?: string
  error_param_mode?: string
  status_code?: number
}

type ErrorRewriteMonitorRule = {
  id?: number
  status_code?: number
  channel_id?: number
  channel_name?: string
  channel_group?: string
  group_scope?: string
  model_name?: string
  content_contains?: string
  keyword?: string
  enabled?: boolean
}

type ErrorRewriteMonitorStats = {
  total: number
  enabled: number
}

function rewriteFieldModeLabel(
  mode?: string,
  value?: string,
  defaultMode = '保留'
): string {
  const normalized = mode?.trim().toLowerCase()
  if (normalized === 'replace' || mode === '替换') return '替换'
  if (
    normalized === 'filter' ||
    normalized === 'remove' ||
    normalized === 'delete' ||
    mode === '过滤'
  ) {
    return '过滤'
  }
  if (normalized === 'keep' || normalized === 'preserve' || mode === '保留') {
    return '保留'
  }
  return value?.trim() ? '替换' : defaultMode
}

const HOURS_IN_DAY = 24

function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || Number.isNaN(bytes)) return '0 Bytes'
  if (bytes === 0) return '0 Bytes'
  if (bytes < 0) return `-${formatBytes(-bytes, decimals)}`
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  if (i < 0 || i >= sizes.length) return `${bytes} Bytes`
  return `${Number.parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${
    sizes[i]
  }`
}

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

function isActiveLogCleanupTask(task: LogCleanupTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

function parseErrorRewriteRules(value: string): ErrorRewriteVisualRule[] {
  try {
    const parsed = JSON.parse(value || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((item): item is Record<string, unknown> => {
        return item !== null && typeof item === 'object'
      })
      .map((item) => {
        const statusCode =
          typeof item.status_code === 'number' &&
          Number.isInteger(item.status_code)
            ? item.status_code
            : undefined

        return {
          content_contains:
            typeof item.content_contains === 'string'
              ? item.content_contains
              : typeof item.keyword === 'string'
                ? item.keyword
                : '',
          message: typeof item.message === 'string' ? item.message : '',
          error_type:
            typeof item.error_type === 'string' ? item.error_type : undefined,
          error_type_mode:
            typeof item.error_type_mode === 'string'
              ? item.error_type_mode
              : undefined,
          error_code:
            typeof item.error_code === 'string' ? item.error_code : undefined,
          error_code_mode:
            typeof item.error_code_mode === 'string'
              ? item.error_code_mode
              : undefined,
          error_param:
            typeof item.error_param === 'string' ? item.error_param : undefined,
          error_param_mode:
            typeof item.error_param_mode === 'string'
              ? item.error_param_mode
              : undefined,
          ...(statusCode ? { status_code: statusCode } : {}),
        }
      })
  } catch {
    return []
  }
}

function stringifyErrorRewriteRules(rules: ErrorRewriteVisualRule[]): string {
  return JSON.stringify(
    rules.map((rule) => {
      const statusCode =
        typeof rule.status_code === 'number' &&
        Number.isInteger(rule.status_code)
          ? rule.status_code
          : undefined

      return {
        content_contains: rule.content_contains,
        message: rule.message,
        ...(rule.error_type ? { error_type: rule.error_type } : {}),
        ...(rule.error_type_mode ? { error_type_mode: rule.error_type_mode } : {}),
        ...(rule.error_code ? { error_code: rule.error_code } : {}),
        ...(rule.error_code_mode ? { error_code_mode: rule.error_code_mode } : {}),
        ...(rule.error_param ? { error_param: rule.error_param } : {}),
        ...(rule.error_param_mode
          ? { error_param_mode: rule.error_param_mode }
          : {}),
        ...(statusCode ? { status_code: statusCode } : {}),
      }
    }),
    null,
    2
  )
}

function monitorRuleContent(rule: ErrorRewriteMonitorRule): string {
  return (
    typeof rule.content_contains === 'string'
      ? rule.content_contains
      : typeof rule.keyword === 'string'
        ? rule.keyword
        : ''
  ).trim()
}

function findReplacementRule(
  rules: ErrorRewriteVisualRule[],
  monitorRule: ErrorRewriteMonitorRule
): ErrorRewriteVisualRule {
  const key = monitorRuleContent(monitorRule)
  return (
    rules.find((rule) => rule.content_contains.trim() === key) ?? {
      content_contains: key,
      message: '',
    }
  )
}

function updateReplacementRuleJSON(
  raw: string,
  monitorRule: ErrorRewriteMonitorRule,
  patch: Partial<ErrorRewriteVisualRule>
): string {
  const key = monitorRuleContent(monitorRule)
  const rules = parseErrorRewriteRules(raw)
  const index = rules.findIndex((rule) => rule.content_contains.trim() === key)
  const nextRule = {
    ...(index >= 0 ? rules[index] : { content_contains: key, message: '' }),
    ...patch,
    content_contains: key,
  }
  if (index >= 0) {
    rules[index] = nextRule
  } else {
    rules.push(nextRule)
  }
  return stringifyErrorRewriteRules(rules)
}

function ruleEnabledLabel(rule: ErrorRewriteMonitorRule): string {
  if (rule.enabled === true) return '已启用'
  if (rule.enabled === false) return '已停用'
  return '未提供'
}

function ruleStatusCodeLabel(rule: ErrorRewriteMonitorRule): string {
  return rule.status_code && rule.status_code > 0
    ? `HTTP ${rule.status_code}`
    : '全部状态'
}

function ruleChannelGroupLabel(rule: ErrorRewriteMonitorRule): string {
  return rule.channel_group?.trim() || '全部分组'
}

function ruleChannelLabel(rule: ErrorRewriteMonitorRule): string {
  const name = rule.channel_name?.trim()
  const id = rule.channel_id && rule.channel_id > 0 ? `#${rule.channel_id}` : ''
  if (id && name) return `${id} ${name}`
  if (id) return id
  if (name) return name
  return '全部渠道'
}

function ruleGroupScopeLabel(rule: ErrorRewriteMonitorRule): string {
  return rule.group_scope?.trim() || '全部归类'
}

function ruleModelLabel(rule: ErrorRewriteMonitorRule): string {
  return rule.model_name?.trim() || '全部模型'
}

function monitorRuleFields(rule: ErrorRewriteMonitorRule) {
  return [
    { label: '启用状态', value: ruleEnabledLabel(rule) },
    { label: '状态码', value: ruleStatusCodeLabel(rule) },
    { label: '渠道分组', value: ruleChannelGroupLabel(rule) },
    { label: '渠道', value: ruleChannelLabel(rule) },
    { label: '渠道分组归类', value: ruleGroupScopeLabel(rule) },
    { label: '模型', value: ruleModelLabel(rule) },
  ]
}

function formatUnixTime(seconds?: number | null): string {
  if (!seconds) return '暂无'
  return dayjs.unix(seconds).format('YYYY-MM-DD HH:mm:ss')
}

export function LogSettingsSection({
  defaultEnabled,
  diagnosticDefaults,
}: LogSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<LogSettingsFormInput, unknown, LogSettingsFormValues>({
    resolver: zodResolver(logSettingsSchema),
    defaultValues: {
      LogConsumeEnabled: defaultEnabled,
      DiagnosticCaptureEnabled: diagnosticDefaults.DiagnosticCaptureEnabled,
      DiagnosticCaptureMode:
        diagnosticDefaults.DiagnosticCaptureMode === 'full'
          ? 'full'
          : 'metadata',
      DiagnosticCaptureDir: diagnosticDefaults.DiagnosticCaptureDir,
      DiagnosticCaptureMaxBodyMB:
        diagnosticDefaults.DiagnosticCaptureMaxBodyMB,
      DiagnosticCapturePaths: diagnosticDefaults.DiagnosticCapturePaths,
      ErrorRewriteEnabled: diagnosticDefaults.ErrorRewriteEnabled,
      ErrorRewriteSource:
        diagnosticDefaults.ErrorRewriteSource === 'sql' ||
        diagnosticDefaults.ErrorRewriteSource === 'http'
          ? diagnosticDefaults.ErrorRewriteSource
          : 'local',
      ErrorRewriteSyncToken: diagnosticDefaults.ErrorRewriteSyncToken,
      ErrorRewriteRulesJSON: diagnosticDefaults.ErrorRewriteRulesJSON,
      ErrorRewriteRulesURL: diagnosticDefaults.ErrorRewriteRulesURL,
      ErrorRewriteFallbackMessage:
        diagnosticDefaults.ErrorRewriteFallbackMessage,
      ErrorRewriteRefreshSeconds:
        diagnosticDefaults.ErrorRewriteRefreshSeconds,
      ErrorRewriteRequestTimeoutMS:
        diagnosticDefaults.ErrorRewriteRequestTimeoutMS,
      ErrorRewriteSQLDriver:
        diagnosticDefaults.ErrorRewriteSQLDriver === 'postgres' ||
        diagnosticDefaults.ErrorRewriteSQLDriver === 'sqlite'
          ? diagnosticDefaults.ErrorRewriteSQLDriver
          : 'mysql',
      ErrorRewriteSQLQuery: diagnosticDefaults.ErrorRewriteSQLQuery,
    },
  })

  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [isStartingLogCleanup, setIsStartingLogCleanup] = useState(false)
  const [logCleanupTask, setLogCleanupTask] = useState<LogCleanupTask | null>(
    null
  )
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [serverLogInfo, setServerLogInfo] = useState<ServerLogInfo | null>(
    null
  )
  const [monitorBlacklistRules, setMonitorBlacklistRules] = useState<
    ErrorRewriteMonitorRule[]
  >([])
  const [monitorRuleStats, setMonitorRuleStats] =
    useState<ErrorRewriteMonitorStats>({ total: 0, enabled: 0 })
  const [monitorLastPullAt, setMonitorLastPullAt] = useState<number>(
    Number(diagnosticDefaults.ErrorRewriteMonitorLastPullAt) || 0
  )
  const [isLoadingMonitorRules, setIsLoadingMonitorRules] = useState(false)
  const [isPullingMonitorRules, setIsPullingMonitorRules] = useState(false)
  const [serverLogCleanupMode, setServerLogCleanupMode] = useState('by_count')
  const [serverLogCleanupValue, setServerLogCleanupValue] = useState(10)
  const [serverLogCleanupLoading, setServerLogCleanupLoading] = useState(false)

  const fetchServerLogInfo = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/logs')
      if (res.data.success) setServerLogInfo(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  const fetchMonitorBlacklistRules = useCallback(async () => {
    setIsLoadingMonitorRules(true)
    try {
      const res = await api.get('/api/error-rewrite/rules')
      if (res.data?.success) {
        const rules = res.data.data?.monitor_rules ?? []
        setMonitorBlacklistRules(rules)
        setMonitorRuleStats(
          res.data.data?.monitor_stats ?? {
            total: rules.length,
            enabled: rules.filter(
              (rule: ErrorRewriteMonitorRule) => rule.enabled !== false
            ).length,
          }
        )
        setMonitorLastPullAt(res.data.data?.monitor_last_pull_at ?? 0)
      }
    } catch {
      setMonitorBlacklistRules([])
      setMonitorRuleStats({ total: 0, enabled: 0 })
      setMonitorLastPullAt(0)
    } finally {
      setIsLoadingMonitorRules(false)
    }
  }, [])

  useEffect(() => {
    form.reset({
      LogConsumeEnabled: defaultEnabled,
      DiagnosticCaptureEnabled: diagnosticDefaults.DiagnosticCaptureEnabled,
      DiagnosticCaptureMode:
        diagnosticDefaults.DiagnosticCaptureMode === 'full'
          ? 'full'
          : 'metadata',
      DiagnosticCaptureDir: diagnosticDefaults.DiagnosticCaptureDir,
      DiagnosticCaptureMaxBodyMB:
        diagnosticDefaults.DiagnosticCaptureMaxBodyMB,
      DiagnosticCapturePaths: diagnosticDefaults.DiagnosticCapturePaths,
      ErrorRewriteEnabled: diagnosticDefaults.ErrorRewriteEnabled,
      ErrorRewriteSource:
        diagnosticDefaults.ErrorRewriteSource === 'sql' ||
        diagnosticDefaults.ErrorRewriteSource === 'http'
          ? diagnosticDefaults.ErrorRewriteSource
          : 'local',
      ErrorRewriteSyncToken: diagnosticDefaults.ErrorRewriteSyncToken,
      ErrorRewriteRulesJSON: diagnosticDefaults.ErrorRewriteRulesJSON,
      ErrorRewriteRulesURL: diagnosticDefaults.ErrorRewriteRulesURL,
      ErrorRewriteFallbackMessage:
        diagnosticDefaults.ErrorRewriteFallbackMessage,
      ErrorRewriteRefreshSeconds:
        diagnosticDefaults.ErrorRewriteRefreshSeconds,
      ErrorRewriteRequestTimeoutMS:
        diagnosticDefaults.ErrorRewriteRequestTimeoutMS,
      ErrorRewriteSQLDriver:
        diagnosticDefaults.ErrorRewriteSQLDriver === 'postgres' ||
        diagnosticDefaults.ErrorRewriteSQLDriver === 'sqlite'
          ? diagnosticDefaults.ErrorRewriteSQLDriver
          : 'mysql',
      ErrorRewriteSQLQuery: diagnosticDefaults.ErrorRewriteSQLQuery,
    })
  }, [defaultEnabled, diagnosticDefaults, form])

  useEffect(() => {
    fetchServerLogInfo()
    fetchMonitorBlacklistRules()
  }, [fetchMonitorBlacklistRules, fetchServerLogInfo])

  useEffect(() => {
    let cancelled = false

    async function fetchCurrentLogCleanupTask() {
      try {
        const res = await getCurrentLogCleanupTask()
        if (!cancelled && res.success && res.data) {
          setLogCleanupTask(res.data)
        }
      } catch {
        /* ignore */
      }
    }

    fetchCurrentLogCleanupTask()

    return () => {
      cancelled = true
    }
  }, [])

  const purgeTimestamp = useMemo(() => {
    if (!purgeDate) return null
    return Math.floor(purgeDate.getTime() / 1000)
  }, [purgeDate])

  const formattedPurgeDate = useMemo(() => {
    if (!purgeDate) return ''
    return formatTimestampToDate(purgeDate.getTime(), 'milliseconds')
  }, [purgeDate])

  const logCleanupActive = isActiveLogCleanupTask(logCleanupTask)
  const logCleanupState = logCleanupTask?.state
  const logCleanupProgress = Math.min(
    100,
    Math.max(0, logCleanupState?.progress ?? 0)
  )
  const logCleanupProcessed = logCleanupState?.processed ?? 0
  const logCleanupTotal = logCleanupState?.total ?? 0
  const logCleanupTaskId = logCleanupTask?.task_id

  useEffect(() => {
    if (!logCleanupTaskId || !logCleanupActive) return

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getSystemTask(logCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            toast.success(
              count > 0
                ? t('{{count}} log entries removed.', { count })
                : t('No log entries matched the selected time.')
            )
          } else if (res.data.status === 'failed') {
            toast.error(res.data.error || t('Failed to clean logs'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logCleanupActive, logCleanupTaskId, t])

  const onSubmit = async (values: LogSettingsFormValues) => {
    const updates = [
      ['LogConsumeEnabled', values.LogConsumeEnabled, defaultEnabled],
      [
        'DiagnosticCaptureEnabled',
        values.DiagnosticCaptureEnabled,
        diagnosticDefaults.DiagnosticCaptureEnabled,
      ],
      [
        'DiagnosticCaptureMode',
        values.DiagnosticCaptureMode,
        diagnosticDefaults.DiagnosticCaptureMode,
      ],
      [
        'DiagnosticCaptureDir',
        values.DiagnosticCaptureDir,
        diagnosticDefaults.DiagnosticCaptureDir,
      ],
      [
        'DiagnosticCaptureMaxBodyMB',
        values.DiagnosticCaptureMaxBodyMB,
        diagnosticDefaults.DiagnosticCaptureMaxBodyMB,
      ],
      [
        'DiagnosticCapturePaths',
        values.DiagnosticCapturePaths,
        diagnosticDefaults.DiagnosticCapturePaths,
      ],
      [
        'ErrorRewriteEnabled',
        values.ErrorRewriteEnabled,
        diagnosticDefaults.ErrorRewriteEnabled,
      ],
      [
        'ErrorRewriteSource',
        values.ErrorRewriteSource,
        diagnosticDefaults.ErrorRewriteSource,
      ],
      [
        'ErrorRewriteSyncToken',
        values.ErrorRewriteSyncToken,
        diagnosticDefaults.ErrorRewriteSyncToken,
      ],
      [
        'ErrorRewriteRulesJSON',
        values.ErrorRewriteRulesJSON,
        diagnosticDefaults.ErrorRewriteRulesJSON,
      ],
      [
        'ErrorRewriteRulesURL',
        values.ErrorRewriteRulesURL,
        diagnosticDefaults.ErrorRewriteRulesURL,
      ],
      [
        'ErrorRewriteFallbackMessage',
        values.ErrorRewriteFallbackMessage,
        diagnosticDefaults.ErrorRewriteFallbackMessage,
      ],
      [
        'ErrorRewriteRefreshSeconds',
        values.ErrorRewriteRefreshSeconds,
        diagnosticDefaults.ErrorRewriteRefreshSeconds,
      ],
      [
        'ErrorRewriteRequestTimeoutMS',
        values.ErrorRewriteRequestTimeoutMS,
        diagnosticDefaults.ErrorRewriteRequestTimeoutMS,
      ],
      [
        'ErrorRewriteSQLDriver',
        values.ErrorRewriteSQLDriver,
        diagnosticDefaults.ErrorRewriteSQLDriver,
      ],
      [
        'ErrorRewriteSQLQuery',
        values.ErrorRewriteSQLQuery,
        diagnosticDefaults.ErrorRewriteSQLQuery,
      ],
    ] as const

    for (const [key, value, original] of updates) {
      if (value === original) continue
      await updateOption.mutateAsync({ key, value })
    }
    form.reset(values)
  }

  const pullMonitorRulesNow = async () => {
    setIsPullingMonitorRules(true)
    try {
      const values = form.getValues()
      await onSubmit(values)
      const res = await api.post('/api/error-rewrite/rules/refresh')
      if (!res.data?.success) {
        throw new Error(res.data?.message || '拉取失败')
      }
      const count = res.data?.data?.monitor_rules?.length ?? 0
      setMonitorBlacklistRules(res.data?.data?.monitor_rules ?? [])
      setMonitorRuleStats(
        res.data?.data?.monitor_stats ?? { total: count, enabled: count }
      )
      setMonitorLastPullAt(res.data?.data?.monitor_last_pull_at ?? 0)
      toast.success(`拉取成功，已同步 ${count} 条监控黑名单规则`)
    } catch (error) {
      const message = error instanceof Error ? error.message : '拉取失败'
      toast.error(`拉取失败：${message}`)
    } finally {
      setIsPullingMonitorRules(false)
    }
  }

  const handleRequestCleanLogs = () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setShowConfirmDialog(true)
  }

  const handleCleanLogs = async () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setIsStartingLogCleanup(true)
    try {
      const res = await startLogCleanupTask(purgeTimestamp)
      if (!res.success) {
        throw new Error(res.message || t('Failed to clean logs'))
      }
      if (!res.data) {
        throw new Error(t('Failed to clean logs'))
      }
      setLogCleanupTask(res.data)
      setShowConfirmDialog(false)
      toast.success(t('Log cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to clean logs')
      toast.error(message)
    } finally {
      setIsStartingLogCleanup(false)
    }
  }

  const cleanupServerLogFiles = async () => {
    if (
      !serverLogCleanupValue ||
      Number.isNaN(serverLogCleanupValue) ||
      serverLogCleanupValue < 1
    ) {
      toast.error(t('Please enter a valid number'))
      return
    }

    setServerLogCleanupLoading(true)
    try {
      const res = await api.delete(
        `/api/performance/logs?mode=${serverLogCleanupMode}&value=${serverLogCleanupValue}`
      )
      if (res.data.success) {
        const { deleted_count, freed_bytes } = res.data.data
        toast.success(
          t('Cleaned up {{count}} log files, freed {{size}}', {
            count: deleted_count,
            size: formatBytes(freed_bytes),
          })
        )
      } else {
        toast.error(res.data.message || t('Cleanup failed'))
      }
      fetchServerLogInfo()
    } catch {
      toast.error(t('Cleanup failed'))
    } finally {
      setServerLogCleanupLoading(false)
    }
  }

  return (
    <SettingsSection title={t('Log Maintenance')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save log settings'
          />
          <FormField
            control={form.control}
            name='LogConsumeEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Record quota usage')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Track per-request consumption to power usage analytics. Keeping this on increases database writes.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />

          <SettingsControlGroup className='space-y-4'>
            <FormField
              control={form.control}
              name='DiagnosticCaptureEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Diagnostic capture')}</FormLabel>
                    <FormDescription>
                      {t('Save relay request and response captures to disk.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />

            <div className='grid gap-4 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='DiagnosticCaptureMode'
                render={({ field }) => (
                  <div className='space-y-2'>
                    <FormLabel>{t('Capture mode')}</FormLabel>
                    <Select
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value='metadata'>
                            {t('Metadata')}
                          </SelectItem>
                          <SelectItem value='full'>{t('Full')}</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </div>
                )}
              />

              <FormField
                control={form.control}
                name='DiagnosticCaptureDir'
                render={({ field }) => (
                  <div className='space-y-2'>
                    <FormLabel>{t('Capture directory')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </div>
                )}
              />

              <FormField
                control={form.control}
                name='DiagnosticCaptureMaxBodyMB'
                render={({ field }) => (
                  <div className='space-y-2'>
                    <FormLabel>{t('Max body size (MB)')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={1} {...field} />
                    </FormControl>
                    <FormMessage />
                  </div>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='DiagnosticCapturePaths'
              render={({ field }) => (
                <div className='space-y-2'>
                  <FormLabel>{t('Capture paths')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('Comma-separated paths. Use * for prefix matching.')}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />
          </SettingsControlGroup>

          <SettingsControlGroup className='space-y-4'>
            <FormField
              control={form.control}
              name='ErrorRewriteEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Error rewrite')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Rewrite upstream error messages when monitoring rules match blacklisted text.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />

            <FormField
              control={form.control}
              name='ErrorRewriteSource'
              render={({ field }) => (
                <div className='space-y-2'>
                  <FormLabel>{t('Rules source')}</FormLabel>
                  <FormControl>
                    <div className='grid gap-2 md:grid-cols-2'>
                      <Button
                        type='button'
                        variant={field.value === 'local' ? 'default' : 'outline'}
                        onClick={() => field.onChange('local')}
                      >
                        模式 1：等待监控服务器推送
                      </Button>
                      <Button
                        type='button'
                        variant={field.value === 'http' ? 'default' : 'outline'}
                        onClick={() => field.onChange('http')}
                      >
                        模式 2：定时拉取监控规则
                      </Button>
                    </div>
                  </FormControl>
                  <FormDescription>
                    模式 1 需要监控服务器能访问 New API；模式 2 适合本地电脑没有公网 IP 时测试，New API 会主动访问监控服务器。
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name='ErrorRewriteRulesJSON'
              render={({ field }) => (
                <div className='space-y-3'>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <FormLabel>监控黑名单筛选规则</FormLabel>
                    <div className='text-muted-foreground flex flex-wrap gap-2 text-xs'>
                      <span className='bg-muted rounded px-2 py-1'>
                        已读取 {monitorRuleStats.total} 条
                      </span>
                      <span className='bg-muted rounded px-2 py-1'>
                        已启用 {monitorRuleStats.enabled} 条
                      </span>
                      <span className='bg-muted rounded px-2 py-1'>
                        上次拉取：{formatUnixTime(monitorLastPullAt)}
                      </span>
                      <span className='bg-muted rounded px-2 py-1'>
                        拉取间隔：{form.watch('ErrorRewriteRefreshSeconds')} 秒
                      </span>
                    </div>
                  </div>
                  <FormControl>
                    <div className='space-y-3'>
                      {isLoadingMonitorRules ? (
                        <div className='text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
                          正在读取监控黑名单规则...
                        </div>
                      ) : monitorBlacklistRules.length === 0 ? (
                        <div className='text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
                          暂未同步到监控黑名单规则
                        </div>
                      ) : (
                        monitorBlacklistRules.map((monitorRule, index) => {
                          const localRules = parseErrorRewriteRules(field.value)
                          const replacement = findReplacementRule(
                            localRules,
                            monitorRule
                          )

                          return (
                            <div
                              key={`${monitorRule.id ?? index}-${monitorRuleContent(
                                monitorRule
                              )}`}
                              className='bg-card rounded-lg border-2 border-border p-4 shadow-sm'
                            >
                              <div className='space-y-2'>
                                <div className='flex flex-wrap items-center gap-2'>
                                  <Label>黑名单筛选规则</Label>
                                  {monitorRule.id ? (
                                    <span className='bg-muted text-muted-foreground rounded px-2 py-0.5 text-xs'>
                                      #{monitorRule.id}
                                    </span>
                                  ) : null}
                                </div>
                                <div className='grid gap-2 md:grid-cols-3 xl:grid-cols-6'>
                                  {monitorRuleFields(monitorRule).map((item) => (
                                    <div
                                      key={item.label}
                                      className='bg-muted/40 rounded-md border px-3 py-2'
                                    >
                                      <div className='text-muted-foreground text-xs'>
                                        {item.label}
                                      </div>
                                      <div className='mt-1 break-words text-sm'>
                                        {item.value}
                                      </div>
                                    </div>
                                  ))}
                                </div>
                                <div className='space-y-1'>
                                  <div className='text-muted-foreground text-xs'>
                                    黑名单过滤内容
                                  </div>
                                  <div className='border-l-destructive bg-destructive/5 min-h-11 whitespace-pre-wrap rounded-md border border-l-4 px-3 py-2 text-sm font-medium'>
                                    {monitorRuleContent(monitorRule) || '空内容'}
                                  </div>
                                </div>
                              </div>

                              <div className='mt-3 grid gap-3 md:grid-cols-[minmax(0,1fr)_180px] md:items-start'>
                                <div className='space-y-2'>
                                  <Label>{t('Error message content')}</Label>
                                  <Textarea
                                    rows={3}
                                    value={replacement.message}
                                    placeholder={t(
                                      'Upstream service is temporarily unavailable. Please try again later.'
                                    )}
                                    onChange={(event) =>
                                      field.onChange(
                                        updateReplacementRuleJSON(
                                          field.value,
                                          monitorRule,
                                          {
                                            message: event.target.value,
                                          }
                                        )
                                      )
                                    }
                                  />
                                </div>
                                <div className='space-y-2'>
                                  <Label>{t('Returned status code')}</Label>
                                  <Input
                                    type='number'
                                    min={100}
                                    max={599}
                                    value={replacement.status_code ?? ''}
                                    placeholder='保留上游状态码'
                                    onChange={(event) => {
                                      const value = event.target.value.trim()
                                      field.onChange(
                                        updateReplacementRuleJSON(
                                          field.value,
                                          monitorRule,
                                          {
                                            status_code:
                                              value === ''
                                                ? undefined
                                                : Number(value),
                                          }
                                        )
                                      )
                                    }}
                                  />
                                </div>
                              </div>
                              <div className='mt-3 grid gap-3 md:grid-cols-3'>
                                <div className='space-y-2'>
                                  <Label>错误 type</Label>
                                  <div className='grid grid-cols-[120px_minmax(0,1fr)] gap-2'>
                                    <Select
                                      value={
                                        rewriteFieldModeLabel(
                                          replacement.error_type_mode,
                                          replacement.error_type
                                        )
                                      }
                                      onValueChange={(value) =>
                                        field.onChange(
                                          updateReplacementRuleJSON(
                                            field.value,
                                            monitorRule,
                                            { error_type_mode: value }
                                          )
                                        )
                                      }
                                    >
                                      <SelectTrigger>
                                        <SelectValue />
                                      </SelectTrigger>
                                      <SelectContent>
                                        <SelectItem value='替换'>
                                          替换
                                        </SelectItem>
                                        <SelectItem value='过滤'>
                                          过滤
                                        </SelectItem>
                                        <SelectItem value='保留'>
                                          保留
                                        </SelectItem>
                                      </SelectContent>
                                    </Select>
                                    <Input
                                      value={replacement.error_type ?? ''}
                                      disabled={
                                        rewriteFieldModeLabel(
                                          replacement.error_type_mode,
                                          replacement.error_type
                                        ) !== '替换'
                                      }
                                      placeholder='仅上游有 type 时替换'
                                      onChange={(event) =>
                                        field.onChange(
                                          updateReplacementRuleJSON(
                                            field.value,
                                            monitorRule,
                                            {
                                              error_type:
                                                event.target.value.trim() ||
                                                undefined,
                                            }
                                          )
                                        )
                                      }
                                    />
                                  </div>
                                </div>
                                <div className='space-y-2'>
                                  <Label>错误 code</Label>
                                  <div className='grid grid-cols-[120px_minmax(0,1fr)] gap-2'>
                                    <Select
                                      value={
                                        rewriteFieldModeLabel(
                                          replacement.error_code_mode,
                                          replacement.error_code,
                                          '过滤'
                                        )
                                      }
                                      onValueChange={(value) =>
                                        field.onChange(
                                          updateReplacementRuleJSON(
                                            field.value,
                                            monitorRule,
                                            { error_code_mode: value }
                                          )
                                        )
                                      }
                                    >
                                      <SelectTrigger>
                                        <SelectValue />
                                      </SelectTrigger>
                                      <SelectContent>
                                        <SelectItem value='替换'>
                                          替换
                                        </SelectItem>
                                        <SelectItem value='过滤'>
                                          过滤
                                        </SelectItem>
                                        <SelectItem value='保留'>
                                          保留
                                        </SelectItem>
                                      </SelectContent>
                                    </Select>
                                    <Input
                                      value={replacement.error_code ?? ''}
                                      disabled={
                                        rewriteFieldModeLabel(
                                          replacement.error_code_mode,
                                          replacement.error_code,
                                          '过滤'
                                        ) !== '替换'
                                      }
                                      placeholder='仅上游有 code 时替换'
                                      onChange={(event) =>
                                        field.onChange(
                                          updateReplacementRuleJSON(
                                            field.value,
                                            monitorRule,
                                            {
                                              error_code:
                                                event.target.value.trim() ||
                                                undefined,
                                            }
                                          )
                                        )
                                      }
                                    />
                                  </div>
                                </div>
                                <div className='space-y-2'>
                                  <Label>错误 param</Label>
                                  <div className='grid grid-cols-[120px_minmax(0,1fr)] gap-2'>
                                    <Select
                                      value={
                                        rewriteFieldModeLabel(
                                          replacement.error_param_mode,
                                          replacement.error_param,
                                          '过滤'
                                        )
                                      }
                                      onValueChange={(value) =>
                                        field.onChange(
                                          updateReplacementRuleJSON(
                                            field.value,
                                            monitorRule,
                                            { error_param_mode: value }
                                          )
                                        )
                                      }
                                    >
                                      <SelectTrigger>
                                        <SelectValue />
                                      </SelectTrigger>
                                      <SelectContent>
                                        <SelectItem value='替换'>
                                          替换
                                        </SelectItem>
                                        <SelectItem value='过滤'>
                                          过滤
                                        </SelectItem>
                                        <SelectItem value='保留'>
                                          保留
                                        </SelectItem>
                                      </SelectContent>
                                    </Select>
                                    <Input
                                      value={replacement.error_param ?? ''}
                                      disabled={
                                        rewriteFieldModeLabel(
                                          replacement.error_param_mode,
                                          replacement.error_param,
                                          '过滤'
                                        ) !== '替换'
                                      }
                                      placeholder='仅上游有 param 时替换'
                                      onChange={(event) =>
                                        field.onChange(
                                          updateReplacementRuleJSON(
                                            field.value,
                                            monitorRule,
                                            {
                                              error_param:
                                                event.target.value.trim() ||
                                                undefined,
                                            }
                                          )
                                        )
                                      }
                                    />
                                  </div>
                                </div>
                              </div>
                            </div>
                          )
                        })
                      )}
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t(
                      'These filter rules are synchronized from the monitoring system. New API only configures the message and optional status code returned after a rule is matched.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />

            <div className='grid gap-4 md:grid-cols-2'>
              <div className='space-y-2'>
                  <FormLabel>{t('Monitor snapshot version')}</FormLabel>
                <Input
                  value={diagnosticDefaults.ErrorRewriteMonitorRulesVersion}
                  disabled
                />
              </div>
              <div className='space-y-2'>
                  <FormLabel>{t('Monitor snapshot size')}</FormLabel>
                <Input
                  value={diagnosticDefaults.ErrorRewriteMonitorRulesJSON.length}
                  disabled
                />
              </div>
            </div>

            <FormField
              control={form.control}
              name='ErrorRewriteRulesURL'
              render={({ field }) => (
                <div className='space-y-2'>
                  <FormLabel>监控规则拉取地址</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='http://38.181.57.188:8086/api/log-management/blacklist-rules/export'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    模式 2 使用这个地址。New API 会按设置的间隔拉取黑名单规则，并保存到本地数据库。
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name='ErrorRewriteSyncToken'
              render={({ field }) => (
                <div className='space-y-2'>
                  <FormLabel>监控同步密钥</FormLabel>
                  <FormControl>
                    <Input type='password' autoComplete='new-password' {...field} />
                  </FormControl>
                  <FormDescription>
                    模式 1 和模式 2 共用这个密钥，请和监控服务器的 APM_LOG_BLACKLIST_SYNC_TOKEN 保持一致。
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />

            <div className='grid gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='ErrorRewriteSQLDriver'
                render={({ field }) => (
                  <div className='space-y-2'>
                    <FormLabel>{t('Monitoring database driver')}</FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value='mysql'>MySQL</SelectItem>
                          <SelectItem value='postgres'>PostgreSQL</SelectItem>
                          <SelectItem value='sqlite'>SQLite</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </div>
                )}
              />

              <div className='space-y-2'>
                <FormLabel>{t('Monitoring database DSN')}</FormLabel>
                <Input value='ERROR_REWRITE_SQL_DSN' disabled />
                <FormDescription>
                  {t(
                    'Set the real database connection string with this server environment variable.'
                  )}
                </FormDescription>
              </div>
            </div>

            <FormField
              control={form.control}
              name='ErrorRewriteSQLQuery'
              render={({ field }) => (
                <div className='space-y-2'>
                  <FormLabel>{t('Monitoring rules SQL')}</FormLabel>
                  <FormControl>
                    <Textarea rows={4} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Return keyword and rule_type columns. Only rows with rule_type blacklist rewrite errors.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name='ErrorRewriteFallbackMessage'
              render={({ field }) => (
                <div className='space-y-2'>
                  <FormLabel>{t('Fallback error message')}</FormLabel>
                  <FormControl>
                    <Textarea rows={3} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Used when a blacklist rule matches and the monitoring response does not provide a replacement message.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </div>
              )}
            />

            <div className='grid gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='ErrorRewriteRefreshSeconds'
                render={({ field }) => (
                  <div className='space-y-2'>
                    <FormLabel>
                      拉取间隔（秒）
                    </FormLabel>
                    <FormControl>
                      <Input type='number' min={1} {...field} />
                    </FormControl>
                    <FormMessage />
                  </div>
                )}
              />

              <FormField
                control={form.control}
                name='ErrorRewriteRequestTimeoutMS'
                render={({ field }) => (
                  <div className='space-y-2'>
                    <FormLabel>{t('Rules request timeout (ms)')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={100} {...field} />
                    </FormControl>
                    <FormMessage />
                  </div>
                )}
              />
            </div>

            <div className='flex justify-end'>
              <Button
                type='button'
                variant='outline'
                onClick={pullMonitorRulesNow}
                disabled={isPullingMonitorRules || updateOption.isPending}
              >
                {isPullingMonitorRules
                  ? '正在拉取...'
                  : '手动拉取信息'}
              </Button>
            </div>
          </SettingsControlGroup>

          <SettingsControlGroup className='space-y-3'>
            <div>
              <h4 className='text-sm font-medium'>{t('Clean history logs')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Remove all log entries created before the selected timestamp.'
                )}
              </p>
            </div>
            <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
            <div className='flex flex-wrap gap-3'>
              {quickSelectOptions.map((option) => (
                <Button
                  key={option.label}
                  type='button'
                  variant='outline'
                  onClick={() => setPurgeDate(option.getValue())}
                >
                  {t(option.label)}
                </Button>
              ))}
              <Button
                type='button'
                variant='destructive'
                onClick={handleRequestCleanLogs}
                disabled={isStartingLogCleanup || logCleanupActive}
              >
                {isStartingLogCleanup || logCleanupActive
                  ? t('Cleaning...')
                  : t('Clean logs')}
              </Button>
            </div>
            {logCleanupTask && (
              <div className='rounded-md border p-3'>
                <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
                  <span className='font-medium'>
                    {t('Log cleanup progress')}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {logCleanupProgress}%
                  </span>
                </div>
                <Progress value={logCleanupProgress} />
                <div className='text-muted-foreground mt-2 text-xs'>
                  {t('{{processed}} of {{total}} log entries processed.', {
                    processed: logCleanupProcessed,
                    total: logCleanupTotal,
                  })}
                </div>
                {logCleanupTask.status === 'failed' && logCleanupTask.error && (
                  <div className='text-destructive mt-2 text-xs'>
                    {logCleanupTask.error}
                  </div>
                )}
              </div>
            )}
          </SettingsControlGroup>
        </SettingsForm>
      </Form>

      <Separator />

      <div className='space-y-4'>
        <div>
          <h4 className='font-medium'>{t('Server Log Management')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Manage server log files. Log files accumulate over time; regular cleanup is recommended to free disk space.'
            )}
          </p>
        </div>

        {serverLogInfo !== null &&
          (serverLogInfo.enabled ? (
            <div className='space-y-4'>
              <div className='rounded-lg border p-4'>
                <div className='grid grid-cols-2 gap-2 text-sm md:grid-cols-4'>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log Directory')}:
                    </span>{' '}
                    <span className='font-mono text-xs'>
                      {serverLogInfo.log_dir}
                    </span>
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log File Count')}:
                    </span>{' '}
                    {serverLogInfo.file_count}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Total Log Size')}:
                    </span>{' '}
                    {formatBytes(serverLogInfo.total_size)}
                  </div>
                  {serverLogInfo.oldest_time && serverLogInfo.newest_time && (
                    <div>
                      <span className='text-muted-foreground'>
                        {t('Date Range')}:
                      </span>{' '}
                      {dayjs(serverLogInfo.oldest_time).format('YYYY-MM-DD')} ~{' '}
                      {dayjs(serverLogInfo.newest_time).format('YYYY-MM-DD')}
                    </div>
                  )}
                </div>
              </div>

              <div className='flex flex-wrap items-end gap-3'>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>{t('Cleanup Mode')}</Label>
                  <Select
                    items={[
                      { value: 'by_count', label: t('Retain last N files') },
                      { value: 'by_days', label: t('Retain last N days') },
                    ]}
                    value={serverLogCleanupMode}
                    onValueChange={(value) =>
                      value !== null && setServerLogCleanupMode(value)
                    }
                  >
                    <SelectTrigger className='w-[160px]'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='by_count'>
                          {t('Retain last N files')}
                        </SelectItem>
                        <SelectItem value='by_days'>
                          {t('Retain last N days')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>
                    {serverLogCleanupMode === 'by_count'
                      ? t('Files to Retain')
                      : t('Days to Retain')}
                  </Label>
                  <Input
                    type='number'
                    min={1}
                    max={serverLogCleanupMode === 'by_count' ? 1000 : 3650}
                    value={serverLogCleanupValue}
                    onChange={(event) =>
                      setServerLogCleanupValue(Number(event.target.value))
                    }
                    className='w-[120px]'
                  />
                </div>
                <AlertDialog>
                  <AlertDialogTrigger
                    render={
                      <Button
                        type='button'
                        variant='destructive'
                        size='sm'
                        disabled={serverLogCleanupLoading}
                      />
                    }
                  >
                    {serverLogCleanupLoading
                      ? t('Cleaning...')
                      : t('Clean Up Log Files')}
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {t('Confirm log file cleanup?')}
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        {serverLogCleanupMode === 'by_count'
                          ? t(
                              'Only the last {{value}} log files will be retained; the rest will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )
                          : t(
                              'Log files older than {{value}} days will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )}
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                      <AlertDialogAction
                        variant='destructive'
                        onClick={cleanupServerLogFiles}
                      >
                        {t('Confirm Cleanup')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            </div>
          ) : (
            <Alert>
              <AlertDescription>
                {t(
                  'Server logging is not enabled (log directory not configured)'
                )}
              </AlertDescription>
            </Alert>
          ))}
      </div>

      <AlertDialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm log cleanup')}</AlertDialogTitle>
            <AlertDialogDescription>
              {formattedPurgeDate
                ? t(
                    'This will permanently remove all log entries created before {{date}}.',
                    { date: formattedPurgeDate }
                  )
                : t(
                    'This will permanently remove log entries before the selected timestamp.'
                  )}{' '}
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isStartingLogCleanup}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleCleanLogs}
              disabled={isStartingLogCleanup}
            >
              {isStartingLogCleanup ? t('Cleaning...') : t('Delete logs')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
