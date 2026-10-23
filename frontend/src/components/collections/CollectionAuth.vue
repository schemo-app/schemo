<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AuthFields from '../request/AuthFields.vue'
import PaneSelect from '../request/PaneSelect.vue'
import { useCollectionsStore } from '../../stores/collections'
import { findCollection, trailOf } from '../../lib/collectionTree'
import { useAuthSchemes } from '../../composables/useAuthSchemes'
import { textOf, t } from '../../i18n'
import { AuthType, type Auth } from '../../../bindings/schemo/internal/domain'

// What every request of the collection goes out authorized with. The page writes itself: a change
// is saved as it is made, because there is no button here to press.
const { levelId } = defineProps<{ levelId: string }>()

const collections = useCollectionsStore()
const { menuFor, nameOf, schemeOf } = useAuthSchemes()

const NONE: Auth = { type: AuthType.AuthNone, fields: {} }

const level = computed(() => findCollection(collections.tree, collections.collectionId))
const stored = computed<Auth>(() => level.value?.auth ?? NONE)
// A collection inside another inherits; the top one has nothing above it to inherit from.
const canInherit = computed(() => (trailOf(collections.tree, levelId)?.ancestors.length ?? 0) > 0)

const draft = ref<Auth>(stored.value)

watch([() => levelId, () => stored.value.type, () => stored.value.fields], () => {
  draft.value = { type: stored.value.type, fields: { ...(stored.value.fields ?? {}) } }
})

const scheme = computed(() => schemeOf(draft.value.type))

const options = computed(() => menuFor(canInherit.value))

const label = computed(() => nameOf(draft.value.type))

// The row's line is a divider, so it is drawn only when there is something under it to part from:
// a scheme with no fields and no note leaves the row standing alone.
const followed = computed(() => Boolean(scheme.value?.fields?.length || scheme.value?.note))

async function save() {
  await collections.saveAuth(levelId, draft.value)
}

function choose(type: string) {
  draft.value = { ...draft.value, type: type as Auth['type'] }
  void save()
}

function onField(key: string, value: string) {
  draft.value = { ...draft.value, fields: { ...draft.value.fields, [key]: value } }
}
</script>

<template>
  <div class="col-auth">
    <div class="auth-row" :class="{ followed }">
      <span class="auth-label">{{ t('collections.authType') }}</span>
      <span class="auth-value">
        <PaneSelect
          variant="cell"
          :label
          :options
          :current="draft.type"
          @select="choose"
        />
      </span>
    </div>

    <AuthFields
      v-if="scheme?.fields?.length"
      :scheme
      :auth="draft"
      scale="rows"
      @update="onField"
      @commit="save"
    />

    <p v-if="scheme?.note" class="auth-note">{{ textOf(scheme.note) }}</p>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.col-auth {
  @apply flex flex-col flex-1 min-h-0 overflow-y-auto;
  padding: 0.75rem 1.5rem;
  max-width: 47.5rem;
}

.auth-row {
  @apply grid items-center flex-none;
  grid-template-columns: 11.25rem minmax(0, 1fr);
  min-height: 3.25rem;
}

.auth-row.followed {
  border-bottom: 1px solid var(--line2);
}

.auth-label {
  @apply text-text-secondary;
}

.auth-value {
  @apply flex items-center;
}

.auth-note {
  @apply m-0 text-text-secondary;
  padding: 0.75rem 0;
  line-height: 1.55;
}
</style>
