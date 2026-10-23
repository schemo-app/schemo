<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Browser } from '@wailsio/runtime'
import AccountPlan from './AccountPlan.vue'
import AccountSync from './AccountSync.vue'
import DeleteAccountSheet from '../cloud/DeleteAccountSheet.vue'
import SettingsRow from './SettingsRow.vue'
import SettingsSection from './SettingsSection.vue'
import Segment from '../ui/Segment.vue'
import Button from '../ui/Button.vue'
import Icon from '../ui/Icon.vue'
import Spinner from '../ui/Spinner.vue'
import { useAccount } from '../../composables/useAccount'
import { usePlan } from '../../composables/usePlan'
import { formatDate, t } from '../../i18n'

// The account: who is here and the way to the panel, then, signed in, where syncing stands, the
// tariff and how to leave, each in a part of its own. The devices themselves, the list and ending a
// session, live in the panel, so the pane names them and links there.

const {
  state,
  sessions,
  failure,
  busy,
  signOutOpen,
  openSignOut,
  deleteOpen,
  openDelete,
  closeDelete,
  openPanel,
  loadAccount,
  loadSessions,
  checkServer,
  beginSignIn,
  cancelSignIn,
  checkAddress,
} = useAccount()
const { loadPlan } = usePlan()

const account = computed(() => state.value?.account ?? null)
const signedIn = computed(() => state.value?.signedIn === true)

// The mark on the card: the initials of a name, or the first letter of the address when nobody
// gave one.
const initials = computed(() => {
  const name = (account.value?.name ?? '').trim()
  if (name) {
    return name
      .split(/\s+/)
      .slice(0, 2)
      .map((word) => word[0]?.toUpperCase() ?? '')
      .join('')
  }
  return (account.value?.email ?? '').slice(0, 1).toUpperCase()
})

// Where the sign-in goes: the app's own cloud unless somebody names a server of their own. The
// choice lives in this window until the sign-in itself, which is the click that keeps it; the
// status, not the pane, says which server the account is on.
const custom = ref(false)
const chosen = ref('')

// What the sign-in will go to — nothing typed means the app's own server, which the sign-in reads
// as "nobody chose one" — and what the line beside the button names.
const target = computed(() => (custom.value && chosen.value ? chosen.value : ''))
// The row shows the account's own state: whatever server the account was last kept on becomes the
// chosen one, and a first visit has nothing to draw but the cloud.
watch(
  () => account.value?.server ?? '',
  (stored) => {
    custom.value = stored !== ''
    chosen.value = stored
  },
  { immediate: true }
)

const signingIn = computed(() => state.value?.waiting === true)
const code = computed(() => state.value?.challenge?.userCode ?? '')

const serverOptions = computed(() => [
  { value: 'cloud', label: t('account.out.cloud') },
  { value: 'custom', label: t('account.out.custom') },
])

function pickServer(value: string): void {
  custom.value = value === 'custom'
}

// The check is the discovery document's answer, worded by the catalogue when it is not there; the
// address itself is kept by the sign-in, not by the check.
const probing = ref(false)

async function probe(): Promise<void> {
  if (probing.value) return
  probing.value = true
  try {
    await checkAddress(chosen.value)
  } finally {
    probing.value = false
  }
}

function startSignIn(): void {
  void beginSignIn(target.value)
}

function openAgain(): void {
  const url = state.value?.challenge?.url
  if (url) void Browser.OpenURL(url)
}

function giveUp(): void {
  void cancelSignIn()
}

onMounted(async () => {
  await checkServer()
  if (signedIn.value) void loadSessions()
  void loadPlan()
})

// The other devices, in one line: their names and when they signed in. The list itself, and ending
// a session, are the panel's; here is what a person checks before opening it. A failed read says
// nothing here, since the sync line is where this pane reports the server being away.
const devicesLine = computed(() => {
  const others = sessions.value.filter((one) => !one.isCurrent)
  if (!others.length) return t('account.devicesEmpty')
  return others
    .map((one) => [one.device.name || one.device.platform, when(one.createdAt)].filter(Boolean).join(' · '))
    .join(', ')
})

/** When a device signed in, in the words the language writes dates with. */
function when(value: string): string {
  const at = new Date(value)
  if (Number.isNaN(at.getTime())) return ''
  return formatDate(at, { day: 'numeric', month: 'long' })
}

// The devices are read when an account arrives (signed in from this pane, from another window, or
// by a code confirmed in a browser), because the list is the server's and the pane only knows to
// ask. A sign-out clears what is drawn; the sheet closing is when the account is read again.
watch(signedIn, (here) => {
  if (here) void loadSessions()
})

watch(signOutOpen, (leaving) => {
  if (leaving) return
  void loadAccount()
  void loadSessions()
})
</script>

