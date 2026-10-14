import { nextTick, watch } from 'vue'
import { useEventListener } from '@vueuse/core'

/** Interface defining list navigation callbacks and DOM selectors. */
export interface ListKeys {
  /** Returns rendered element IDs in display order. */
  ids(): string[]
  /** Returns the currently selected element ID. */
  current(): string | null
  /** Moves selection to the specified ID. */
  move(id: string): void
  /** CSS selector targeting the currently selected DOM element. */
  selected: string
  /** Optional handler for horizontal arrow keys. Returning true consumes the event. */
  onSideKey?(key: 'ArrowLeft' | 'ArrowRight', id: string | null): boolean
}

/** Composable providing keyboard list navigation (Arrow keys) with automatic auto-scroll support. */
export function useListKeys(keys: ListKeys): void {
  function onKeydown(e: KeyboardEvent) {
    if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return
    if (isTyping(e.target) || isCovered()) return

    const key = e.key
    if (key === 'ArrowLeft' || key === 'ArrowRight') {
      if (keys.onSideKey?.(key, keys.current()) && e.cancelable) e.preventDefault()
      return
    }
    if (key !== 'ArrowDown' && key !== 'ArrowUp') return

    const list = keys.ids()
    if (list.length === 0) return
    const at = keys.current() ? list.indexOf(keys.current() as string) : -1
    const next = neighbour(list, at, key === 'ArrowDown' ? 1 : -1)
    if (next < 0) return

    keys.move(list[next])
    if (e.cancelable) e.preventDefault()
  }

  // Scroll active element into view on selection change
  watch(
      () => keys.current(),
      () => {
        void nextTick(() => {
          document.querySelector<HTMLElement>(keys.selected)?.scrollIntoView({ block: 'nearest' })
        })
      }
  )

  useEventListener(window, 'keydown', onKeydown)
}

/** Calculates the adjacent target index in the list, or -1 if boundary is reached. */
function neighbour(list: string[], at: number, step: number): number {
  if (at < 0) return step > 0 ? 0 : list.length - 1
  const next = at + step
  return next < 0 || next >= list.length ? -1 : next
}

/** Checks whether the event originated from an active text input element. */
function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el?.tagName) return false
  return el.isContentEditable || el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT'
}

/** Checks whether an overlay component (dialog, menu, popover) is currently open over the list. */
function isCovered(): boolean {
  return !!document.querySelector('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]')
}