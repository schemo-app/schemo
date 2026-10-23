import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite()
const { comboOfEvent, isCombo } = await load('/src/lib/commands.ts')

function press(code, key, modifiers = {}) {
  return {
    code,
    key,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    getModifierState: (name) => modifiers.altGraph === true && name === 'AltGraph',
    ...modifiers,
  }
}

test('the platform modifier is Command on macOS and Control elsewhere', () => {
  assert.equal(isCombo('mod+K', press('KeyK', 'k', { metaKey: true }), true), true)
  assert.equal(isCombo('mod+K', press('KeyK', 'k', { ctrlKey: true }), false), true)
})

test('Control on macOS is the text field\'s own caret key, not a command', () => {
  assert.equal(isCombo('mod+K', press('KeyK', 'k', { ctrlKey: true }), true), false)
  assert.equal(isCombo('mod+E', press('KeyE', 'e', { ctrlKey: true }), true), false)
  assert.equal(comboOfEvent(press('KeyE', 'e', { ctrlKey: true, shiftKey: true }), true), '')
})

test('the Windows key is not the platform modifier on Windows and Linux', () => {
  assert.equal(isCombo('mod+K', press('KeyK', 'k', { metaKey: true }), false), false)
})

test('a character typed with AltGr is typing, not a shortcut', () => {
  const altGr = press('KeyW', 'w', { ctrlKey: true, altKey: true, altGraph: true })
  assert.equal(isCombo('mod+alt+W', altGr, false), false)
  assert.equal(isCombo('mod+alt+W', press('KeyW', 'w', { ctrlKey: true, altKey: true }), false), true)
})

test('a combination is exactly its modifiers', () => {
  assert.equal(isCombo('mod+S', press('KeyS', 's', { ctrlKey: true, shiftKey: true }), false), false)
  assert.equal(comboOfEvent(press('KeyS', 's', { ctrlKey: true, shiftKey: true }), false), 'mod+shift+S')
  assert.equal(comboOfEvent(press('Enter', 'Enter', { ctrlKey: true }), false), 'mod+enter')
  assert.equal(comboOfEvent(press('KeyS', 's'), false), '')
})
