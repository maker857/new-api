import { javascript } from '@codemirror/lang-javascript'
import { foldGutter } from '@codemirror/language'
import { EditorState } from '@codemirror/state'
import { EditorView, lineNumbers } from '@codemirror/view'
import { Copy, Download, Loader2 } from 'lucide-react'
import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'

interface DiagnosticCapturePreviewDialogProps {
  open: boolean
  content: string
  sizeBytes: number
  loading: boolean
  error: string | null
  onDownload: (() => void) | null
  onOpenChange: (open: boolean) => void
}

function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return '0 B'
  }
  if (bytes < 1024) {
    return `${Math.round(bytes)} B`
  }

  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unitIndex = -1
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }

  const formattedValue = value.toFixed(2).replace(/\.?(0+)$/, '')
  return `${formattedValue} ${units[unitIndex]}`
}

export function DiagnosticCapturePreviewDialog(
  props: DiagnosticCapturePreviewDialogProps
) {
  const { t } = useTranslation()
  const editorRef = useRef<HTMLDivElement>(null)
  const { copyToClipboard } = useCopyToClipboard()
  const bodyJson = useMemo(() => {
    if (!props.content || props.loading || props.error) {
      return { request: null, response: null }
    }
    try {
      const capture = JSON.parse(props.content) as {
        request?: { body?: { json?: unknown } }
        response?: { response?: { body?: { json?: unknown } } }
      }
      const requestBody = capture.request?.body?.json
      const responseBody = capture.response?.response?.body?.json
      return {
        request:
          requestBody === undefined || requestBody === null
            ? null
            : JSON.stringify(requestBody, null, 2),
        response:
          responseBody === undefined || responseBody === null
            ? null
            : JSON.stringify(responseBody, null, 2),
      }
    } catch {
      return { request: null, response: null }
    }
  }, [props.content, props.error, props.loading])

  useEffect(() => {
    if (!props.open || !editorRef.current || props.loading || props.error) {
      return
    }
    const view = new EditorView({
      parent: editorRef.current,
      state: EditorState.create({
        doc: props.content,
        extensions: [
          lineNumbers(),
          foldGutter(),
          javascript(),
          EditorState.readOnly.of(true),
          EditorView.editable.of(false),
          EditorView.lineWrapping,
          EditorView.theme({
            '&': {
              height: '100%',
              backgroundColor: 'transparent',
              color: 'var(--foreground)',
            },
            '.cm-scroller': {
              overflow: 'auto',
              fontFamily: 'var(--font-mono)',
              lineHeight: '1.5rem',
            },
            '.cm-content': { padding: '1rem 0' },
            '.cm-gutters': {
              backgroundColor: 'transparent',
              border: 0,
              color: 'var(--muted-foreground)',
            },
            '.cm-foldGutter': { color: 'var(--muted-foreground)' },
          }),
        ],
      }),
    })
    return () => view.destroy()
  }, [props.content, props.error, props.loading, props.open])

  let previewBody: ReactNode
  if (props.loading) {
    previewBody = (
      <div className='text-muted-foreground flex h-full min-h-48 items-center justify-center gap-2 text-sm'>
        <Loader2 className='size-4 animate-spin' aria-hidden='true' />
        {t('Loading')}
      </div>
    )
  } else if (props.error) {
    previewBody = (
      <div className='text-muted-foreground flex h-full min-h-48 items-center justify-center px-6 text-center text-sm'>
        {props.error}
      </div>
    )
  } else {
    previewBody = (
      <div
        ref={editorRef}
        className={cn('h-full min-h-48 text-[13px]')}
        aria-label={t('Diagnostic Log Preview')}
      />
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Diagnostic Log Preview')}
      description={t('View the original diagnostic capture file.')}
      contentClassName='min-w-0 overflow-hidden sm:max-w-5xl'
      contentHeight='min(78dvh, 860px)'
      bodyClassName='pr-2 sm:pr-4'
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-2'>
          {!props.loading && !props.error && (
            <span className='text-muted-foreground text-xs'>
              {formatFileSize(props.sizeBytes)}
            </span>
          )}
          <div className='flex flex-wrap items-center justify-end gap-2'>
            <Button
              variant='outline'
              className='gap-1.5'
              disabled={!bodyJson.request}
              onClick={() => {
                if (bodyJson.request) {
                  void copyToClipboard(bodyJson.request)
                }
              }}
            >
              <Copy className='size-4' aria-hidden='true' />
              {t('Copy Request Body')}
            </Button>
            <Button
              variant='outline'
              className='gap-1.5'
              disabled={!bodyJson.response}
              onClick={() => {
                if (bodyJson.response) {
                  void copyToClipboard(bodyJson.response)
                }
              }}
            >
              <Copy className='size-4' aria-hidden='true' />
              {t('Copy Response Body')}
            </Button>
            {props.onDownload ? (
              <Button
                variant='outline'
                className='gap-1.5'
                onClick={props.onDownload}
              >
                <Download className='size-4' aria-hidden='true' />
                {t('Download')}
              </Button>
            ) : undefined}
          </div>
        </div>
      }
    >
      <div className='bg-muted/20 h-full min-h-0 overflow-hidden rounded-lg border'>
        {previewBody}
      </div>
    </Dialog>
  )
}
