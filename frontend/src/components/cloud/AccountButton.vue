<script setup lang="ts">
import { computed, ref } from 'vue'
import { System } from '../../../bindings/schemo/internal/app'
import { useAccount } from '../../composables/useAccount'
import { useSyncStore, syncLine } from '../../stores/sync'
import { useWorkspacesStore } from '../../stores/workspaces'
import { t } from '../../i18n'
import PopoverContent from '../ui/PopoverContent.vue'
import { PopoverRoot as Popover, PopoverTrigger, PopoverClose } from 'reka-ui'
import Icon from '../ui/Icon.vue'

const { compact = false } = defineProps<{ compact?: boolean }>()
const sync = useSyncStore()
const workspaces = useWorkspacesStore()
const accountOpen = ref(false)
const { state, serverDown, syncing, lastReach, checkServer, openSignIn, openSignOut } = useAccount()
const account = computed(() => state.value?.account ?? null)
const signedIn = computed(() => state.value?.signedIn === true)
const accountMeta = computed(() =>
  [account.value?.server ?? '', syncLine(sync.workspaces, workspaces.activeId, sync.waitingCount)]
    .filter(Boolean)
    .join(' · ')
)

function onAccountOpen(open: boolean) {
  if (open) void checkServer()
}

function openSettings(category = '') {
  System.ShowSettings(category)
}
</script>

<template>
  <Popover v-model:open="accountOpen" @update:open="onAccountOpen">
    <PopoverTrigger as-child>
      <button class="foot-account" :class="{ compact }" :title="account?.email || t('account.signedOut')">
        <Icon name="User" :size="16" :stroke-width="1.7" class="foot-icon" />
        <span v-if="!compact" class="foot-email cap-line">{{ account?.email || t('account.signedOut') }}</span>
      </button>
    </PopoverTrigger>
    <PopoverContent class="account-menu" :side="compact ? 'right' : 'top'" :align="compact ? 'end' : 'start'" :side-offset="6">
      <div class="account-head">
        <span class="account-face"><Icon name="User" :size="18" :stroke-width="1.7" /></span>
        <span class="account-who">
          <span class="account-name">{{ account?.email || t('account.signedOut') }}</span>
          <span class="account-meta">{{ accountMeta }}</span>
        </span>
      </div>

      <div v-if="serverDown" class="account-reach" :class="{ busy: syncing }">
        <span class="account-reach-dot"></span>
        <span class="account-who">
          <span class="account-reach-title">
            {{ syncing ? t('account.reconnecting') : t('account.serverDown') }}
          </span>
          <span class="account-reach-when">
            {{ syncing ? t('account.retrying') : lastReach ? t('account.lastReach', { when: lastReach }) : '' }}
          </span>
        </span>
        <button
          type="button"
          class="account-reach-retry"
          :title="syncing ? t('account.trying') : t('account.retry')"
          @click="checkServer()"
        >
          <Icon name="RotateCw" :size="14" :stroke-width="1.9" />
        </button>
      </div>

      <PopoverClose as-child>
        <button class="account-row" @click="openSettings('account')">
          <Icon name="User" :size="14" :stroke-width="1.7" />
          <span class="cap-line">{{ t('settings.sec.account') }}</span>
        </button>
      </PopoverClose>
      <PopoverClose as-child>
        <button class="account-row" @click="openSettings()">
          <Icon name="SlidersHorizontal" :size="14" :stroke-width="1.8" />
          <span class="cap-line">{{ t('settings.title') }}</span>
        </button>
      </PopoverClose>

      <PopoverClose as-child>
        <button class="account-row" @click="workspaces.openSheet()">
          <Icon name="AppWindow" :size="14" :stroke-width="1.8" />
          <span class="cap-line">{{ t('workspaces.title') }}</span>
        </button>
      </PopoverClose>

      <div class="account-divider"></div>

      <PopoverClose v-if="signedIn" as-child>
        <button class="account-row bad" @click="openSignOut()">
          <Icon name="LogOut" :size="14" :stroke-width="1.8" />
          <span class="cap-line">{{ t('account.signOut') }}</span>
        </button>
      </PopoverClose>
      <PopoverClose v-else as-child>
        <button class="account-row good" @click="openSignIn()">
          <Icon name="User" :size="14" :stroke-width="1.7" />
          <span class="cap-line">{{ t('account.signIn') }}</span>
        </button>
      </PopoverClose>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
@reference "../../style.css";

.foot-account {
  @apply flex items-center gap-2 flex-1 min-w-0;
  height: 1.875rem;
  padding: 0 0.5rem;
  border: 0;
  border-radius: 0.4375rem;
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
  font-size: var(--text-13);
  cursor: default;
  text-align: left;
  --wails-draggable: no-drag;
}

.foot-account.compact {
  flex: none;
  justify-content: center;
  width: 2.375rem;
  height: 2.125rem;
  padding: 0;
  border-radius: 0.5rem;
}

.foot-account:hover,
.foot-account[data-state='open'] {
  background: var(--bg-hover);
  color: var(--text);
}

.foot-icon {
  flex: none;
}

.foot-email {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
