<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import { NodeKind, type NodeChild } from '../../../bindings/schemo/internal/domain'

// A container's members, browsable without opening each one: a click on a row goes there.
defineProps<{ children: NodeChild[] }>()

const emit = defineEmits<{ pick: [path: string] }>()

// A scalar's preview reads as the value it is; a nested container reads as its size.
function tone(kind: NodeKind): string {
  switch (kind) {
    case NodeKind.NodeString:
      return 'str'
    case NodeKind.NodeNumber:
    case NodeKind.NodeBoolean:
      return 'num'
    default:
      return 'muted'
  }
}
</script>

<template>
  <div class="children">
    <button
      v-for="child in children"
      :key="child.path"
      type="button"
      class="row"
      @click="emit('pick', child.path)"
    >
      <span class="key">{{ child.name }}</span>
      <span class="prev" :class="tone(child.kind)">{{ child.preview }}</span>
      <Icon name="ChevronRight" :size="10" :stroke-width="2.6" class="chev" />
    </button>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.children {
  @apply flex flex-col;
}

.row {
  @apply grid w-full cursor-pointer items-center gap-2.5 border-0 bg-transparent px-1.5 text-left;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.1fr) 0.625rem;
  height: 1.875rem;
  border-radius: 0.3rem;
  font-family: var(--mono);
  font-size: var(--text-12-5);
  --wails-draggable: no-drag;
}

.row:hover {
  background: var(--bg-hover);
}

.key {
  @apply min-w-0 truncate;
  color: var(--accent);
}

.prev {
  @apply min-w-0 truncate text-text;
}

.prev.str {
  color: var(--tok-str);
}

.prev.num {
  color: var(--tok-num);
}

.prev.muted {
  @apply text-text-tertiary;
}

.chev {
  @apply flex-none text-text-tertiary;
}
</style>
