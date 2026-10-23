<script setup lang="ts">

const { variant = 'outline', size = 'md' } = defineProps<{
  variant?: 'outline' | 'primary' | 'ghost' | 'quiet' | 'danger'
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'bar' | 'field' | 'pane'
}>()
</script>

<template>
  <button type="button" :class="['btn', `btn--${variant}`, `btn--${size}`]">
    <slot />
  </button>
</template>

<style scoped>
@reference "../../style.css";

.btn {
  @apply inline-flex flex-none items-center justify-center cursor-pointer whitespace-nowrap bg-bg-inset text-text;
  border: 1px solid var(--border-strong);
  font: inherit;
  transition: background 0.12s ease, border-color 0.12s ease, filter 0.12s ease;
  --wails-draggable: no-drag;
}

.btn:hover:not(:disabled) {
  @apply bg-bg-hover;
}

.btn:active:not(:disabled) {
  @apply bg-bg-active;
}

.btn:disabled {
  @apply opacity-50 cursor-default shadow-none;
}

.btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.btn--md {
  gap: 0.3rem;
  height: 1.625rem;
  padding: 0 0.75rem;
  border-radius: 0.3rem;
  font-size: var(--text-12);
}

.btn--sm {
  gap: 0.25rem;
  height: 1.5rem;
  padding: 0 0.5625rem;
  border-radius: 0.3rem;
  font-size: var(--text-12);
}

.btn--lg {
  gap: 0.3rem;
  height: 2rem;
  padding: 0 0.875rem;
  border-radius: 0.5rem;
  font-size: var(--text-13);
  font-weight: 500;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.18);
}

.btn--xl {
  gap: 0.5rem;
  height: 2.5rem;
  padding: 0 1.125rem;
  border-radius: 0.5rem;
  font-size: var(--text-14);
  font-weight: 600;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.18);
}

.btn--primary {
  @apply bg-accent border-accent text-accent-text;
}

.btn--primary:hover:not(:disabled) {
  @apply bg-accent border-accent brightness-110;
}

.btn--primary:active:not(:disabled) {
  @apply brightness-95;
}

.btn.btn--ghost,
.btn.btn--quiet {
  @apply border-transparent bg-transparent shadow-none;
  height: auto;
  font-size: var(--text-12);
}

.btn--ghost {
  @apply text-accent;
  padding: 0.125rem 0.25rem;
  border-radius: 0.25rem;
}

.btn--ghost.btn--md,
.btn--quiet.btn--md {
  padding: 0.25rem 0.5rem;
  border-radius: 0.3rem;
}

.btn--quiet {
  @apply text-text-secondary;
  padding: 0.25rem 0.5rem;
  border-radius: 0.3rem;
}

.btn--ghost:hover:not(:disabled) {
  @apply bg-accent-soft;
}

.btn--ghost:active:not(:disabled) {
  @apply bg-accent-soft brightness-95;
}

.btn--quiet:hover:not(:disabled) {
  @apply bg-bg-hover text-text;
}

.btn.btn--bar {
  gap: 0.3rem;
  height: 1.75rem;
  padding: 0 0.6rem;
  border-radius: 0.3rem;
  font-size: var(--text-12);
  font-weight: 500;
  background: var(--bg-inset);
  border-color: var(--border);
  box-shadow: none;
}

.btn.btn--field {
  gap: 0.3rem;
  height: 1.875rem;
  padding: 0 0.75rem;
  border-radius: 0.3rem;
  font-size: var(--text-13);
  font-weight: 500;
  box-shadow: none;
}

.btn.btn--pane {
  gap: 0.3rem;
  height: 2.125rem;
  padding: 0 0.875rem;
  border-radius: 0.5rem;
  font-size: var(--text-13);
  font-weight: 500;
}

.btn--pane.btn--primary {
  padding: 0 1rem;
  font-weight: 600;
}

.btn--danger {
  color: var(--red-text);
  border-color: var(--red-soft);
  background: transparent;
}

.btn--danger:hover:not(:disabled) {
  background: var(--red-soft);
}
</style>
