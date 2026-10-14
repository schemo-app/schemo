<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useTimeoutFn } from '@vueuse/core'
import Icon from '../ui/Icon.vue'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import { Input } from '../ui/input'
import Segment from '../ui/Segment.vue'
import { CaptureSource, VariableKind, VarScope, type Capture } from '../../../bindings/schemo/internal/domain'
import type { RequestSource } from '../../lib/requestSource'
import { isVariableName } from '../../lib/vars'
import { SECRET_MASK } from '../../lib/layout'
import { runOrToast } from '../../stores/calls'
import { useEnvironmentsStore } from '../../stores/environments'
import { useMessages } from '../../i18n'

// Takes the value into a variable, the step between one request and the next. It guesses the name,
// the place and whether it is a secret, so that the usual case is one press.
const { suggested, secretByDefault, source, path, value, host, heading } = defineProps<{
  /** The name the variable is offered under. */
  suggested: string
  secretByDefault: boolean
  /** Where in the response the value is, for the rule that keeps it up to date. */
  source: CaptureSource
  path: string
  /** What is saved, read when the button is pressed: a container's text is a call away. */
  value: () => Promise<string | null>
  /** The request the response is of, which can capture the value on every send; none for a
   *  response that is not of a request of ours. */
  host: RequestSource | null
  /** Where the value comes from, said under the title; absent inside the inspector, which already
   *  says so around the form. */
  heading?: string
}>()

const emit = defineEmits<{ done: []; cancel: [] }>()

const { t } = useMessages()
const envs = useEnvironmentsStore()

const name = ref(suggested)
const where = ref<'environment' | 'globals'>(envs.activeEnvironment ? 'environment' : 'globals')
const secret = ref(secretByDefault)

// Keeping the value up to date is the point of most saves: the id a creation answered with is the
// address the next request asks for.
const keep = ref(!!host?.scriptsLevel)

const valid = computed(() => isVariableName(name.value.trim()))

// The template cannot write "{{" itself: its own mustache syntax would swallow it.
const braceOpen = '{{'
const braceClose = '}}'

// Read once, so the preview below the form and the value saved are the same call's answer.
const preview = ref<string | null>(null)
onMounted(async () => (preview.value = await value()))

const previewLine = computed(() => {
  if (preview.value === null) return ''
  const shown = secret.value ? SECRET_MASK : preview.value
  return shown.length > 80 ? shown.slice(0, 80) + '…' : shown
})

// What a save can be undone back to: the variable's own state before it, and the rule's.
interface Saved {
  name: string
  envId: string | null
  varId: string
  prior: { value: string; secret: boolean } | null
  priorRule: Capture | undefined
  ruleAdded: boolean
}
const saved = ref<Saved | null>(null)
const dismiss = useTimeoutFn(
  () => {
    saved.value = null
    emit('done')
  },
  4000,
  { immediate: false }
)

async function save() {
  if (!valid.value) return
  const trimmed = name.value.trim()
  const envId = where.value === 'globals' ? null : (envs.activeEnvironment?.id ?? null)
  const existing = envs.varsOf(envId).find((v) => v.name === trimmed)
  const prior = existing
    ? { value: envs.effectiveValue(existing), secret: existing.kind === VariableKind.VariableSecret }
    : null

  const text = await value()
  if (text === null) return
  const wrote = await runOrToast(
    envs.capture(where.value, trimmed, text, secret.value),
    'inspector.saveFailed'
  )
  if (!wrote) return

  const priorRule = host?.captures.find((rule) => rule.name === trimmed)
  if (keep.value) await captureOnEverySend(trimmed)

  saved.value = {
    name: trimmed,
    envId,
    varId: envs.varsOf(envId).find((v) => v.name === trimmed)?.id ?? '',
    prior,
    priorRule,
    ruleAdded: keep.value,
  }
  dismiss.start()
}

// A rule for the same variable is replaced, not doubled: two rules with one name would each win by
// turns.
async function captureOnEverySend(varName: string) {
  const rule = {
    id: '',
    name: varName,
    source,
    path,
    scope: where.value === 'globals' ? VarScope.ScopeGlobals : VarScope.ScopeEnvironment,
    secret: secret.value,
  }
  await host?.saveCaptures([...(host.captures.filter((one) => one.name !== rule.name)), rule])
}

async function undo() {
  const s = saved.value
  if (!s) return
  dismiss.stop()
  if (s.prior) {
    await envs.updateVar(s.envId, s.varId, {
      value: s.prior.value,
      kind: s.prior.secret ? VariableKind.VariableSecret : VariableKind.VariableText,
    })
  } else {
    await envs.removeVar(s.envId, s.varId)
  }
  if (s.ruleAdded && host) {
    const rest = host.captures.filter((rule) => rule.name !== s.name)
    await host.saveCaptures(s.priorRule ? [...rest, s.priorRule] : rest)
  }
  saved.value = null
  emit('done')
}
</script>

