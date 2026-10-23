<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import { t } from '../../i18n'
import {
  relIdentifiers,
  resourceKey,
  linkHref,
  resourceLabel,
  type Relationship,
  type Resource,
} from '../../lib/jsonapi'

const { rel, resourceIndex } = defineProps<{
  rel: Relationship
  resourceIndex: Map<string, Resource>
}>()

const emit = defineEmits<{
  (e: 'jump', key: string): void
  (e: 'fetch', url: string): void
  // No payload: the parent knows the relationship name from its own v-for.
  (e: 'inspect'): void
}>()


const targets = computed(() => relIdentifiers(rel))

function labelFor(type: string, id: string): string {
  const target = resourceIndex.get(resourceKey(type, id))
  return target ? resourceLabel(target) : `${type}/${id}`
}

function onJump(key: string) {
  emit('jump', key)
  emit('inspect')
}

function onFetch(url: string) {
  emit('fetch', url)
  emit('inspect')
}
</script>

<template>
  <span v-if="targets.length === 0" class="rel-chip-empty">{{ t('json.empty') }}</span>
  <span v-else class="ja-rel-targets">
    <template v-for="(target, i) in targets" :key="i">
      <button
        v-if="resourceIndex.has(resourceKey(target.type, target.id))"
        class="rel-chip in-doc"
        :title="resourceKey(target.type, target.id)"
        @click="onJump(resourceKey(target.type, target.id))"
      >
        {{ labelFor(target.type, target.id) }}
      </button>
      <button
        v-else-if="linkHref(rel.links?.related)"
        class="rel-chip fetchable"
        :title="`fetch ${linkHref(rel.links?.related)}`"
        @click="onFetch(linkHref(rel.links?.related))"
      >
        {{ target.type }}/{{ target.id }}<Icon name="ArrowUpRight" :size="12" />
      </button>
      <span v-else class="rel-chip missing" :title="t('json.notIncluded')">
        {{ target.type }}/{{ target.id }}
      </span>
    </template>
  </span>
</template>
