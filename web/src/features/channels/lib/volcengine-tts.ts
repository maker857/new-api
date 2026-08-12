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
export const VOLC_TTS_PROTOCOLS = [
  'v1_ws_binary',
  'v3_ws_uni',
  'v3_http_chunked',
] as const

export const VOLC_TTS_AUTH_MODES = ['new_console', 'legacy'] as const

export const VOLC_TTS_RESOURCE_IDS = [
  'seed-tts-1.0-concurr',
  'seed-tts-2.0',
  'seed-icl-2.0',
] as const

export type VolcTTSProtocol = (typeof VOLC_TTS_PROTOCOLS)[number]
export type VolcTTSAuthMode = (typeof VOLC_TTS_AUTH_MODES)[number]

export type VolcTTSFormFields = {
  volc_tts_protocol?: VolcTTSProtocol
  volc_tts_resource_id?: string
  volc_tts_auth_mode?: VolcTTSAuthMode
  volc_tts_require_usage?: boolean
}

const VOLC_TTS_DEFAULTS: Required<VolcTTSFormFields> = {
  volc_tts_protocol: 'v1_ws_binary',
  volc_tts_resource_id: '',
  volc_tts_auth_mode: 'new_console',
  volc_tts_require_usage: true,
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isVolcTTSProtocol(value: unknown): value is VolcTTSProtocol {
  return VOLC_TTS_PROTOCOLS.includes(value as VolcTTSProtocol)
}

function isVolcTTSAuthMode(value: unknown): value is VolcTTSAuthMode {
  return VOLC_TTS_AUTH_MODES.includes(value as VolcTTSAuthMode)
}

export function parseVolcTTSSettings(
  settings: string | undefined
): Required<VolcTTSFormFields> {
  if (!settings?.trim()) return { ...VOLC_TTS_DEFAULTS }

  try {
    const parsed: unknown = JSON.parse(settings)
    if (!isRecord(parsed) || !isRecord(parsed.volc_tts)) {
      return { ...VOLC_TTS_DEFAULTS }
    }
    const config = parsed.volc_tts
    return {
      volc_tts_protocol: isVolcTTSProtocol(config.protocol)
        ? config.protocol
        : VOLC_TTS_DEFAULTS.volc_tts_protocol,
      volc_tts_resource_id:
        typeof config.resource_id === 'string' ? config.resource_id : '',
      volc_tts_auth_mode: isVolcTTSAuthMode(config.auth_mode)
        ? config.auth_mode
        : VOLC_TTS_DEFAULTS.volc_tts_auth_mode,
      volc_tts_require_usage:
        typeof config.require_usage === 'boolean'
          ? config.require_usage
          : VOLC_TTS_DEFAULTS.volc_tts_require_usage,
    }
  } catch {
    return { ...VOLC_TTS_DEFAULTS }
  }
}

export function serializeVolcTTSSettings(
  settings: string | undefined,
  channelType: number,
  fields: VolcTTSFormFields
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
    delete parsed.volc_tts
    return JSON.stringify(parsed)
  }

  parsed.volc_tts = {
    protocol: fields.volc_tts_protocol || VOLC_TTS_DEFAULTS.volc_tts_protocol,
    resource_id: fields.volc_tts_resource_id?.trim() || '',
    auth_mode:
      fields.volc_tts_auth_mode || VOLC_TTS_DEFAULTS.volc_tts_auth_mode,
    require_usage:
      fields.volc_tts_require_usage ?? VOLC_TTS_DEFAULTS.volc_tts_require_usage,
  }
  return JSON.stringify(parsed)
}
