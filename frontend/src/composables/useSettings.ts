import { computed, ref, type Ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { Settings as SettingsService } from '../../bindings/schemo/internal/workspace'
import type { Settings } from '../../bindings/schemo/internal/domain'
import { callOrToast } from '../lib/calls'
import { DEFAULTS } from '../lib/layout'
import { Events } from '@wailsio/runtime'

const LAYOUT_DEBOUNCE_MS = 400

/**
 * A change to the settings: only the fields that change. Nested objects merge field by field,
 * lists replace, and a shortcut set to null goes back to its default (null for all of them resets
 * every one). Go mints the MCP token and keeps the secrets; they are never sent back.
 */
export type SettingsPatch = {
  [K in keyof Settings]?: K extends 'shortcuts'
    ? Record<string, string | null> | null
    : Settings[K] extends unknown[]
      ? Settings[K]
      : Settings[K] extends object
        ? Partial<Settings[K]>
        : Settings[K]
}

// Layout drags write after a pause. Until then they are kept on top of what Go answers, so a
// reload in the middle of a drag does not snap the panel back.
const pendingLayout: SettingsPatch = {}

const resource = defineResource<Settings>('settings', () => SettingsService.Get(), (next) => ({
  ...next,
  ...(pendingLayout as Partial<Settings>),
}))
const settings = resource.data

const writeLayout = useDebounceFn(() => {
  const flush = { ...pendingLayout }
  for (const key of Object.keys(pendingLayout)) delete pendingLayout[key as keyof SettingsPatch]
  save(flush).catch(() => {
    // The window keeps the layout it shows; the next change writes again.
  })
}, LAYOUT_DEBOUNCE_MS)

/** Reads the settings once; every pane shares the answer and it follows changes from any window. */
async function loadSettings(): Promise<void> {
  await callOrToast(resource.load(), 'settings.readFailed')
}

/** Saves a change and takes Go's answer. Rejects when Go refuses it. */
async function save(patch: SettingsPatch): Promise<Settings> {
  const saved = await SettingsService.Save(patch)
  resource.set(saved)
  return saved
}

/** Like `save`, for a change that fails out loud: the window goes on showing what it had. */
async function saveOrToast(patch: SettingsPatch): Promise<Settings | undefined> {
  return await callOrToast(save(patch), 'settings.writeFailed')
}

/** Panel sizes and visibility: shown at once, written after the drag settles. */
function setLayout(patch: SettingsPatch): void {
  if (settings.value) Object.assign(settings.value, patch)
  Object.assign(pendingLayout, patch)
  void writeLayout()
}

/** Opens the file dialog and trusts the authority in the chosen file. */
async function addRootCertificate(title: string): Promise<Settings> {
  const saved = await SettingsService.AddRootCertificate(title)
  resource.set(saved)
  return saved
}

/** Reissues the MCP token and ends every agent that was speaking through the old one. */
async function rotateMCPToken(): Promise<Settings> {
  const saved = await SettingsService.RotateMCPToken()
  resource.set(saved)
  return saved
}

/** Composable for reading and changing the application settings. */
export function useSettings() {
  return {
    settings,
    loadSettings,
    save,
    saveOrToast,
    setLayout,
    addRootCertificate,
    rotateMCPToken,
    /** The proxy password itself, for the eye beside the field. */
    revealProxyPassword: () => SettingsService.RevealProxyPassword(),
  }
}

/** One setting as the window shows it: what was stored, or the default until it is read. */
export function useSetting<K extends keyof Settings>(key: K) {
  return computed(() => settings.value?.[key] ?? DEFAULTS[key])
}

interface Resource<T> {
  /** The value Go last answered with, or null before the first load. */
  data: Ref<T | null>
  /** Loads once; later calls wait for the same answer. A failed load can be asked for again. */
  load(): Promise<void>
  /** Asks Go again. A slower earlier answer never replaces a newer one. */
  reload(): Promise<void>
  /** Takes a value Go answered with elsewhere, such as the result of a save. */
  set(value: T): void
}

/**
 * A server-owned value every component of the window shares. It reloads whenever Go publishes
 * `changed` with its kind, so features need no events of their own and no payloads to merge.
 * `adopt` lets the owner keep local edits that are not saved yet on top of what Go answers.
 */
function defineResource<T>(
  kind: string,
  fetch: () => Promise<T>,
  adopt: (next: T) => T = (next) => next,
): Resource<T> {
  const data = ref<T | null>(null) as Ref<T | null>
  let first: Promise<void> | null = null
  let ticket = 0

  async function reload(): Promise<void> {
    const mine = ++ticket
    const next = await fetch()
    if (mine === ticket) data.value = adopt(next)
  }

  function load(): Promise<void> {
    first ??= reload().catch((error: unknown) => {
      first = null
      throw error
    })
    return first
  }

  function set(value: T): void {
    ticket++
    data.value = adopt(value)
  }

  Events.On('changed', (event) => {
    if (event.data?.kind === kind && data.value !== null) void reload().catch(() => {})
  })

  return { data, load, reload, set }
}
