<script setup lang="ts">
import { computed } from 'vue'
import { DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { MessageFormat, type SavedMessage } from '../../../bindings/schemo/internal/domain'
import type { RequestSource } from '../../lib/requestSource'
import { useLiveMessage } from '../../composables/useLiveMessage'
import { useToast } from '../../composables/useToast'
import { t } from '../../i18n'
import Segment from '../ui/Segment.vue'
import Button from '../ui/Button.vue'
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import EditorPane from './EditorPane.vue'

const { source } = defineProps<{ source: RequestSource }>()
const toast = useToast()
// The message goes out from the request's Send button once connected, or by its shortcut from here.
const { format, invalid } = useLiveMessage(source)
const saved = computed(() => source.options?.messages ?? [])
const choices = computed(() => [
  { value: MessageFormat.MessageJSON, label: 'JSON' },
  { value: MessageFormat.MessageText, label: t('request.protocol.text') },
  { value: MessageFormat.MessageBinary, label: t('request.protocol.binary') },
])
const placeholder = computed(() => {
  if (format.value === MessageFormat.MessageBinary) return 'Base64 / 0x…'
  if (format.value === MessageFormat.MessageJSON) return '{ "type": "ping" }'
  return t('request.protocol.messagePlaceholder')
})

function sameMessage(a: SavedMessage, b: SavedMessage) {
  return a.data === b.data && a.format === b.format
}

function selectMessage(message: SavedMessage) {
  source.setBody(message.data)
  source.setOptions({ messageFormat: message.format })
}

function saveMessage() {
  const message = { data: source.body.trim(), format: format.value }
  if (!message.data) {
    toast.show(t('request.protocol.enterMessage'))
    return
  }
  source.setOptions({ messages: [message, ...saved.value.filter(row => !sameMessage(row, message))].slice(0, 6) })
  toast.show(t('request.protocol.messageSaved'))
}

function removeMessage(message: SavedMessage) {
  source.setOptions({ messages: saved.value.filter(row => !sameMessage(row, message)) })
}

function oneLine(text: string) {
  return text.replace(/\s+/g, ' ')
}
</script>

<template>
  <EditorPane
    grows :language="format === MessageFormat.MessageJSON ? 'json' : null" :placeholder
    :label="t('request.protocol.messagePlaceholder')" :error="invalid ? t('request.protocol.invalidMessage') : ''"
    :model-value="source.body" @update:model-value="source.setBody"
  >
    <template #tools>
      <Segment
        variant="toolbar" :value="format" :options="choices" size="xs" :grow="false"
        @pick="source.setOptions({ messageFormat: $event as MessageFormat })"
      />
    </template>
    <template #actions>
      <DropdownMenuRoot v-if="saved.length">
        <DropdownMenuTrigger as-child>
          <Button size="bar" :title="t('request.protocol.savedMessages')">
            {{ t('request.protocol.saved') }}
            <span class="ws-muted">{{ saved.length }}</span>
            <Icon name="ChevronDown" :size="9" :stroke-width="3" class="ws-muted" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="ws-saved-menu">
          <DropdownMenuItem
            v-for="message in saved" :key="message.format + message.data"
            class="ws-saved-row" :title="message.data" @select="selectMessage(message)"
          >
            <span class="ws-saved-text">{{ oneLine(message.data) }}</span>
            <IconButton
              variant="danger" :title="t('common.delete')" :aria-label="t('common.delete')"
              @pointerdown.stop @click.stop="removeMessage(message)"
            >
              <Icon name="X" :size="12" :stroke-width="2.4" />
            </IconButton>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenuRoot>
      <Button size="bar" :title="t('request.protocol.saveMessageHint')" @click="saveMessage">
        <Icon name="BookmarkPlus" :size="14" />
        {{ t('common.save') }}
      </Button>
    </template>
  </EditorPane>
</template>

<style scoped>
@reference "../../style.css";

.ws-muted {
  color: var(--text-tertiary);
}

.ws-saved-menu {
  width: 20rem;
}

.ws-saved-row {
  gap: 0.5rem;
  padding-right: 0.25rem;
}

.ws-saved-text {
  @apply flex-1 min-w-0 truncate font-mono;
  font-size: 0.78125rem;
}
</style>
