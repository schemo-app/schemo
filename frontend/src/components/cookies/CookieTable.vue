<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import IconButton from '../ui/IconButton.vue'
import { useCookiesStore } from '../../stores/cookies'
import { useWorkspacesStore } from '../../stores/workspaces'
import { domainLabel, expiryLabel, flagBadges } from '../../lib/responseCookies'
import { t } from '../../i18n'

// The cookies of the domain on screen, one row each. Nothing here is typed in: a row is read, and
// the two things a person does to it — change it or take it away — are the buttons at its end.
const cookies = useCookiesStore()
const workspaces = useWorkspacesStore()
</script>

<template>
  <div class="card">
    <div v-if="cookies.rows.length" class="grid card-head">
      <div>{{ t('cookies.name') }}</div>
      <div>{{ t('cookies.value') }}</div>
      <div>{{ t('cookies.path') }}</div>
      <div>{{ t('cookies.expires') }}</div>
      <div>{{ t('cookies.flags') }}</div>
      <div></div>
      <div></div>
    </div>

    <div v-for="cookie in cookies.rows" :key="cookie.id" class="grid row">
      <div class="cell">
        <span class="cell-text name mono" :title="cookie.name">{{ cookie.name }}</span>
      </div>
      <div class="cell">
        <span class="cell-text mono" :title="cookie.value">{{ cookie.value }}</span>
      </div>
      <div class="cell">
        <span class="cell-text quiet">{{ cookie.path }}</span>
      </div>
      <div class="cell">
        <span class="cell-text quiet">{{ expiryLabel(cookie.expiresAt) }}</span>
      </div>
      <div class="cell">
        <span class="cell-text quiet flags" :title="domainLabel(cookie.domain, cookie.hostOnly)">
          <abbr v-for="flag in flagBadges(cookie)" :key="flag.text" class="flag" :title="flag.hint">
            {{ flag.text }}
          </abbr>
        </span>
      </div>
      <div class="cell cell-icon">
        <IconButton
          size="xl"
          :hint="t('cookies.edit')"
          :disabled="!workspaces.canWrite"
          @click="cookies.edit(cookie)"
        >
          <Icon name="Pencil" :size="14" :stroke-width="1.9" />
        </IconButton>
      </div>
      <div class="cell cell-icon">
        <IconButton
          variant="danger"
          size="xl"
          :hint="t('cookies.remove')"
          :disabled="!workspaces.canWrite"
          @click="cookies.remove(cookie.id)"
        >
          <Icon name="X" :size="14" :stroke-width="1.9" />
        </IconButton>
      </div>
    </div>

    <div v-if="cookies.rows.length === 0" class="none">{{ t('cookies.empty') }}</div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.card {
  @apply overflow-hidden;
}

.grid {
  @apply grid items-stretch;
  grid-template-columns: 8rem minmax(0, 1fr) 3.5rem 5.75rem 6.75rem 2rem 2rem;
  padding: 0 0.75rem;
}

.card-head {
  @apply items-center text-text-tertiary;
  height: 1.875rem;
  font-size: var(--text-11-5);
  border-bottom: 1px solid var(--border);
}

.row {
  @apply border-b;
  height: 2.375rem;
  border-color: var(--line2);
}

.row:hover {
  @apply bg-bg-hover;
}

.cell {
  @apply flex items-center min-w-0;
  padding-right: 0.75rem;
}

.cell-text {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-text;
  font-size: var(--text-12-5);
}

.cell-text.mono {
  font-family: var(--mono);
}

.cell-text.name {
  color: var(--accent);
}

.cell-text.quiet {
  @apply text-text-tertiary;
}

.flag {
  text-decoration: none;
}

.flag + .flag::before {
  content: '·';
  margin: 0 0.3125rem;
}

.cell-icon {
  @apply justify-center;
  padding-right: 0;
}

.none {
  @apply flex items-center text-13 text-text-secondary border-b;
  height: 2.375rem;
  padding: 0 0.75rem;
  border-color: var(--line2);
}
</style>