<template>
  <div class="save" @keydown.esc="emit('cancel')">
    <div v-if="saved" class="done">
      <Icon name="Check" :size="14" :stroke-width="2.6" class="done-icon" />
      <span class="done-text">{{ t('inspector.saved', { name: saved.name }) }}</span>
      <button type="button" class="undo" @click="undo">{{ t('inspector.undo') }}</button>
    </div>

    <form v-else class="form" :class="{ bare: !!heading }" @submit.prevent="save">
      <div v-if="heading" class="heading">
        <span class="heading-title">{{ t('inspector.saveToVariable') }}</span>
        <span class="heading-sub">{{ heading }}</span>
      </div>

      <label class="field">
        <span class="label">{{ t('inspector.save.name') }}</span>
        <div class="name-box">
          <span class="brace">{{ braceOpen }}</span>
          <Input v-model="name" variant="bare" size="sm" mono autofocus class="name-input" />
          <span class="brace">{{ braceClose }}</span>
        </div>
      </label>
      <p v-if="!valid" class="hint bad">{{ t('inspector.save.badName') }}</p>

      <div class="field">
        <span class="label">{{ t('inspector.save.where') }}</span>
        <Segment
          :options="[
            {
              value: 'environment',
              label: envs.activeEnvironment?.name ?? t('inspector.save.noEnvironment'),
              soon: !envs.activeEnvironment,
            },
            { value: 'globals', label: t('inspector.save.globals') },
          ]"
          :value="where"
          size="sm"
          @pick="(v) => (where = v as 'environment' | 'globals')"
        />
      </div>

      <label class="toggle-row">
        <Checkbox v-model="secret" tone="secret" />
        <span class="toggle-copy">
          <span class="toggle-title">{{ t('inspector.save.secret') }}</span>
          <span class="toggle-hint">{{ t('inspector.save.secretHint') }}</span>
        </span>
      </label>

      <label v-if="host?.scriptsLevel" class="toggle-row">
        <Checkbox v-model="keep" />
        <span class="toggle-copy">
          <span class="toggle-title">{{ t('inspector.save.keep') }}</span>
          <span class="toggle-hint">{{ t('inspector.save.keepHint') }}</span>
        </span>
      </label>

      <div class="preview">{{ previewLine }}</div>

      <div class="buttons">
        <span class="keys">{{ t('inspector.save.hint') }}</span>
        <Button type="button" variant="outline" size="md" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </Button>
        <Button type="submit" variant="primary" size="md" :disabled="!valid">
          {{ t('inspector.save.submit') }}
        </Button>
      </div>
    </form>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.save {
  @apply rounded-lg;
}

.form {
  @apply flex flex-col gap-3 rounded-lg p-3;
  background: var(--bg-hover);
}

.form.bare {
  @apply rounded-none p-0;
  background: transparent;
}

.field {
  @apply flex flex-col gap-1.5;
}

.label {
  @apply section-label;
}

.hint.bad {
  @apply text-12 text-red;
}

.heading {
  @apply flex flex-col gap-0.5;
}

.heading-title {
  @apply text-13 font-semibold text-text;
}

.heading-sub {
  @apply text-12 text-text-tertiary;
}

.name-box {
  @apply flex items-center gap-0.5 rounded-[0.375rem] px-2.5 text-13;
  height: 1.875rem;
  font-family: var(--mono);
  background: var(--bg-inset);
  border: 1px solid var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.brace {
  @apply flex-none text-text-tertiary;
}

.name-box :deep(.name-input) {
  @apply flex-1 min-w-0 px-0 text-13;
  height: auto;
}

.toggle-row {
  @apply flex cursor-pointer items-start gap-2.5;
  padding: 0.125rem 0;
}

.toggle-copy {
  @apply flex min-w-0 flex-col gap-0.5;
}

.toggle-title {
  @apply text-13;
}

.toggle-hint {
  @apply text-11-5 leading-[1.4] text-text-tertiary;
}

.preview {
  @apply overflow-hidden truncate rounded-md px-2 py-1.5 text-12 text-text-secondary;
  font-family: var(--mono);
  background: var(--bg-inset);
}

.buttons {
  @apply flex items-center justify-end gap-2;
}

.keys {
  @apply flex-1 text-11 text-text-tertiary;
}

.done {
  @apply flex items-center gap-2 rounded-lg px-2.5 py-2 text-12-5 bg-green-soft;
}

.done-icon {
  @apply flex-none;
  color: var(--green-text);
}

.done-text {
  @apply min-w-0 flex-1 truncate text-text;
}

.undo {
  @apply flex-none cursor-pointer rounded-sm border-0 bg-transparent px-1 text-12 font-medium text-text-secondary;
  --wails-draggable: no-drag;
}

.undo:hover {
  @apply text-text;
  background: var(--bg-hover);
}
</style>
