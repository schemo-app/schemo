import assert from 'node:assert/strict'
import { test } from 'node:test'
import { effectScope, nextTick, ref } from 'vue'
import { vite } from './vite.mjs'

globalThis.window = new EventTarget()
const load = await vite()
const { useTabRow } = await load('/src/composables/useTabRow.ts')

function rowOf(count, pins = 0) {
  const tabs = ref(Array.from({ length: count }, (_, i) => ({ id: String(i), compact: i < pins })))
  const active = ref(String(count - 1))
  const scope = effectScope()
  const row = scope.run(() => useTabRow({ tabs: () => tabs.value, activeId: () => active.value }))
  return { tabs, active, row, stop: () => scope.stop() }
}

test('a few tabs stop growing at their maximum width', () => {
  const { row, stop } = rowOf(3)
  assert.equal(row.tabW.value, 220)
  assert.equal(row.hiddenCount.value, 0)
  stop()
})

test('overflow preserves readable titles and keeps the active tab visible', () => {
  const { row, stop } = rowOf(100)
  assert.ok(row.tabW.value >= 120)
  assert.ok(row.hiddenCount.value > 0)
  assert.ok(row.visibleIds.value.has('99'))
  assert.equal(row.visibleIds.value.size + row.hiddenCount.value, 100)
  stop()
})

test('switching to a hidden tab retains recently used tabs without exceeding capacity', async () => {
  const { active, row, stop } = rowOf(20)
  const capacity = row.visibleIds.value.size
  active.value = '18'
  await nextTick()
  assert.ok(row.visibleIds.value.has('18'))
  assert.ok(row.visibleIds.value.has('19'))
  assert.equal(row.visibleIds.value.size, capacity)
  stop()
})

test('compact collection tabs remain visible and reserve room before overflow is calculated', () => {
  const { row, stop } = rowOf(20, 3)
  assert.ok(['0', '1', '2', '19'].every((id) => row.visibleIds.value.has(id)))
  assert.equal(row.visibleIds.value.size + row.hiddenCount.value, 20)
  assert.ok(row.tabW.value >= 120)
  stop()
})

test('no tabs take no room', () => {
  const { row, stop } = rowOf(0)
  assert.equal(row.tabW.value, 0)
  assert.equal(row.hiddenCount.value, 0)
  stop()
})

test('closing a tab holds its width until the pointer leaves the strip', () => {
  const { tabs, row, stop } = rowOf(5)
  const width = row.tabW.value
  row.hold()
  tabs.value.pop()
  assert.equal(row.tabW.value, width)
  row.release()
  assert.ok(row.tabW.value > width)
  stop()
})
