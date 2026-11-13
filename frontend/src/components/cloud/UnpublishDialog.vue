<script setup lang="ts">
import { computed } from 'vue'
import Modal from '../ui/Modal.vue'
import DialogButton from '../ui/DialogButton.vue'
import { useShownWhileOpen } from '../../composables/useShownWhileOpen'
import { usePublicationsStore } from '../../stores/publications'
import { useCollectionsStore } from '../../stores/collections'
import { pageName } from '../../lib/publicationText'
import { t } from '../../i18n'

// Taking a page down: the address stops answering, so the question names what is asked about and
// says what happens to it. The name comes from the tree, since the page itself carries only a slug
// and a person is owed the name they gave the collection.
const publications = usePublicationsStore()
const collections = useCollectionsStore()

const open = computed(() => publications.asking !== '')

const page = computed(() => publications.pages.find((row) => row.collectionId === publications.asking) ?? null)

const name = computed(() => {
  if (!publications.asking) return ''
  return pageName(collections.tree, publications.asking, page.value?.slug)
})

// The row carries the address; a page asked about from the collection's own popover is the one the
// preview holds, and that is where the address is read when the list has not been loaded.
const url = computed(() => {
  if (publications.collection === publications.asking && publications.preview?.publication) {
    return publications.preview.publication.url ?? ''
  }
  return page.value?.url ?? publications.markOf(publications.asking)?.page?.url ?? ''
})

const shown = useShownWhileOpen(open, () => ({ name: name.value, url: url.value }))
</script>

<template>
  <Modal
    :open="open"
    :title="t('publications.unpublishQuestion', { name: shown.name })"
    variant="ask"
    @close="publications.cancelUnpublish()"
  >
    <span class="why">{{ t('publications.unpublishBody', { url: shown.url }) }}</span>
    <template #foot>
      <DialogButton @click="publications.cancelUnpublish()">{{ t('common.cancel') }}</DialogButton>
      <DialogButton danger @click="publications.confirmUnpublish()">
        {{ t('publications.unpublish') }}
      </DialogButton>
    </template>
  </Modal>
</template>

<style scoped>
@reference "../../style.css";

.why {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
  text-wrap: pretty;
}
</style>
