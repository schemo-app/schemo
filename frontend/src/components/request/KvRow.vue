<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import Checkbox from '../ui/Checkbox.vue'
import { t } from '../../i18n'

// One row of a key/value table: the box that switches it on, its name, and a value the caller
// draws. The tools are a slot (a form has one more) and the numbers are in style.css. A table whose
// order may be dragged says so, and the row then leads with the drag handle. A table whose rows
// have no on and off, like an example's headers, says that too and keeps no box.
const {
  enabled,
  name,
  rowId,
  namePlaceholder,
  grabbable = false,
  checkable = true,
  nameReadonly = false,
  removable = true,
} = defineProps<{
  enabled: boolean
  name: string
  /** The row's own id, written on the field: it is how a pane puts the caret in a row it just made. */
  rowId: string
  namePlaceholder: string
  grabbable?: boolean
  checkable?: boolean
  /** A name that is not the row's to change: a path parameter takes it from the address. */
  nameReadonly?: boolean
  removable?: boolean
}>()

const emit = defineEmits<{
  toggle: []
  rename: [value: string]
  remove: []
  grab: [event: PointerEvent]
}>()

</script>

<template>
  <div class="kv-row" :class="{ off: !enabled }" :data-kv="rowId">
    <span v-if="grabbable" class="kv-anchor">
      <button
        type="button"
        class="kv-grip"
        :title="t('request.pane.reorder')"
        @pointerdown="emit('grab', $event)"
      >
        <Icon name="GripVertical" :size="12" :stroke-width="1.9" />
      </button>
    </span>
    <Checkbox
      v-if="checkable"
      :model-value="enabled"
      :title="enabled ? t('request.pane.sent') : t('request.pane.skipped')"
      @update:model-value="emit('toggle')"
    />
    <span class="kv-cell">
      <input
        :value="name"
        :data-row="rowId"
        class="kv-input mono"
        :placeholder="namePlaceholder"
        :readonly="nameReadonly"
        spellcheck="false"
        @input="emit('rename', ($event.target as HTMLInputElement).value)"
      />
    </span>
    <span class="kv-cell"><slot /></span>
    <span v-if="$slots.desc" class="kv-cell"><slot name="desc" /></span>
    <span class="kv-tools">
      <slot name="tools" />
      <button
        v-if="removable"
        type="button"
        class="kv-remove"
        :title="t('request.pane.remove')"
        @click="emit('remove')"
      >
        <Icon name="X" :size="12" :stroke-width="2.2" />
      </button>
    </span>
  </div>
</template>
