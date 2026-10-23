<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import ActionRow from './ActionRow.vue'
import Block from '../ui/Block.vue'
import SaveToVariable from './SaveToVariable.vue'
import DropdownMenuContent from '../ui/DropdownMenuContent.vue'
import DropdownMenuItem from '../ui/DropdownMenuItem.vue'
import { DropdownMenuRoot as DropdownMenu, DropdownMenuTrigger } from 'reka-ui'
import {
  BodyKind,
  CaptureSource,
  DocKind,
  type NodeView,
  type TypeFormat,
} from '../../../bindings/schemo/internal/domain'
import { Inspector } from '../../../bindings/schemo/internal/request'
import { isContainer, type Inspection } from '../../composables/useInspection'
import { useToast } from '../../composables/useToast'
import { capturedAt, looksSecret, variableNameFor } from '../../lib/capturePolicy'
import { copyToClipboard } from '../../lib/clipboard'
import type { RequestSource } from '../../lib/requestSource'
import { textOf, t } from '../../i18n'

// What can be done with the selected node. `host` is the request the response is of; without one
// (a request the browser made) nothing is written to a request.
const { inspection, node, host } = defineProps<{
  inspection: Inspection
  node: NodeView
  host: RequestSource | null
}>()

const toast = useToast()

const container = computed(() => isContainer(node))
const kind = computed(() => inspection.kind.value)

// Types are inferred from values, which a table and a JSON document have and markup has not.
const typeable = computed(
  () => container.value && (kind.value === DocKind.DocJSON || kind.value === DocKind.DocCSV)
)
const bodyable = computed(() => container.value && kind.value === DocKind.DocJSON && !!host?.scriptsLevel)

const formats = ref<TypeFormat[]>([])
onMounted(async () => (formats.value = (await Inspector.TypeFormats()) ?? []))

async function copied(text: string | null, message: string) {
  if (text !== null && (await copyToClipboard(text))) toast.show(message)
}

const copyText = async () => copied(await inspection.valueOf(node), t('common.copied'))

async function copyTypes(format: TypeFormat) {
  const message = t('inspector.typesCopied', { format: textOf(`inspector.types.${format}`) })
  await copied(await inspection.typesOf(node, format), message)
}

// A body already written is not replaced unasked: `replacing` holds whether the copy is bare while
// the answer is awaited, and null when nothing is asked.
const replacing = ref<boolean | null>(null)

async function useAsBody(bare: boolean, confirmed = false) {
  if (!host) return
  if (host.hasBody && !confirmed) {
    replacing.value = bare
    return
  }
  replacing.value = null
  const text = await inspection.bodyOf(node, bare)
  if (text === null) return
  await host.setBodyKind(BodyKind.BodyJSON)
  host.setBody(text)
  await host.flush()
  host.setOpenChip('body')
  toast.show(t('inspector.bodyReplaced'))
}

// The rule that already keeps this node, if one does: saving it again would be a second rule.
const captured = computed(() =>
  host ? capturedAt(host.captures, CaptureSource.CaptureBody, node.path) : undefined
)
const stopCapturing = () =>
  host?.saveCaptures(host.captures.filter((rule) => rule.id !== captured.value?.id))

const saving = ref(false)
const hidden = computed(
  () => looksSecret(node.name) || (node.readings ?? []).some((reading) => reading.kind === 'jwt')
)
</script>

<template>
  <Block :title="t('inspector.use')">
    <ActionRow v-if="container" @click="copyText">{{ t('inspector.copyText') }}</ActionRow>

    <DropdownMenu v-if="typeable">
      <DropdownMenuTrigger as-child>
        <ActionRow menu>{{ t('inspector.copyTypes') }}</ActionRow>
      </DropdownMenuTrigger>
      <DropdownMenuContent :side-offset="4">
        <DropdownMenuItem v-for="format in formats" :key="format" @select="copyTypes(format)">
          {{ textOf(`inspector.types.${format}`) }}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>

    <DropdownMenu v-if="bodyable && replacing === null">
      <DropdownMenuTrigger as-child>
        <ActionRow menu>{{ t('inspector.useAsBody') }}</ActionRow>
      </DropdownMenuTrigger>
      <DropdownMenuContent :side-offset="4">
        <DropdownMenuItem @select="useAsBody(false)">{{ t('inspector.bodyAsIs') }}</DropdownMenuItem>
        <DropdownMenuItem @select="useAsBody(true)">{{ t('inspector.bodyBare') }}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    <div v-else-if="replacing !== null" class="ask">
      <span>{{ t('inspector.replaceBody') }}</span>
      <button type="button" class="yes" @click="useAsBody(replacing, true)">
        {{ t('inspector.replace') }}
      </button>
      <button type="button" class="no" @click="replacing = null">{{ t('common.cancel') }}</button>
    </div>

    <ActionRow v-if="captured" @click="stopCapturing">
      <span>{{ t('inspector.captured', { name: captured.name }) }}</span>
      <span class="stop">{{ t('inspector.stopCapturing') }}</span>
    </ActionRow>
    <ActionRow v-else-if="!saving" @click="saving = true">
      {{ t('inspector.saveToVariable') }}
    </ActionRow>
    <SaveToVariable
      v-else
      :key="node.path"
      :suggested="variableNameFor(node.name)"
      :secret-by-default="hidden"
      :source="CaptureSource.CaptureBody"
      :path="node.path"
      :host
      :value="() => inspection.valueOf(node)"
      @done="saving = false"
      @cancel="saving = false"
    />
  </Block>
</template>

<style scoped>
@reference "../../style.css";

.stop {
  @apply text-12 text-text-tertiary;
}

.ask {
  @apply flex items-center gap-2 rounded-[0.3rem] px-2 text-13;
  min-height: 1.875rem;
  background: var(--bg-hover);
}

.ask span {
  @apply flex-1;
}

.yes {
  @apply cursor-pointer border-0 bg-transparent text-13 font-semibold text-accent;
}

.no {
  @apply cursor-pointer border-0 bg-transparent text-13 text-text-tertiary;
}
</style>
