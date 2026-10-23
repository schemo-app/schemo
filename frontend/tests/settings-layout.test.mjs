import assert from 'node:assert/strict'
import { after, mock, test } from 'node:test'
import { effectScope, nextTick } from 'vue'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
globalThis.layoutWrites = []
globalThis.changedHandlers = []
globalThis.settingsService = {
  Save: (patch) => new Promise((resolve) => globalThis.layoutWrites.push({ patch, resolve })),
}

const load = await vite({
  '/bindings/schemo/internal/workspace/index.ts': 'export const Settings = globalThis.settingsService',
  '@wailsio/runtime': 'export const Events = { On: (name, handler) => globalThis.changedHandlers.push({ name, handler }) }',
  '/src/lib/calls.ts': 'export const callOrToast = (call) => call',
})
const { useSettings } = await load('/src/composables/useSettings.ts')
const { useAppearance } = await load('/src/composables/useAppearance.ts')

test('a layout change made while a write is in flight survives the answer to it', async () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  const { settings, setLayout } = useSettings()
  settings.value = { sideWidth: 232 }

  setLayout({ sideWidth: 300 })
  mock.timers.tick(400)
  assert.deepEqual(globalThis.layoutWrites[0].patch, { sideWidth: 300 })

  setLayout({ sideWidth: 310 })
  globalThis.layoutWrites[0].resolve({ sideWidth: 300 })
  await Promise.resolve()

  assert.equal(settings.value.sideWidth, 310)
})

test('saved code font settings update the window size and family without resetting the UI scale', async (context) => {
  const properties = new Map()
  globalThis.document = { documentElement: {
    dataset: {},
    style: { setProperty: (name, value) => properties.set(name, value) },
  } }
  const { settings, save } = useSettings()
  settings.value = { appearance: { accent: 'blue', size: 'small', monoFont: 'system', monoSize: 13 } }
  globalThis.settingsService.Save = async patch => ({
    ...settings.value, appearance: { ...settings.value.appearance, ...patch.appearance },
  })
  const setAppearance = (appearance) => save({ appearance })
  const scope = effectScope()
  context.after(() => scope.stop())
  scope.run(useAppearance)
  assert.equal(properties.get('--mono-size'), '0.8125rem')
  await setAppearance({ monoSize: 16, monoFont: 'cascadia' })
  await nextTick()
  assert.equal(properties.get('--mono-size'), '1rem')
  assert.equal(document.documentElement.dataset.mono, 'cascadia')
  assert.equal(document.documentElement.dataset.size, 'small')
  await setAppearance({ monoSize: 12 })
  await nextTick()
  assert.equal(properties.get('--mono-size'), '0.75rem')
  assert.equal(document.documentElement.dataset.mono, 'cascadia')
})

test('a changed event for the settings reloads them in every window', async () => {
  const { settings } = useSettings()
  settings.value = { theme: 'light' }
  globalThis.settingsService.Get = async () => ({ theme: 'dark' })
  for (const { name, handler } of globalThis.changedHandlers) {
    if (name === 'changed') handler({ data: { kind: 'workspace' } })
  }
  await Promise.resolve()
  assert.equal(settings.value.theme, 'light')
  for (const { name, handler } of globalThis.changedHandlers) {
    if (name === 'changed') handler({ data: { kind: 'settings' } })
  }
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(settings.value.theme, 'dark')
})
