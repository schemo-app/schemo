import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import type { CancellablePromise } from '@wailsio/runtime/cancellable'
import { callOrToast, runOrToast } from '../lib/calls'
import { Events } from '@wailsio/runtime'
import { Cookies } from '../../bindings/schemo/internal/request'
import { CookieOrigin } from '../../bindings/schemo/internal/domain'
import type { JarCookie, JarDomain } from '../../bindings/schemo/internal/domain'
import { useWorkspaceSnapshot } from '../composables/useWorkspaceSnapshot'

export const useCookiesStore = defineStore('cookies', () => {
  const sheetOpen = ref(false)
  const snapshot = useWorkspaceSnapshot<JarDomain[]>([])
  const domains = snapshot.state
  // A domain name, not an id: it may name a domain with no cookies yet, so they can be added.
  const domain = ref('')
  // The edit form's own copy: nothing reaches the jar until it is saved.
  const editing = ref<JarCookie | null>(null)

  const current = computed(() => domains.value.find((level) => level.domain === domain.value) ?? null)
  const rows = computed(() => current.value?.cookies ?? [])
  const blocked = computed(() => current.value?.blocked ?? false)

  // The manager opens on the domain the jar keeps an address's cookies under. Go names it: that is
  // the jar's key, not the address's host with its port and user.
  async function openManager(address = '') {
    sheetOpen.value = true
    const host = address ? await callOrToast(Cookies.DomainOf(address), 'cookies.readFailed') : ''
    await load(host ?? '')
  }

  function closeSheet() {
    sheetOpen.value = false
    editing.value = null
  }

  async function reset() {
    snapshot.reset()
    domain.value = ''
    editing.value = null
    if (sheetOpen.value) await load()
  }

  // The host asked for is kept even when the read fails or finds no cookies for it.
  async function load(host = '') {
    if (host) domain.value = host
    const signal = snapshot.signal()
    const jar = await callOrToast(snapshot.take(Cookies.Jar().then(jar => jar ?? [])), 'cookies.readFailed')
    if (!jar || signal.aborted || domains.value !== jar) return
    if (!domain.value) domain.value = jar[0]?.domain ?? ''
  }

  // A form left open on one domain must not save into another.
  function select(next: string) {
    if (next === domain.value) return
    domain.value = next
    editing.value = null
  }

  // A write that landed is followed by a fresh read of the jar: Go's answer is the only state.
  async function write(call: CancellablePromise<unknown>): Promise<boolean> {
    const signal = snapshot.signal()
    if (!(await runOrToast(call.cancelOn(signal), 'cookies.writeFailed')) || signal.aborted) return false
    await load()
    return !signal.aborted
  }

  async function setBlocked(value: boolean) {
    if (domain.value) await write(Cookies.SetBlocked(domain.value, value))
  }

  function edit(cookie: JarCookie) {
    editing.value = { ...cookie }
  }

  // A cookie typed by hand is host-only: no leading dot in its domain. It starts on the domain on
  // screen, and an empty jar has none: the form asks for it.
  function beginNew() {
    editing.value = {
      id: '',
      domain: domain.value,
      name: '',
      value: '',
      path: '/',
      secure: true,
      sameSite: 'Lax',
      origin: CookieOrigin.CookieByHand,
      hostOnly: true,
      writtenAt: Date.now(),
    }
  }

  function cancelEdit() {
    editing.value = null
  }

  async function save(cookie: JarCookie) {
    const form = editing.value
    if (await write(Cookies.Save(cookie)) && editing.value === form) editing.value = null
  }

  async function remove(id: string) {
    await write(Cookies.Delete(id))
  }

  // The domain on screen goes with its cookies and its flag; the rail falls to the first one left.
  async function removeDomain() {
    const host = domain.value
    const signal = snapshot.signal()
    if (!host) return
    if (!(await runOrToast(Cookies.DeleteDomain(host).cancelOn(signal), 'cookies.writeFailed')) || signal.aborted) return
    if (domain.value === host) {
      domain.value = ''
      editing.value = null
    }
    await load()
  }

  // A response, another window or sync changed the jar: an open manager reads it again.
  let pending: ReturnType<typeof setTimeout> | null = null
  const offChanged = Events.On('changed', (event) => {
    const kind = event.data?.kind
    if ((kind !== 'cookie' && kind !== 'jar_domain') || !sheetOpen.value || pending) return
    pending = setTimeout(() => {
      pending = null
      if (sheetOpen.value) void load()
    }, 50)
  })

  onScopeDispose(() => {
    offChanged?.()
    domain.value = ''
    editing.value = null
  })

  return {
    sheetOpen,
    domains,
    domain,
    editing,
    current,
    rows,
    blocked,
    openManager,
    closeSheet,
    reset,
    load,
    select,
    setBlocked,
    edit,
    beginNew,
    cancelEdit,
    save,
    remove,
    removeDomain,
  }
})
