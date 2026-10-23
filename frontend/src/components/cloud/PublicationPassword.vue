<script setup lang="ts">
import { ref } from 'vue'
import { t } from '../../i18n'
import Icon from '../ui/Icon.vue'
import { copyToClipboard } from '../../lib/clipboard'
import { useToast } from '../../composables/useToast'

const password = defineModel<string>({ required: true })
defineProps<{ disabled?: boolean; placeholder: string; tools?: boolean; save?: boolean; saveDisabled?: boolean }>()
defineEmits<{ save: [] }>()
const visible = ref(false)
const toast = useToast()

async function copy() {
  if (password.value && await copyToClipboard(password.value)) toast.show(t('common.copied'))
}

function generate() {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  password.value = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
  visible.value = true
}
</script>

<template>
  <div class="publication-password" :class="{ tools }">
    <div class="password-input">
      <input v-model="password" :type="visible ? 'text' : 'password'" autocomplete="new-password"
        :disabled="disabled" :aria-label="t('publications.password')" :placeholder="placeholder" />
      <template v-if="tools">
        <button type="button" :disabled="disabled || !password" :title="t('publications.copyPassword')"
          :aria-label="t('publications.copyPassword')" @click="copy"><Icon name="Copy" :size="14" /></button>
        <button type="button" :disabled="disabled" :title="t('publications.generatePassword')"
          :aria-label="t('publications.generatePassword')" @click="generate"><Icon name="RefreshCw" :size="14" /></button>
        <button type="button" :disabled="disabled" :title="t(visible ? 'publications.hidePassword' : 'publications.showPassword')"
          :aria-label="t(visible ? 'publications.hidePassword' : 'publications.showPassword')" :aria-pressed="visible"
          @click="visible = !visible"><Icon :name="visible ? 'EyeOff' : 'Eye'" :size="14" /></button>
      </template>
    </div>
    <button v-if="save" type="button" class="save-password" :disabled="disabled || saveDisabled" @click="$emit('save')">{{ t('common.save') }}</button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.publication-password {
  @apply flex;
  gap: 0.3rem;
  padding: 0.125rem 0 0.25rem;
}

.password-input {
  @apply flex items-center flex-1 min-w-0;
  height: 1.875rem;
  padding: 0 0.6rem;
  border-radius: 0.4375rem;
  background: var(--bg-inset);
  box-shadow: inset 0 0 0 1px var(--border-strong);
}

.password-input:focus-within {
  box-shadow: inset 0 0 0 1px var(--accent), 0 0 0 3px var(--accent-soft);
}

input {
  @apply flex-1 min-w-0 border-0 outline-none bg-transparent text-text;
  font: inherit;
}

.password-input button {
  @apply flex items-center justify-center flex-none border-0 bg-transparent text-text-secondary cursor-pointer;
  width: 1.5rem;
  height: 1.5rem;
  border-radius: 0.3rem;
}

.password-input button:hover {
  background: var(--bg-hover);
}

.save-password {
  @apply flex-none border-0 text-text cursor-pointer;
  padding: 0 0.75rem;
  border-radius: 0.4375rem;
  background: var(--chip);
  font: inherit;
}

.tools .password-input {
  height: 2rem;
  padding: 0 0.25rem 0 0.625rem;
  gap: 0.125rem;
  border-radius: 0.5rem;
}

.tools input {
  font-family: var(--mono);
  font-size: var(--text-12-5);
}

.tools .save-password {
  background: var(--accent);
  color: var(--accent-text);
  border-radius: 0.5rem;
  font-weight: 600;
}

button:disabled {
  @apply cursor-default opacity-45;
}
</style>
