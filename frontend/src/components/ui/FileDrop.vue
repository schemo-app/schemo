<script setup lang="ts">
import { useId } from 'vue'
import { Primitive, type PrimitiveProps } from 'reka-ui'
import { useWailsEvent } from '../../composables/useWailsEvent'

const { as = 'div', disabled = false, multiple = false } = defineProps<{
    as?: PrimitiveProps['as']
    /** Turns off the drop target while keeping its contents mounted. */
    disabled?: boolean
    /** Emits every dropped path; the default selects only the first file. */
    multiple?: boolean
}>()

const emit = defineEmits<{ files: [paths: string[]] }>()
const targetId = useId()

// The generated id must match in the DOM and in the native event handler.
defineOptions({ inheritAttrs: false })

useWailsEvent('files-dropped', ({ data }) => {
    if (disabled || data.target !== targetId || !data.files?.length) return
    emit('files', multiple ? data.files : data.files.slice(0, 1))
})
</script>

<template>
    <Primitive
        :as
        v-bind="$attrs"
        :id="targetId"
        class="file-drop"
        :data-file-drop-target="disabled ? undefined : ''"
        :data-disabled="disabled ? '' : undefined"
    >
        <slot />
    </Primitive>
</template>

<style scoped>
.file-drop.file-drop-target-active:not([data-disabled]) {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
}
</style>
