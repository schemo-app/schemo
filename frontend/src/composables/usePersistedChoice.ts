import { useStorage } from '@vueuse/core'

/**
 * A choice this machine remembers, so that a window can paint before Go has answered. An address
 * that carries it as `?param=` — the way Go opens a window — wins over what was remembered. Another
 * window's write is not listened for: Go's event is what moves every window, and a copy that moved
 * first would make that event look like nothing changed.
 */
export function usePersistedChoice<T extends string>(
  key: string,
  param: string,
  allowed: readonly T[],
  fallback: T
) {
  const known = (raw: string | null): raw is T => allowed.includes(raw as T)

  const choice = useStorage<T>(key, fallback, undefined, {
    listenToStorageChanges: false,
    serializer: { read: (raw) => (known(raw) ? raw : fallback), write: (value) => value },
  })

  const given = new URLSearchParams(location.search).get(param)
  if (known(given)) choice.value = given
  return choice
}
