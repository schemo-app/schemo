<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '../ui/Icon.vue'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger } from 'reka-ui'
import { useEnvironmentsStore } from '../../stores/environments'
import { useCommands } from '../../composables/useCommands'
import { useRequestEnvironment } from '../../composables/useRequestEnvironment'
import { dotOf } from '../../lib/environmentPalette'
import type { RequestSource } from '../../lib/requestSource'
import { t } from '../../i18n'

// The environment this one request goes out under, drawn beside the address it applies to: it is
// the last thing a person checks before pressing send.
//
// Following the window is the answer a request gives until told otherwise, and the bar says so
// by saying nothing: an unpinned request wears the plain stack, and only a pinned one is worth the
// room a name takes. The tooltip spells out which it is and which environment.
const { source } = defineProps<{ source: RequestSource }>()

const environments = useEnvironmentsStore()
const { hint } = useCommands()

const { pinned, label, options, choose } = useRequestEnvironment(source)

const open = ref(false)
const color = computed(() => dotOf(options.value.find(option => option.value === source.environmentId)?.color ?? ''))

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
  environments.openSheet()
}

const editHint = computed(() => hint('environments'))
</script>

<template>
  <Popover v-model:open="open">
    <!-- A trigger rather than an anchor: the button is what opens the panel, and reka exempts its own
         trigger from the outside-press that would otherwise close it under the finger. -->
    <div class="bar-env" :class="{ open, pinned }" :style="{ '--env-color': color }">
      <PopoverTrigger as-child>
        <button
          type="button"
          class="bar-env-trigger"
          :title
          :aria-label="title"
        >
          <span v-if="pinned" class="bar-env-dot"></span>
          <Icon v-else name="Database" :size="14" :stroke-width="1.9" />
          <span v-if="pinned" class="bar-env-name">{{ label }}</span>
        </button>
      </PopoverTrigger>
      <button v-if="pinned" type="button" class="bar-env-reset" :title="t('request.envReset')" :aria-label="t('request.envReset')" @click="pick('')">
        <Icon name="X" :size="10" :stroke-width="2.6" />
      </button>
    </div>
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
  @apply flex flex-none items-center;
  height: 1.625rem;
  margin-left: 0.75rem;
  margin-right: 0.375rem;
  border-radius: 0.375rem;
  --wails-draggable: no-drag;
}

.bar-env.pinned {
  padding: 0 0.25rem 0 0;
  background: color-mix(in srgb, var(--env-color) 16%, transparent);
}

.bar-env.pinned:hover,
.bar-env.pinned.open {
  background: color-mix(in srgb, var(--env-color) 22%, transparent);
}

.bar-env-trigger {
  @apply inline-flex items-center justify-center border-0 cursor-default text-text-tertiary;
  width: 1.75rem;
  height: 1.625rem;
  padding: 0;
  border-radius: 0.375rem;
  background: transparent;
  font: inherit;
  font-size: var(--text-12);
  font-weight: 500;
  line-height: normal;
}

.bar-env:not(.pinned):hover,
.bar-env.open:not(.pinned) {
  background: var(--bg-hover);
}

.bar-env:not(.pinned):hover .bar-env-trigger,
.bar-env.open:not(.pinned) .bar-env-trigger {
  color: var(--text-secondary);
}

.pinned .bar-env-trigger {
  width: auto;
  gap: 0.4375rem;
  padding: 0 0.4375rem 0 0.5625rem;
  color: var(--text);
}

.bar-env-dot {
  @apply flex-none rounded-full;
  width: 0.4375rem;
  height: 0.4375rem;
  background: var(--env-color);
}

.bar-env-reset {
  @apply inline-flex flex-none items-center justify-center border-0 text-text-secondary cursor-default;
  width: 1.125rem;
  height: 1.125rem;
  padding: 0;
  border-radius: 0.25rem;
  background: transparent;
}

.bar-env-reset:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.bar-env-name {
  @apply truncate;
  max-width: 8.125rem;
}
</style>
