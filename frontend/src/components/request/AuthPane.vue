<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import Button from '../ui/Button.vue'
import AuthFields from './AuthFields.vue'
import PaneSelect from './PaneSelect.vue'
import { useAuthSchemes } from '../../composables/useAuthSchemes'
import type { RequestSource } from '../../lib/requestSource'
import { formatCheckedAt, textOf, t } from '../../i18n'
import type { Auth } from '../../../bindings/schemo/internal/domain'

const { source } = defineProps<{ source: RequestSource }>()

const { menuFor, nameOf, schemeOf } = useAuthSchemes()

const scheme = computed(() => schemeOf(source.auth.type))

const options = computed(() => menuFor(source.canInherit))

const label = computed(() => nameOf(source.auth.type))
const sends = computed(() => (scheme.value?.sends ? textOf(scheme.value.sends) : ''))

const inheritedLabel = computed(() => {
  const auth = source.inheritedAuth
  if (!auth) return t('request.inheritedNothing')
  const above = schemeOf(auth.type)
  return t('request.inherited', { kind: above ? textOf(above.label) : auth.type })
})

function selectAuth(type: string) {
  if (type === source.auth.type) return
  void source.setAuth({ ...source.auth, type: type as Auth['type'] })
}

function setField(key: string, value: string) {
  void source.setAuth({ ...source.auth, fields: { ...source.auth.fields, [key]: value } })
}

const tokenState = computed(() => {
  if (!source.token?.held) return t('request.auth.noToken')
  if (!source.token.expiresAt) return t('request.auth.tokenHeld')
  return t('request.auth.tokenUntil', { at: formatCheckedAt(source.token.expiresAt) })
})
</script>

<template>
  <div class="auth-pane">
    <div class="auth-head">
      <PaneSelect :label :options :current="source.auth.type" @select="selectAuth" />
      <span class="auth-sends mono" :title="sends">{{ sends }}</span>
    </div>

    <div v-if="scheme?.note" class="auth-note">{{ textOf(scheme.note) }}</div>
    <div v-if="source.auth.type === 'inherit'" class="auth-note">{{ inheritedLabel }}</div>

    <AuthFields
      v-if="scheme?.fields?.length"
      :scheme
      :auth="source.auth"
      scale="rows"
      @update="setField"
    />

    <!-- The part of an authorization that is not a field: a token somebody else issues, and the two
         things a person can do about it. -->
    <div v-if="scheme?.fetches && source.token" class="token">
      <div class="token-state">
        <Icon name="Lock" :size="12" />
        <span>{{ tokenState }}</span>
      </div>
      <div class="token-actions">
        <Button variant="primary" size="sm" @click="source.obtainAuth()">
          {{ t('request.auth.obtain') }}
        </Button>
        <Button variant="outline" size="sm" :disabled="!source.token.held" @click="source.forgetAuth()">
          {{ t('request.auth.forget') }}
        </Button>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.auth-pane {
  @apply flex flex-col;
  padding: 0.625rem 1.25rem 0.875rem;
}

.auth-head {
  @apply flex items-center gap-2.5;
  height: 2rem;
  margin-bottom: 0.625rem;
}

.auth-sends {
  @apply flex-1 min-w-0 truncate text-text-tertiary;
  font-size: var(--text-12-5);
}

.auth-note {
  @apply text-13 text-text-secondary;
  line-height: 1.5;
  padding: 0.5rem 0;
  border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.token {
  @apply flex items-center gap-3 p-2.5 rounded-lg bg-bg-sidebar border border-border;
  margin: 0.5rem 0;
}

.token-state {
  @apply flex flex-1 items-center gap-1.5 text-12 text-text-secondary;
}

.token-state svg {
  @apply flex-none;
}

.token-actions {
  @apply flex gap-1.5;
}

</style>
