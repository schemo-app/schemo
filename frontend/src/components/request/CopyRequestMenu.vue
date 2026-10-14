<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { Format, OmissionKind, type Omission } from '../../../bindings/schemo/internal/command'
import type { RequestSource } from '../../lib/requestSource'
import { useCopyFeedback } from '../../composables/useCopyFeedback'
import { useToast } from '../../composables/useToast'
import { useMessages, type MessageKey } from '../../i18n'

// The request copied out of the window: its address, or the whole of it as a command for one tool.
// It sits beside the address because what is copied is the request being composed, not an answer.
const { source } = defineProps<{ source: RequestSource }>()

const { t } = useMessages()

// Wire formats, not words: cURL is called cURL in every language.
const FORMATS: { id: Format; label: string }[] = [
  { id: Format.FormatCurl, label: 'cURL' },
  { id: Format.FormatFetch, label: 'fetch (JS)' },
  { id: Format.FormatWget, label: 'wget' },
  { id: Format.FormatHTTPie, label: 'HTTPie' },
  { id: Format.FormatPowerShell, label: 'PowerShell' },
]

// Every copy lights the button that opened the menu, which is where the eye already is.
const { copied, copy } = useCopyFeedback()

const toast = useToast()

// One sentence per thing the notation could not write. Partial: the generated enum carries a zero
// value that is no kind at all, and an unknown kind says nothing rather than inventing a word.
const OMITTED: Partial<Record<OmissionKind, MessageKey>> = {
  [OmissionKind.OmissionVariable]: 'request.copyOmitted.variable',
  [OmissionKind.OmissionToken]: 'request.copyOmitted.token',
  [OmissionKind.OmissionDigest]: 'request.copyOmitted.digest',
  [OmissionKind.OmissionUpload]: 'request.copyOmitted.upload',
}

// Nothing is said about what a command left out until the command is in the clipboard: a copy that
// never happened has nothing in it to be missing from.
async function copyAs(format: Format) {
  const answer = await source.copyAs(format)
  if (answer === null) return
  if (!(await copy(answer.text))) return

  const notes = (answer.omitted ?? []).flatMap((o: Omission) => {
    const key = OMITTED[o.kind]
    return key ? [t(key, { name: o.name ?? '' })] : []
  })
  if (notes.length) toast.show(t('request.copyOmitted.toast', { notes: notes.join('; ') }), 'info')
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <button
        type="button"
        class="copy-request"
        :disabled="!source.url.trim()"
        :title="t('request.copyRequest')"
        :aria-label="t('request.copyRequest')"
      >
        <!-- A tick where the sheet was is the whole answer a button with no word can give. -->
        <Icon :name="copied ? 'Check' : 'Copy'" :size="16" />
        <Icon class="caret" name="ChevronDown" :size="10" :stroke-width="2.6" />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent class="copy-menu" :side-offset="6">
      <!-- The link first, then the shapes the request can be written in: they are read for
           different reasons. -->
      <DropdownMenuItem @select="copyAs(Format.FormatURL)">
        <span class="menu-label-text cap-line">{{ t('response.copyUrl') }}</span>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem v-for="f in FORMATS" :key="f.id" @select="copyAs(f.id)">
        <span class="menu-label-text cap-line">{{ f.label }}</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<style scoped>
@reference "../../style.css";

.copy-request {
  @apply relative flex flex-none items-center justify-center cursor-pointer border-0;
  height: 2.125rem;
  padding: 0 0.4375rem 0 0.5625rem;
  gap: 0.125rem;
  border-radius: 0.375rem;
  background: transparent;
  color: var(--text-secondary);
  --wails-draggable: no-drag;
}

.copy-request:hover:not(:disabled),
.copy-request[data-state='open'] {
  background: var(--bg-hover);
  color: var(--accent);
}

.copy-request:disabled {
  @apply opacity-50 cursor-default;
}

.caret {
  @apply flex-none text-text-tertiary;
}
</style>
