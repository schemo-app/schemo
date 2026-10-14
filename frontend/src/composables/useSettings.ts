import { computed, ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import {
  BridgeService,
  McpService,
  RecordsService,
  SettingsService,
} from '../../bindings/schemo/internal/transport/wails'
import type {
  CaptureFilters,
  Settings,
} from '../../bindings/schemo/internal/domain'
import type {
  AppearancePatch,
  CertificatesPatch,
  EditorPatch,
  ExtensionPatch,
  LayoutPatch,
  MCPPatch,
  OnboardingPatch,
  ProxyPatch,
  RequestsPatch,
  SyncScopesPatch,
} from '../../bindings/schemo/internal/usecase/settings'
import { useWailsEvent } from './events/useWailsEvent'
import { callOrToast } from '../stores/calls'
import { DEFAULTS } from '../lib/layout'

const LAYOUT_DEBOUNCE_MS = 400

const settings = ref<Settings | null>(null)
let loading: Promise<void> | null = null

const pendingLayout: Record<string, unknown> = {}

const writeLayout = useDebounceFn(() => {
  const flush = { ...pendingLayout }
  for (const key of Object.keys(pendingLayout)) delete pendingLayout[key]
  SettingsService.SetLayout(flush)
    .then((next) => {
      settings.value = next
    })
    .catch(() => {})
}, LAYOUT_DEBOUNCE_MS)

// Synchronizes incoming backend settings changes while preserving active unpersisted layout edits
useWailsEvent('settings:changed', (ev) => {
  const incoming = ev.data as Settings | null
  if (!incoming) return
  settings.value = { ...incoming, ...pendingLayout }
})

/** Composable for reading and updating global application settings. */
export function useSettings() {
  return {
    settings,
    loadSettings,
    setTheme,
    setLanguage,
    setLayout,
    setRetention,
    setCaptureFilters,
    setExtension,
    setShortcut,
    resetShortcuts,
    setSyncScopes,
    setMCP,
    rotateMCPToken,
    setUpdateCheck,
    setUpdateChannel,
    setEditor,
    setReopenWorkspace,
    setRestoreTabs,
    setProxy,
    setCertificates,
    addRootCertificate,
    removeRootCertificate,
    setRequests,
    setAppearance,
    setOnboarding,
    revealProxyPassword,
  }
}

/** One setting as the window shows it: what was stored, or what Go answers with until it is read. */
export function useSetting<K extends keyof Settings>(key: K) {
  return computed(() => settings.value?.[key] ?? DEFAULTS[key])
}

/** Loads application settings snapshot from the backend. */
async function loadSettings(): Promise<void> {
  if (loading) return await loading
  loading = (async () => {
    const loaded = await callOrToast(SettingsService.Snapshot(), 'settings.readFailed')
    if (loaded) {
      settings.value = loaded
      return
    }
    // A read that failed is left to be asked for again: holding on to the promise would hold on to
    // the defaults the failed answer never replaced, and every pane reads once and never again.
    loading = null
  })()
  await loading
}

/** Takes the settings Go answered with: every change answers with them whole. */
async function keep(call: Promise<Settings>): Promise<Settings> {
  const saved = await call
  settings.value = saved
  return saved
}

/** Like `keep`, for a save that fails out loud: the window goes on showing what it had. */
async function keepOrToast(call: Promise<Settings>): Promise<void> {
  const saved = await callOrToast(call, 'settings.writeFailed')
  if (saved) settings.value = saved
}

const setTheme = (theme: Settings['theme']) => keepOrToast(SettingsService.SetTheme(theme))
const setLanguage = (language: Settings['language']) =>
  keepOrToast(SettingsService.SetLanguage(language))

/** Persists panel geometric layout changes with debouncing. */
function setLayout(patch: LayoutPatch): void {
  if (settings.value) Object.assign(settings.value, patch)
  Object.assign(pendingLayout, patch)
  void writeLayout()
}

/** Updates request capture filters and syncs them with the browser extension bridge. */
async function setCaptureFilters(filters: CaptureFilters): Promise<Settings> {
  const saved = await keep(SettingsService.SetCaptureFilters(filters))
  await BridgeService.ApplyCaptureFilters(saved.captureFilters)
  return saved
}

/** Writes down the combination one command wears; an empty one restores the default. */
const setShortcut = (id: string, combo: string) => keep(SettingsService.SetShortcut(id, combo))

/** Tells the welcome what has happened: the overview offered, the checklist put away, a step walked. */
const setOnboarding = (patch: OnboardingPatch) =>
  keepOrToast(SettingsService.SetOnboarding(patch))

/** Gives every command back the combination it comes with. */
const resetShortcuts = () => keep(SettingsService.ResetShortcuts())

/** Changes what of this device may leave it: the scopes the engine reads before its next push. */
const setSyncScopes = (patch: SyncScopesPatch) => keep(SettingsService.SetSyncScopes(patch))

/** Updates the extension switches and hands them to the extension beside the save. */
async function setExtension(patch: ExtensionPatch): Promise<Settings> {
  const saved = await keep(SettingsService.SetExtension(patch))
  await BridgeService.ApplyExtensionSettings(saved.extension)
  return saved
}

/** Changes the MCP server. The save carries it to the server in the same call: what the pane
    shows and what listens are one answer, not two. */
const setMCP = (patch: MCPPatch) => keep(SettingsService.SetMCP(patch))

/** Reissues the MCP token and ends every agent that was speaking through the old one. */
const rotateMCPToken = () => keep(McpService.Rotate())

/** Updates history retention settings and triggers immediate record pruning. */
async function setRetention(retention: Settings['historyRetention']): Promise<void> {
  await keep(SettingsService.SetRetention(retention))
  try {
    await RecordsService.Prune()
  } catch {}
}

/** Updates code editor configuration preferences. */
const setEditor = (patch: EditorPatch) => keep(SettingsService.SetEditor(patch))

/** Toggles reopening the last active workspace on app launch. */
const setReopenWorkspace = (reopen: boolean) => keep(SettingsService.SetReopenWorkspace(reopen))

/** Toggles restoring the open tabs — their order and pins included — on app launch. */
const setRestoreTabs = (restore: boolean) => keep(SettingsService.SetRestoreTabs(restore))

/** Toggles automatic application update checks. */
const setUpdateCheck = (auto: boolean) => keep(SettingsService.SetUpdateCheck(auto))

/** Sets update release channel (e.g., stable/beta). */
const setUpdateChannel = (channel: Settings['updateChannel']) =>
  keep(SettingsService.SetUpdateChannel(channel))

/** Updates the proxy the app's requests go through. A field left out is left as it is. */
const setProxy = (patch: ProxyPatch) => keep(SettingsService.SetProxy(patch))

/** Sets what the app checks in the certificates a server presents. A field left out is left. */
const setCertificates = (patch: CertificatesPatch) => keep(SettingsService.SetCertificates(patch))

/** Opens the file dialog and trusts the authorities the chosen file holds. */
const addRootCertificate = (title: string) => keep(SettingsService.AddRootCertificate(title))

/** Stops trusting one authority, named as the row draws it. */
const removeRootCertificate = (name: string) => keep(SettingsService.RemoveRootCertificate(name))

/** Updates how the app's own requests are sent. A field left out is left as it is. */
const setRequests = (patch: RequestsPatch) => keep(SettingsService.SetRequests(patch))

/** Updates how the window itself is drawn. Every window follows the broadcast that answers. */
const setAppearance = (patch: AppearancePatch) => keep(SettingsService.SetAppearance(patch))

/**
 * The proxy's password, for the eye beside the field. The only call that hands a value over:
 * every answer carries `hasPassword` and never the value itself.
 */
async function revealProxyPassword(): Promise<string> {
  return await SettingsService.RevealProxyPassword()
}