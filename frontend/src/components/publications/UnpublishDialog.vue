<script setup lang="ts">
import { computed } from 'vue'
import { DialogTitle } from 'reka-ui'
import { Dialog } from '../ui/dialog'
import { Button } from '../ui/button'
import { usePublicationsStore } from '../../stores/publications'
import { useCollectionsStore } from '../../stores/collections'
import { findCollection } from '../../lib/collectionTree'
import { useMessages } from '../../i18n'

// Taking a page down: the address stops answering, so the question names what is asked about and
// says what happens to it. The name comes from the tree — the page itself carries only a slug, and
// a person is owed the name they gave the collection.
const { t } = useMessages()
const store = usePublicationsStore()
const collections = useCollectionsStore()

const open = computed(() => store.asking !== '')

const page = computed(() => store.pages.find((row) => row.collectionId === store.asking) ?? null)

const name = computed(() => {
  if (!store.asking) return ''
  return findCollection(collections.tree, store.asking)?.name || page.value?.slug || ''
})

// The row carries the address; a page asked about from the collection's own popover is the one the
// preview holds, and that is where the address is read when the list has not been loaded.
const url = computed(() => {
  if (store.collection === store.asking && store.preview?.publication) {
    return store.preview.publication.url ?? ''
  }
  return page.value?.url ?? store.markOf(store.asking)?.page?.url ?? ''
})
</script>

<template>
  <Dialog
    :open="open"
    tone="cover"
    class="unpublish-dialog"
    @update:open="store.cancelUnpublish()"
  >
    <div class="ask">
      <DialogTitle class="question">
        {{ t('publications.unpublishQuestion', { name }) }}
      </DialogTitle>
      <span class="why">{{ t('publications.unpublishBody', { url }) }}</span>
    </div>
    <div class="actions">
      <Button size="lg" class="cancel-button" @click="store.cancelUnpublish()">{{ t('common.cancel') }}</Button>
      <Button size="lg" variant="danger" class="unpublish-button" @click="store.confirmUnpublish()">
        {{ t('publications.unpublish') }}
      </Button>
    </div>
  </Dialog>
</template>

<style scoped>
@reference "../../style.css";

:global(.dialog.unpublish-dialog) {
  width: 460px;
  max-width: calc(100vw - 48px);
  font-size: var(--text-13);
  line-height: normal;
}

.ask {
  @apply flex flex-col;
  padding: 1.375rem 1.25rem;
  gap: 0.5rem;
}

.question {
  font-size: var(--text-15);
  font-weight: 600;
  line-height: 1.35;
}

.why {
  @apply text-text-tertiary;
  line-height: 1.5;
  text-wrap: pretty;
}

.actions {
  @apply flex justify-end;
  gap: 0.5rem;
  padding: 0.875rem 1.25rem;
  border-top: 1px solid var(--border);
}

.actions .cancel-button {
  border: 0;
  background: var(--chip);
  font-weight: 400;
  box-shadow: none;
}

.actions .unpublish-button {
  @apply border-0 text-white;
  padding: 0 1rem;
  background: var(--red);
  font-weight: 600;
  box-shadow: none;
}

.actions .unpublish-button:hover:not(:disabled) {
  background: var(--red);
  filter: brightness(1.06);
}
</style>
