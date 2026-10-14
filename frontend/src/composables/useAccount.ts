import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { AccountService } from '../../bindings/schemo/internal/transport/wails'
import type { Status } from '../../bindings/schemo/internal/usecase/account'
import type { Session } from '../../bindings/schemo/internal/domain'
import { describeFailure, formatCheckedAt, failureText } from '../i18n'
import { usePlan } from './usePlan'
import { useWailsEvent } from './events/useWailsEvent'

const { refreshPlan } = usePlan()

// Global reactive account state shared across application windows
const state = ref<Status | null>(null)
const sessions = ref<Session[]>([])
const failure = ref('')
const busy = ref(false)

// UI dialog visibility states
const signInOpen = ref(false)
const signOutOpen = ref(false)
const deleteOpen = ref(false)

// Server connectivity indicators
const reachability = computed(() => state.value?.reachability ?? null)
const reachChecked = computed(() => reachability.value?.checked === true)
const reachable = computed(() => reachability.value?.reachable === true)

/** True when the account is signed in, a connectivity check completed, and the server failed to respond. */
const serverDown = computed(() => state.value?.signedIn === true && reachChecked.value && !reachable.value)

/** Active network health check flag. */
const syncing = ref(false)

/** Formatted timestamp of the last successful reachability check. */
const lastReach = computed(() => {
  const at = reachability.value?.at
  if (!at) return ''
  const when = new Date(at)
  if (Number.isNaN(when.getTime()) || when.getFullYear() < 2000) return ''
  return formatCheckedAt(when.getTime())
})

// Listen for cross-window account state mutations emitted from Go backend
useWailsEvent('account:changed', (ev) => {
  const arriving = ev.data as Status | null
  if (!arriving) return
  state.value = arriving
  failure.value = arriving.failure ? (failureText(arriving.failure) ?? '') : ''
})

/** Composable providing account state management, session controls, and authentication actions. */
export function useAccount() {
  return {
    state,
    reachability,
    lastReach,
    serverDown,
    syncing,
    sessions,
    failure,
    busy,
    signInOpen,
    openSignIn,
    closeSignIn,
    signOutOpen,
    openSignOut,
    closeSignOut,
    deleteOpen,
    openDelete,
    closeDelete,
    loadAccount,
    checkServer,
    beginSignIn,
    cancelSignIn,
    setServer,
    checkAddress,
    signOut,
    deleteAccount,
    loadSessions,
    openPanel,
  }
}

/** Fetches current account state from the backend. */
async function loadAccount(): Promise<void> {
  try {
    state.value = await AccountService.Status()
  } catch {
    // Unhandled error leaves state as signed out
  }
}

// A call whose refusal is the account pane's to word: the sentence lands in `failure`, and the
// answer is undefined.
async function attempt<T>(call: Promise<T>): Promise<T | undefined> {
  try {
    return await call
  } catch (error) {
    failure.value = describeFailure(error)
    return undefined
  }
}

// Most calls answer with the whole account status; this takes it and says whether it came.
async function take(call: Promise<Status>): Promise<boolean> {
  const next = await attempt(call)
  if (next) state.value = next
  return next !== undefined
}

/** Checks the server's reachability, and refreshes the plan beside it. */
async function checkServer(): Promise<void> {
  if (syncing.value) return
  void refreshPlan()
  syncing.value = true
  await take(AccountService.Check())
  syncing.value = false
}

function openSignIn(): void {
  signInOpen.value = true
  void loadAccount()
}

function closeSignIn(): void {
  signInOpen.value = false
}

function openSignOut(): void {
  signOutOpen.value = true
}

function closeSignOut(): void {
  signOutOpen.value = false
}

function openDelete(): void {
  deleteOpen.value = true
}

function closeDelete(): void {
  deleteOpen.value = false
}

/** Starts the sign-in in the browser against the given server. */
async function beginSignIn(server: string): Promise<void> {
  busy.value = true
  failure.value = ''
  await take(AccountService.Begin(server))
  busy.value = false
}

async function cancelSignIn(): Promise<void> {
  signInOpen.value = false
  await take(AccountService.Cancel())
}

/** Points the account at another server; answers whether it took. */
async function setServer(server: string): Promise<boolean> {
  if (!(await take(AccountService.SetServer(server)))) return false
  failure.value = ''
  return true
}

/**
 * Asks whether an address is a server this app can sign in to, before anybody signs in there. The
 * answer that is not "yes" is a refusal in `failure`, worded by the catalogue; nothing is stored —
 * keeping an address is the sign-in's own step.
 */
async function checkAddress(server: string): Promise<void> {
  failure.value = ''
  await attempt(AccountService.CheckServer(server))
}

/**
 * Opens the account's panel in the browser. Billing, seats and sessions are the panel's, and the app
 * opens it rather than redrawing it; Go answers with the address the sign-in itself uses, because
 * the stored row keeps "" for the app's own server.
 */
function openPanel(): void {
  const address = state.value?.address
  if (address) void Browser.OpenURL(address)
}

/** Signs out on this machine; `wipe` also deletes what the device holds of the account. */
async function signOut(wipe = false): Promise<void> {
  signOutOpen.value = false
  if (await take(AccountService.SignOut(wipe))) sessions.value = []
}

/**
 * Deletes the account in the cloud, and this device's copy of it with it. Go asks the server first —
 * the account is the server's — and the answer that is not "gone" is a refusal in `failure`, with
 * everything on this machine left where it was.
 */
async function deleteAccount(): Promise<void> {
  deleteOpen.value = false
  if (await take(AccountService.Delete())) sessions.value = []
}

/** The account's sessions; a failed read leaves the list from the last one. */
async function loadSessions(): Promise<void> {
  try {
    sessions.value = (await AccountService.Sessions()) ?? []
  } catch {
    // Nothing to do: the list that is drawn is the one from the last read.
  }
}

