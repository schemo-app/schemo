<script setup lang="ts">
import { computed, ref } from 'vue'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import Segment from '../ui/Segment.vue'
import { domainLabel, expiryInput, expiryValue } from '../../lib/cookies'
import { useMessages } from '../../i18n'
import type { JarCookie } from '../../../bindings/schemo/internal/domain'

// One cookie of the jar, written by hand. It is a copy of what the table holds — or a blank one — and
// nothing here reaches the jar until Save: the row a person is typing is a row that may never be.
const { cookie } = defineProps<{ cookie: JarCookie }>()

const emit = defineEmits<{
  (e: 'save', cookie: JarCookie): void
  (e: 'cancel'): void
}>()

const { t } = useMessages()

const fresh = computed(() => cookie.id === '')

// The domain is drawn with the dot a browser's cookie list writes it with, because that dot is the
// whole of how far the cookie reaches: Go reads it back off this field when the row is saved, and
// there is no switch beside it to disagree with.
const name = ref(cookie.name)
const value = ref(cookie.value)
const domain = ref(domainLabel(cookie.domain, cookie.hostOnly))
const path = ref(cookie.path || '/')
const expires = ref(expiryInput(cookie.expiresAt))
const sameSite = ref(cookie.sameSite || 'Lax')
const secure = ref(cookie.secure === true)
const httpOnly = ref(cookie.httpOnly === true)
const notice = ref('')

// What the expiry field says, read back: `undefined` is text that names no instant, which is the one
// answer this form will not save — the alternative is a date nobody typed, kept as a session cookie
// or as midnight of some day the person never named.
const at = computed(() => expiryValue(expires.value))

const sameSites = ['Lax', 'Strict', 'None'].map((option) => ({ value: option, label: option }))

function save() {
  const until = at.value
  if (until === undefined) {
    notice.value = t('cookies.expiryUnreadable')
    return
  }
  if (name.value.trim() === '') {
    notice.value = t('cookies.nameEmpty')
    return
  }
  if (domain.value.trim() === '') {
    notice.value = t('cookies.domainEmpty')
    return
  }
  notice.value = ''
  // How far the cookie reaches is not sent: it is the dot in the domain above, and Go reads it off
  // the field — a flag travelling beside it would be a second answer to the same question.
  emit('save', {
    ...cookie,
    name: name.value.trim(),
    value: value.value,
    domain: domain.value.trim(),
    path: path.value.trim() || '/',
    expiresAt: until,
    secure: secure.value,
    httpOnly: httpOnly.value,
    sameSite: sameSite.value,
  })
}
</script>

<template>
  <div class="pane">
    <div class="pane-body">
      <span class="pane-title">{{ fresh ? t('cookies.newCookie') : t('cookies.editCookie') }}</span>

      <div class="pair name-value">
        <label class="field">
          <span class="field-label">{{ t('cookies.name') }}</span>
          <input v-model="name" class="input mono" spellcheck="false" autocomplete="off" />
        </label>
        <label class="field">
          <span class="field-label">{{ t('cookies.value') }}</span>
          <input v-model="value" class="input mono" spellcheck="false" autocomplete="off" />
        </label>
      </div>

      <div class="pair domain-path">
        <label class="field">
          <span class="field-label">{{ t('cookies.domain') }}</span>
          <input
            v-model="domain"
            class="input mono"
            spellcheck="false"
            autocomplete="off"
            :placeholder="t('cookies.domainPlaceholder')"
          />
        </label>
        <label class="field">
          <span class="field-label">{{ t('cookies.path') }}</span>
          <input v-model="path" class="input mono" spellcheck="false" autocomplete="off" />
        </label>
      </div>

      <label class="field">
        <span class="field-label">{{ t('cookies.expires') }}</span>
        <input
          v-model="expires"
          class="input mono"
          spellcheck="false"
          autocomplete="off"
          :placeholder="t('cookies.expiryPlaceholder')"
        />
        <span class="field-hint">{{ t('cookies.expiryHint') }}</span>
      </label>

      <div class="pair attributes">
        <div class="field">
          <span class="field-label">{{ t('cookies.sameSite') }}</span>
          <Segment :options="sameSites" :value="sameSite" @pick="sameSite = $event" />
        </div>
        <div class="field flags">
          <label class="flag">
            <Checkbox
              :model-value="httpOnly"
              @update:model-value="httpOnly = $event === true"
            />
            <span>{{ t('cookies.httpOnly') }}</span>
          </label>
          <label class="flag">
            <Checkbox :model-value="secure" @update:model-value="secure = $event === true" />
            <span>{{ t('cookies.secure') }}</span>
          </label>
        </div>
      </div>
    </div>

    <div class="foot">
      <span class="notice" :class="{ bad: !!notice }">
        {{ notice || (fresh ? t('cookies.editNoteNew') : t('cookies.editNoteExisting')) }}
      </span>
      <Button size="pane" @click="emit('cancel')">{{ t('common.cancel') }}</Button>
      <Button size="pane" variant="primary" @click="save">{{ t('common.save') }}</Button>
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

.field {
  @apply flex flex-col gap-1.5 min-w-0;
}

.field-label {
  @apply section-label;
}

.field-hint {
  @apply text-11-5 text-text-tertiary;
}

.pair {
  @apply grid gap-3.5 items-end;
}

.name-value,
.attributes {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
}

.domain-path {
  grid-template-columns: minmax(0, 1fr) 8.125rem;
}

.attributes .flags {
  @apply flex-row items-center gap-4.5;
  padding-bottom: 0.25rem;
}

.input {
  @apply w-full h-8.5 px-2.5 rounded-lg text-13 text-text outline-none;
  font-family: inherit;
  background: var(--bg-inset);
  border: 1px solid var(--border-strong);
}

.input.mono {
  font-family: var(--mono);
}

.input::placeholder {
  @apply text-text-tertiary;
}

.input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.flag {
  @apply flex items-center gap-2 cursor-pointer text-13 text-text;
}

.foot {
  @apply flex-none flex items-center h-12 px-5 gap-2.5;
  border-top: 1px solid var(--border);
}

.foot .notice {
  @apply flex-1 min-w-0 text-13 text-text-tertiary;
}

.notice.bad {
  @apply text-red;
}
</style>
