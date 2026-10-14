import { computed } from 'vue'
import type { RecordView } from '../../lib/requestRecord'
import { useCopyFeedback } from '../useCopyFeedback'
import type { ResponseTab } from './useResponseView'

// What the row's copy button copies, reading by reading. Cookies, Timings and Scripts have no
// single text, so the answer there is null and the button is not drawn.
export function useResponseCopy(opts: {
  record: () => RecordView
  tab: () => ResponseTab
  prettyRaw: () => string
  exportText: (format: string) => string | undefined
}) {
  const { copied, copy } = useCopyFeedback()

  const sentText = computed(() => {
    const record = opts.record()
    return [
      `${record.method} ${record.url}`,
      ...record.requestHeaders.map((h) => `${h.name}: ${h.value}`),
      ...(record.requestBody ? ['', record.requestBody] : []),
    ].join('\n')
  })

  const copyText = computed<string | null>(() => {
    switch (opts.tab()) {
      case 'body':
        return opts.prettyRaw()
      case 'headers':
        return opts
          .record()
          .responseHeaders.map((h) => `${h.name}: ${h.value}`)
          .join('\n')
      case 'request':
        return sentText.value
      default:
        return null
    }
  })

  async function copyExport(format: string) {
    const text = opts.exportText(format)
    if (text) await copy(text)
  }

  return { copied, copy, copyText, copyExport }
}
