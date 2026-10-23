import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const strip = await load('/src/lib/strip.ts')

const request = (id, extra = {}) => ({ id, kind: 'request', draftId: `tab:${id}`, pinned: false, preview: false, ...extra })
const ids = change => change.tabs.map(tab => tab.id)

test('a plain open takes the preview slot in place, and the old preview takes its draft with it', () => {
  const s = { tabs: [request('a'), request('p', { ref: 'n1', draftId: 'n1', preview: true }), request('z')], active: 'a' }
  const change = strip.open(s, { kind: 'request', ref: 'n2', draftId: 'n2' })
  assert.equal(change.tabs[1].ref, 'n2')
  assert.equal(change.tabs[1].preview, true)
  assert.equal(change.active, change.tabs[1].id)
  assert.deepEqual(change.dropped, [])

  const fromTab = { tabs: [request('a'), request('p', { preview: true })], active: 'a' }
  assert.deepEqual(strip.open(fromTab, { kind: 'request', ref: 'n3', draftId: 'n3' }).dropped, ['tab:p'])
})

test('a ⌘-open keeps a tab of its own right after the shown one', () => {
  const s = { tabs: [request('a'), request('b')], active: 'a' }
  const change = strip.open(s, { kind: 'collections', ref: 'c1', newTab: true })
  assert.equal(change.tabs[1].ref, 'c1')
  assert.equal(change.tabs[1].preview, false)
  assert.equal(change.tabs[2].id, 'b')
})

test('what is already open is shown, and a ⌘-open keeps its preview for good', () => {
  const s = { tabs: [request('a'), request('n', { ref: 'n1', draftId: 'n1', preview: true })], active: 'a' }
  const change = strip.open(s, { kind: 'request', ref: 'n1', newTab: true })
  assert.deepEqual(ids(change), ['a', 'n'])
  assert.equal(change.active, 'n')
  assert.equal(change.tabs[1].preview, false)
})

test('a page is one however it is reached, never a preview, and a background open stays put', () => {
  const s = { tabs: [request('a')], active: 'a' }
  const opened = strip.open(s, { kind: 'history', background: true })
  assert.equal(opened.active, 'a')
  assert.equal(opened.tabs[1].kind, 'history')
  assert.equal(opened.tabs[1].preview, false)
  const again = strip.open(opened, { kind: 'history' })
  assert.equal(again.tabs.length, 2)
  assert.equal(again.active, opened.tabs[1].id)
})

test('a request opened with nothing named gets a tab draft of its own', () => {
  const change = strip.open({ tabs: [], active: '' }, { kind: 'request', record: 'r1' })
  assert.ok(change.tabs[0].draftId.startsWith('tab:'))
  assert.equal(change.tabs[0].record, 'r1')
})

test('a close falls to the tab that took its place, or the last one left', () => {
  const s = { tabs: [request('a'), request('b'), request('c')], active: 'b' }
  assert.equal(strip.closeOne(s, 'b').active, 'c')
  assert.deepEqual(strip.closeOne(s, 'b').dropped, ['tab:b'])
  assert.equal(strip.closeOne({ ...s, active: 'c' }, 'c').active, 'b')
  assert.equal(strip.closeOne(s, 'a').active, 'b')
  const last = strip.closeOne({ tabs: [request('a')], active: 'a' }, 'a')
  assert.deepEqual(last, { tabs: [], active: '', dropped: ['tab:a'] })
})

test('closing the others and the right keeps the pinned tabs and shows the named one', () => {
  const s = { tabs: [request('p', { pinned: true }), request('a'), request('b'), request('c')], active: 'c' }
  const others = strip.closeOthers(s, 'a')
  assert.deepEqual(ids(others), ['p', 'a'])
  assert.equal(others.active, 'a')
  const right = strip.closeRight(s, 'a')
  assert.deepEqual(ids(right), ['p', 'a'])
  assert.equal(right.active, 'a')
  assert.deepEqual(right.dropped, ['tab:b', 'tab:c'])
  assert.deepEqual(strip.closeRight(s, 'missing'), { tabs: s.tabs, active: 'c', dropped: [] })
})

test('a pin stands first and is never a preview', () => {
  const s = { tabs: [request('a'), request('b', { preview: true })], active: 'a' }
  const change = strip.pin(s, 'b', true)
  assert.deepEqual(ids(change), ['b', 'a'])
  assert.equal(change.tabs[0].preview, false)
})

test('a dragged tab takes the place and the pin of the tab it lands on', () => {
  const s = { tabs: [request('p', { pinned: true }), request('a'), request('b', { preview: true })], active: 'a' }
  const change = strip.move(s, 'b', 'p')
  assert.deepEqual(ids(change), ['b', 'p', 'a'])
  assert.equal(change.tabs[0].pinned, true)
  assert.equal(change.tabs[0].preview, false)
})

test('a duplicate lands right after its source, shown, unpinned and bound to no saved request', () => {
  const s = { tabs: [request('a', { ref: 'n1', draftId: 'n1', pinned: true }), request('b')], active: 'b' }
  const change = strip.duplicate(s, 'a', 'tab:copy')
  assert.equal(change.tabs[1].draftId, 'tab:copy')
  assert.equal(change.tabs[1].ref, undefined)
  assert.equal(change.tabs[1].pinned, false)
  assert.equal(change.active, change.tabs[1].id)
  const page = { tabs: [{ id: 'h', kind: 'history', pinned: false, preview: false }], active: 'h' }
  assert.equal(strip.duplicate(page, 'h', 'tab:x').tabs.length, 1)
})

test('a tab saved as a node edits the node from then on and its draft goes', () => {
  const s = { tabs: [request('a', { preview: true })], active: 'a' }
  const change = strip.adopt(s, 'a', 'node-1')
  assert.equal(change.tabs[0].ref, 'node-1')
  assert.equal(change.tabs[0].draftId, 'node-1')
  assert.equal(change.tabs[0].preview, false)
  assert.deepEqual(change.dropped, ['tab:a'])
})

test('tabs of collections and requests gone from the tree close', () => {
  const tree = [{ id: 'c1', items: [{ id: 'n1' }], children: [{ id: 'c2', items: [], children: null }] }]
  const s = {
    tabs: [
      request('kept', { ref: 'n1', draftId: 'n1' }),
      { id: 'folder', kind: 'collections', ref: 'c2', pinned: false, preview: false },
      request('gone', { ref: 'n9', draftId: 'n9' }),
      request('free'),
    ],
    active: 'gone',
  }
  const change = strip.reconcile(s, tree)
  assert.deepEqual(ids(change), ['kept', 'folder', 'free'])
  assert.equal(change.active, 'free')
  assert.deepEqual(change.dropped, [])
  assert.equal(strip.reconcile(change, tree).tabs, change.tabs)
})

test('a stored strip drops kinds this build cannot draw and opens on a blank tab when empty', () => {
  const change = strip.restored([request('a'), { id: 'old', kind: 'gone', draftId: 'tab:old' }], 'old')
  assert.deepEqual(ids(change), ['a'])
  assert.equal(change.active, 'a')
  assert.deepEqual(change.dropped, ['tab:old'])
  const empty = strip.restored([], '')
  assert.equal(empty.tabs.length, 1)
  assert.equal(empty.active, empty.tabs[0].id)
})
