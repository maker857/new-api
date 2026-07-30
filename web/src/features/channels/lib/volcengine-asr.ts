export const VOLC_ASR_PROTOCOLS = ['v3_auc'] as const
export const VOLC_ASR_AUTH_MODES = ['new_console', 'legacy'] as const

export type VolcASRProtocol = (typeof VOLC_ASR_PROTOCOLS)[number]
export type VolcASRAuthMode = (typeof VOLC_ASR_AUTH_MODES)[number]

export type VolcASRFormFields = {
  volc_asr_enabled?: boolean
  volc_asr_protocol?: VolcASRProtocol
  volc_asr_resource_id?: string
  volc_asr_auth_mode?: VolcASRAuthMode
}

const VOLC_ASR_DEFAULTS: Required<VolcASRFormFields> = {
  volc_asr_enabled: false,
  volc_asr_protocol: 'v3_auc',
  volc_asr_resource_id: '',
  volc_asr_auth_mode: 'new_console',
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isVolcASRProtocol(value: unknown): value is VolcASRProtocol {
  return VOLC_ASR_PROTOCOLS.includes(value as VolcASRProtocol)
}

function isVolcASRAuthMode(value: unknown): value is VolcASRAuthMode {
  return VOLC_ASR_AUTH_MODES.includes(value as VolcASRAuthMode)
}

export function parseVolcASRSettings(
  settings: string | undefined
): Required<VolcASRFormFields> {
  if (!settings?.trim()) return { ...VOLC_ASR_DEFAULTS }

  try {
    const parsed: unknown = JSON.parse(settings)
    if (!isRecord(parsed) || !isRecord(parsed.volc_asr)) {
      return { ...VOLC_ASR_DEFAULTS }
    }
    const config = parsed.volc_asr
    return {
      volc_asr_enabled: true,
      volc_asr_protocol: isVolcASRProtocol(config.protocol)
        ? config.protocol
        : VOLC_ASR_DEFAULTS.volc_asr_protocol,
      volc_asr_resource_id:
        typeof config.resource_id === 'string' ? config.resource_id : '',
      volc_asr_auth_mode: isVolcASRAuthMode(config.auth_mode)
        ? config.auth_mode
        : VOLC_ASR_DEFAULTS.volc_asr_auth_mode,
    }
  } catch {
    return { ...VOLC_ASR_DEFAULTS }
  }
}

export function serializeVolcASRSettings(
  settings: string | undefined,
  channelType: number,
  fields: VolcASRFormFields
): string {
  let parsed: Record<string, unknown> = {}
  if (settings?.trim()) {
    try {
      const candidate: unknown = JSON.parse(settings)
      if (isRecord(candidate)) parsed = candidate
    } catch {
      parsed = {}
    }
  }

  if (channelType !== 45) {
    delete parsed.volc_asr
    return JSON.stringify(parsed)
  }

  if (fields.volc_asr_enabled !== true) {
    delete parsed.volc_asr
    return JSON.stringify(parsed)
  }

  parsed.volc_asr = {
    protocol: fields.volc_asr_protocol || VOLC_ASR_DEFAULTS.volc_asr_protocol,
    resource_id: fields.volc_asr_resource_id?.trim() || '',
    auth_mode:
      fields.volc_asr_auth_mode || VOLC_ASR_DEFAULTS.volc_asr_auth_mode,
  }
  return JSON.stringify(parsed)
}
