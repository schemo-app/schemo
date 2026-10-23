import { useRecordsStore } from '../stores/records'

// How close to the end of a list the reading of older records begins, in pixels: early enough that
// the next page is there by the time the scroll reaches it.
const NEAR_END = 300

// A list of records that reads further back as it is scrolled. The store knows when a page is
// already on its way or there is none left; this only knows where the list has been scrolled to.
export function useLoadOlder() {
  const records = useRecordsStore()

  /** Reads the next page when the list stands within reach of its end. */
  function loadIfNearEnd(list: HTMLElement | null | undefined) {
    if (!list || records.records.length === 0) return
    if (list.scrollHeight - list.scrollTop - list.clientHeight < NEAR_END) void records.loadOlder()
  }

  return { loadIfNearEnd }
}
