<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '../ui/Icon.vue'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { useEnvironmentsStore } from '../../stores/environments'
import { useCommands } from '../../composables/useCommands'
import { useRequestEnvironment } from '../../composables/request/useRequestEnvironment'
import { dotOf } from '../environments/palette'
import type { RequestSource } from '../../lib/requestSource'
import { useMessages } from '../../i18n'

// The environment this one request goes out under, drawn beside the address it applies to: it is the
// last thing a person checks before pressing send.
//
// Following the window is the answer a request gives until it is told otherwise, and the drawing says
// so by saying nothing: an unpinned request wears the plain stack, and only a pinned one is worth the
// room a name takes. Which of the two it is, and which environment that is, the tooltip spells out —
// a name on every request would be the same name on every request.
const { source } = defineProps<{ source: RequestSource }>()

const envStore = useEnvironmentsStore()
const { hint } = useCommands()
const { t } = useMessages()

const { pinned, label, options, choose } = useRequestEnvironment(source)

const open = ref(false)

// What the button says when there is no name on it, and what it says behind the name when there is.
const title = computed(() =>
  pinned.value
    ? t('request.envPinnedTitle', { name: label.value })
    : t('request.envFollowTitle', { name: label.value })
)

function pick(id: string) {
  open.value = false
  choose(id)
}

// Editing is not one of the choices — it is another window — so the popover closes behind it.
function editVariables() {
  open.value = false
  envStore.openSheet()
}

const editHint = computed(() => hint('environments'))
</script>

<template>
  <Popover v-model:open="open">
    <!-- A trigger rather than an anchor: the button is what opens the panel, and reka exempts its own
         trigger from the outside-press that would otherwise close it under the finger. -->
    <PopoverTrigger as-child>
      <button
        type="button"
        class="bar-env"
        :class="{ open, pinned }"
        :title
        :aria-label="title"
      >
        <Icon name="Database" :size="16" />
        <span v-if="pinned" class="bar-env-name cap-line">{{ label }}</span>
      </button>
    </PopoverTrigger>
    <PopoverContent class="bar-env-pop" align="end" :side-offset="6">
      <div class="env-head">{{ t('request.envFor') }}</div>
      <button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="env-row"
        :class="{ active: option.value === source.environmentId }"
        @click="pick(option.value)"
      >
        <span class="env-row-mark">
          <span v-if="option.color" class="env-row-dot" :style="{ background: dotOf(option.color) }"></span>
        </span>
        <span class="env-row-name cap-line">{{ option.name }}</span>
        <Icon v-if="option.readonly" name="Lock" :size="12" class="env-row-lock" />
        <span class="env-row-count">{{ option.vars }}</span>
        <span class="env-row-check">
          <Icon
            v-if="option.value === source.environmentId"
            name="Check"
            :size="12"
            :stroke-width="2.6"
          />
        </span>
      </button>

      <div class="env-divider"></div>

      <button type="button" class="env-row env-row-action" @click="editVariables">
        <span class="env-row-mark"><Icon name="AlignLeft" :size="14" :stroke-width="1.8" /></span>
        <span class="env-row-name cap-line">{{ t('environments.editVariables') }}</span>
        <span v-if="editHint" class="env-row-hint">{{ editHint }}</span>
      </button>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.bar-env {
  @apply flex-none flex items-center gap-1.5 h-8.5 px-2 rounded-[0.375rem] cursor-pointer;
  border: 0;
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
  font-size: var(--text-12);
  font-weight: 500;
  --wails-draggable: no-drag;
}

.bar-env.pinned {
  padding: 0 0.625rem 0 0.5625rem;
  background: var(--accent-soft);
  color: var(--accent);
}

.bar-env:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.bar-env.open {
  background: var(--bg-hover);
}

.bar-env svg {
  fill: none;
}

.bar-env.pinned svg {
  fill: var(--accent-soft);
}

.bar-env-name {
  @apply truncate;
  max-width: 8.125rem;
}
</style>
