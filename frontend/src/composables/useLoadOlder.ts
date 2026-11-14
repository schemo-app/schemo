import { useRecordsStore } from '../stores/records'

// A list of records that reads further back as it is scrolled. The store knows when a page is
// already on its way or there is none left; this only knows where the list has been scrolled to.
export function useLoadOlder() {
  const records = useRecordsStore()

  /** Reads the next page when the list stands within reach of its end. */
  function loadIfNearEnd(list: HTMLElement | null | undefined) {
    if (!list || records.records.length === 0) return
    // Reads on 300px before the end, so the next page is there by the time the scroll reaches it.
    if (list.scrollHeight - list.scrollTop - list.clientHeight < 300) void records.loadOlder()
  }

  return { loadIfNearEnd }
}
