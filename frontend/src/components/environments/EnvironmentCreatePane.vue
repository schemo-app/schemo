<script setup lang="ts">
import { useColorLabels } from '../../composables/useColorLabels'
import { computed, ref } from 'vue'
import ColorSwatches from '../ui/ColorSwatches.vue'
import Segment from '../ui/Segment.vue'
import Button from '../ui/Button.vue'
import { DEFAULT_ENVIRONMENT_COLOR, ENVIRONMENT_COLORS, ENVIRONMENT_TINTS } from '../../lib/environmentPalette'
import { useEnvironmentsStore } from '../../stores/environments'
import { useSheetNotice } from '../../composables/useSheetNotice'
import { t } from '../../i18n'

// The create form: a name, a colour, and what to fill the new environment from. The three are one
// action because an environment usually starts as a copy of another, and asking for the name first
// would mean asking for the rest under a heading that already exists.
const { pendingCount } = defineProps<{ pendingCount: number }>()

const emit = defineEmits<{
  (e: 'cancel'): void
  (e: 'created'): void
  (e: 'import'): void
}>()

const environments = useEnvironmentsStore()
const { notice, clearNotice, withNotice } = useSheetNotice()

// One name per swatch, so a colour is readable without a legend. A computed and not a plain object:
// a map made once would keep the words of the language it was made in. It is checked against the
// palette, since a colour with no word and a word left behind by a removed colour are both
// failures.
const colorLabels = useColorLabels('environments', ENVIRONMENT_COLORS)

const name = ref('')
const color = ref(DEFAULT_ENVIRONMENT_COLOR)
// Empty is the blank environment; anything else is an id to copy from.
const base = ref('')

const bases = computed(() => [
  { value: '', label: t('environments.baseEmpty') },
  ...environments.environments.map((environment) => ({ value: environment.id, label: environment.name })),
])

const baseEnv = computed(() => environments.environments.find((environment) => environment.id === base.value) ?? null)

// What the Variables card says before there is anything to show. It is the one place the form can
// answer "what will I get", and the answer differs by which base was chosen.
const note = computed(() => {
  const parts: string[] = []
  const chosen = baseEnv.value

  if (chosen) {
    parts.push(t('environments.createFromBase', { n: chosen.vars.length, name: chosen.name }))
    if (chosen.vars.some((variable) => variable.kind === 'secret')) parts.push(t('environments.createSecretsNote'))
  } else {
    parts.push(t('environments.createEmptyNote'))
  }

  // A .env file chosen before the environment exists has nowhere to go yet: it is written in as soon
  // as the form makes one, and the form says so rather than looking like it forgot.
  if (pendingCount > 0) parts.push(t('environments.importPending', { n: pendingCount }))

  return parts.join(' ')
})

async function create() {
  if (await withNotice(() => environments.createEnv({ name: name.value, color: color.value, startFrom: base.value }))) {
    emit('created')
  }
}

function cancel() {
  clearNotice()
  emit('cancel')
}
</script>

<template>
  <div class="pane">
    <div class="pane-body">
      <span class="pane-title">{{ t('environments.new') }}</span>

      <div class="pair">
        <label class="field">
          <span class="field-label">{{ t('environments.nameLabel') }}</span>
          <input
            v-model="name"
            class="input"
            maxlength="40"
            spellcheck="false"
            :placeholder="t('environments.namePlaceholder')"
            @keydown.enter="create"
          />
        </label>

        <div class="field tight">
          <span class="field-label">{{ t('environments.colorLabel') }}</span>
          <ColorSwatches
            :value="color"
            :colors="ENVIRONMENT_COLORS"
            :tints="ENVIRONMENT_TINTS"
            :labels="colorLabels"
            @pick="color = $event"
          />
        </div>
      </div>

      <div class="field">
        <span class="field-label">{{ t('environments.startFrom') }}</span>
        <Segment class="bases" :options="bases" :value="base" @pick="base = $event" />
      </div>

      <div class="field">
        <span class="field-label">{{ t('environments.variablesLabel') }}</span>
        <div class="card">
          <div class="card-note">{{ note }}</div>
          <div class="card-row">
            <span class="card-text">{{ t('environments.bringKeys') }}</span>
            <button type="button" class="card-button" @click="emit('import')">
              {{ t('environments.importEnv') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <div class="foot">
      <span class="notice" :class="{ bad: !!notice }">{{ notice }}</span>
      <span class="head-spacer"></span>
      <Button size="pane" @click="cancel">{{ t('common.cancel') }}</Button>
      <Button size="pane" variant="primary" @click="create">
        {{ t('environments.createAction') }}
      </Button>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex-1 min-w-0 flex flex-col min-h-0;
}

.pane-body {
  @apply flex-1 min-h-0 overflow-y-auto flex flex-col gap-4.5;
  padding: 1.125rem 1.25rem;
}

.pane-title {
  @apply text-14 font-semibold;
}

.pair {
  @apply flex gap-3.5 items-end;
}

.field {
  @apply flex flex-col gap-1.5 min-w-0;
}

.pair > .field {
  @apply flex-1;
}

.pair > .field.tight {
  @apply flex-none;
}

.field-label {
  @apply section-label;
}

.input {
  @apply w-full h-8.5 px-2.5 rounded-lg text-13 text-text outline-none;
  font-family: inherit;
  background: var(--bg-panel);
  border: 1px solid var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.input::placeholder {
  @apply text-text-tertiary;
}

.bases {
  max-width: 18.75rem;
}

.card {
  @apply border border-border rounded-xl overflow-hidden;
}

.card-note {
  @apply flex items-center h-10 px-3 text-13 text-text-secondary;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
}

.card-row {
  @apply flex items-center gap-2.5 h-11 px-3;
}

.card-text {
  @apply flex-1 min-w-0 text-13 text-text-tertiary;
}

.card-button {
  @apply flex-none h-7.5 px-3 rounded-[0.3rem] cursor-pointer text-13 font-medium text-text;
  font-family: inherit;
  background: var(--bg-panel);
  border: 1px solid var(--border-strong);
}

.card-button:hover {
  @apply bg-bg-hover;
}

.foot .notice {
  @apply text-13 text-text-tertiary;
}

.notice.bad {
  @apply text-red;
}

.foot {
  gap: 0.625rem;
  @apply flex-none flex items-center h-12 px-5;
  border-top: 1px solid var(--border);
}

</style>