<template>
  <div class="pane">
    <template v-if="!signedIn">
      <!-- The card the signed-out pane opens with: what the app is without an account, the one button
      that changes it, and, while a sign-in is in flight, the wait in its place, code and all, because the
      pane is where the sign-in is followed. -->
      <div class="out-card">
        <div v-if="!signingIn" class="out-intro">
          <span class="out-title">{{ t('account.out.title') }}</span>
          <span class="out-note">{{ t('account.out.note') }}</span>
        </div>
        <div v-else class="out-wait">
          <Spinner class="text-accent" />
          <span class="out-wait-text">
            <span class="out-wait-title">{{ t('account.out.waiting') }}</span>
            <span class="out-note">{{ t('account.out.waitingNote') }}</span>
          </span>
          <span v-if="code" class="out-code">
            <span class="out-code-value">{{ code }}</span>
          </span>
        </div>
        <div class="out-go">
          <template v-if="!signingIn">
            <Button variant="primary" size="field" :disabled="busy" @click="startSignIn()">
              {{ t('account.out.signIn') }}
            </Button>
          </template>
          <template v-else>
            <Button variant="outline" size="field" @click="openAgain()">
              {{ t('account.out.openAgain') }}
            </Button>
            <button type="button" class="out-link" @click="giveUp()">
              {{ t('account.out.cancel') }}
            </button>
          </template>
        </div>
      </div>

      <!-- Where the sign-in goes. A server of one's own is another deployment, and the check is
           the discovery document's answer rather than a promise the pane cannot keep. -->
      <SettingsSection :label="t('account.out.server')">
        <SettingsRow
          :title="t('account.out.serverWhere')"
          :note="custom ? t('account.out.customNote') : t('account.out.cloudNote')"
        >
          <Segment
            :value="custom ? 'custom' : 'cloud'"
            :options="serverOptions"
            :grow="false"
            :disabled="signingIn"
            @pick="pickServer"
          />
        </SettingsRow>
        <div v-if="custom" class="card-block addr">
          <input
            v-model="chosen"
            class="addr-field"
            spellcheck="false"
            :placeholder="t('account.out.addressHint')"
          />
          <Button variant="outline" size="field" :disabled="probing || signingIn" @click="probe()">
            {{ t('account.out.check') }}
          </Button>
        </div>
      </SettingsSection>
    </template>

    <template v-else>
      <!-- Who is here, what the account is called, and the way to the panel: billing, seats and
           devices are its business, and the app opens it rather than redrawing it. -->
      <div class="who-card">
        <span class="mark">{{ initials }}</span>
        <span class="who">
          <span class="who-name">{{ account?.name || account?.email }}</span>
          <span v-if="account?.name" class="who-line">{{ account?.email }}</span>
        </span>
        <Button variant="outline" size="field" @click="openPanel()">
          {{ t('account.panel') }}
          <Icon name="ArrowUpRight" :size="10" :stroke-width="2.2" />
        </Button>
      </div>

      <AccountSync />

      <AccountPlan />

      <SettingsSection :label="t('account.entry')">
        <SettingsRow
          v-if="state?.sessionOnly"
          :title="t('account.sessionOnly')"
          :note="t('account.sessionOnlyNote')"
        />
        <SettingsRow :title="t('account.otherDevices')" :note="devicesLine">
          <Button variant="outline" size="field" @click="openPanel()">
            {{ t('account.sessions') }}
            <Icon name="ArrowUpRight" :size="10" :stroke-width="2.2" />
          </Button>
        </SettingsRow>
        <SettingsRow :title="t('account.signOut')" :note="t('account.signOutNote')">
          <Button variant="outline" size="field" @click="openSignOut()">
            {{ t('account.signOutAction') }}
          </Button>
        </SettingsRow>
        <!-- The account's own end, which is not this device's: the row is last and the note says
             what it takes, because everything above it is the reversible way out. -->
        <SettingsRow :title="t('account.delete')" :note="t('account.deleteNote')" alarm>
          <Button variant="danger" size="field" @click="openDelete()">
            {{ t('account.deleteAction') }}
          </Button>
        </SettingsRow>
      </SettingsSection>
    </template>

    <DeleteAccountSheet v-if="deleteOpen" @close="closeDelete()" />

    <p v-if="failure" class="bad">{{ failure }}</p>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.pane {
  @apply flex flex-col;
  gap: 1.125rem;
}

.bad {
  @apply m-0 text-13 text-red;
}

.who-card {
  @apply flex items-center gap-3.5 border border-border rounded-[0.625rem];
  padding: 1rem;
}

.mark {
  @apply flex-none inline-flex items-center justify-center rounded-full text-15 font-semibold;
  width: 2.75rem;
  height: 2.75rem;
  background: var(--accent-soft);
  color: var(--accent);
}

.who {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.1875rem;
}

.who-name {
  @apply text-15 font-semibold truncate;
}

.who-line {
  @apply text-12 text-text-tertiary truncate;
}

.out-card {
  @apply flex flex-col items-start gap-4 border border-border rounded-[0.625rem];
  padding: 1.375rem;
}

.out-intro {
  @apply flex flex-col;
  gap: 0.3rem;
  max-width: 32.5rem;
}

.out-title {
  @apply text-16 font-semibold;
}

.out-note {
  @apply text-13 text-text-secondary;
  line-height: 1.55;
  text-wrap: pretty;
}

.out-wait {
  @apply flex items-center w-full;
  gap: 1.125rem;
}

.out-wait-text {
  @apply flex-1 min-w-0 flex flex-col;
  gap: 0.25rem;
}

.out-wait-title {
  @apply text-15 font-semibold;
}

.out-code {
  @apply flex-none flex flex-col items-end;
  gap: 0.125rem;
}

.out-code-label {
  @apply text-11 text-text-tertiary;
}

.out-code-value {
  @apply text-20 font-bold tracking-[0.06em];
  font-variant-numeric: tabular-nums;
}

.out-go {
  @apply flex items-center gap-3;
}

.out-link {
  @apply border-0 bg-transparent text-13 text-text-secondary cursor-pointer;
  font-family: inherit;
  padding: 0.25rem 0;
}

.out-link:hover {
  @apply text-text;
}

.addr {
  @apply flex items-center gap-2;
  padding: 0.625rem 1rem 0.75rem;
}

.addr-field {
  @apply flex-1 min-w-0 h-7.5 px-2.5 border border-border-strong rounded-[0.3rem] bg-bg-panel
         text-12 text-text;
  font-family: ui-monospace, 'SF Mono', 'Cascadia Code', Menlo, monospace;
}

</style>
