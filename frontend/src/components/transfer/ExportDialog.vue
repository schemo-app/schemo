<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../ui/Icon.vue'
import DialogButton from '../ui/DialogButton.vue'
import TransferCheck from './TransferCheck.vue'
import TransferDialog from './TransferDialog.vue'
import TransferField from './TransferField.vue'
import TransferRow from './TransferRow.vue'
import { useCollectionsStore } from '../../stores/collections'
import { useTransferStore } from '../../stores/transfer'
import { findCollection, requestCount } from '../../lib/collectionTree'
import { t } from '../../i18n'
import type { Collection } from '../../../bindings/schemo/internal/domain'

const FORMATS = [
  { id: 'schemo', mono: 'S' },
  { id: 'postman', mono: 'PM' },
  { id: 'openapi', mono: 'OA' },
  { id: 'insomnia', mono: 'IN' },
  { id: 'curl', mono: '$_' },
] as const

type ExportFormat = (typeof FORMATS)[number]['id']

const transfer = useTransferStore()
const collections = useCollectionsStore()

const format = ref<ExportFormat>('schemo')
const colId = ref('')
const secrets = ref(false)

// A target fixes the collection and the title; otherwise the picker stands open.
const fixed = computed(() => transfer.exportTarget !== '')
const chosen = computed(() => findCollection(collections.tree, colId.value))
const requests = computed(() => (chosen.value ? requestCount(chosen.value) : 0))
// OpenAPI carries authorization schemes only, so the checkbox says nothing there.
const carried = computed(() => secrets.value && format.value !== 'openapi')

const title = computed(() =>
  fixed.value ? t('transfer.exportOf', { name: chosen.value?.name ?? '' }) : t('transfer.export')
)

const options = computed(() => {
  const out: { id: string; label: string }[] = []
  const walk = (list: Collection[]) => {
    for (const collection of list) {
      out.push({ id: collection.id, label: collection.name })
      walk(collection.children ?? [])
    }
  }
  walk(collections.tree)
  return out
})

// Every open starts over: the first format, and the first collection where the dialog picks one.
watch(
  () => transfer.exportOpen,
  (open) => {
    if (!open) return
    format.value = 'schemo'
    secrets.value = false
    colId.value = transfer.exportTarget || options.value[0]?.id || ''
  },
  { immediate: true }
)

const formatName = (id: ExportFormat) => t(`transfer.exportName.${id}`)
const formatHint = (id: ExportFormat) => t(`transfer.exportHint.${id}`)

function toggleSecrets() {
  if (format.value === 'openapi') return
  secrets.value = !secrets.value
}

async function copyAs() {
  const text = await transfer.exportText(colId.value, format.value, carried.value)
  if (text === null) return
  await transfer.copy(
    text,
    t('transfer.copiedAs', { name: chosen.value?.name ?? '', format: formatName(format.value) })
  )
  transfer.closeTransfer()
}

async function primary() {
  if (format.value === 'curl') {
    const text = await transfer.curlText(colId.value, carried.value)
    if (text === null) return
    await transfer.copy(text, t('transfer.copiedCurl', { n: t('counts.commands', requests.value) }))
    transfer.closeTransfer()
    return
  }
  if (await transfer.exportSave(colId.value, format.value, carried.value)) transfer.closeTransfer()
}
</script>

<template>
  <TransferDialog :open="transfer.exportOpen" :title @close="transfer.closeTransfer()">
    <TransferField v-if="!fixed" v-model="colId" :label="t('transfer.collection')" :options />

    <TransferField :label="fixed ? '' : t('transfer.format')">
      <div class="formats">
        <TransferRow
          v-for="row in FORMATS"
          :key="row.id"
          :mono="row.mono"
          :name="formatName(row.id)"
          :hint="formatHint(row.id)"
          :active="row.id === format"
          @click="format = row.id"
        >
          <span class="slot">
            <Icon v-if="row.id === format" name="Check" :size="12" :stroke-width="2.8" />
          </span>
        </TransferRow>
      </div>
    </TransferField>

    <div class="secrets" :class="{ inert: format === 'openapi' }" @click="toggleSecrets">
      <TransferCheck :on="carried" />
      <span>{{ t('transfer.secrets') }}</span>
    </div>

    <template #foot>
      <DialogButton @click="transfer.closeTransfer()">{{ t('transfer.cancel') }}</DialogButton>
      <DialogButton v-if="format !== 'curl'" @click="copyAs">{{ t('transfer.copy') }}</DialogButton>
      <DialogButton primary @click="primary">
        {{ format === 'curl' ? t('transfer.copy') : t('transfer.save') }}
      </DialogButton>
    </template>
  </TransferDialog>
</template>

<style scoped>
@reference "../../style.css";

.formats {
  @apply flex flex-col -mx-2.5;
}

.slot {
  @apply flex justify-center flex-none;
  width: 0.875rem;
  color: var(--accent);
}

.secrets {
  @apply flex items-center;
  gap: 0.5625rem;
  cursor: default;
}

.secrets.inert {
  opacity: 0.5;
}
</style>
