import { useStorage } from '@vueuse/core'

/**
 * A choice remembered locally so a window paints before Go answers; `?param=` in the address wins.
 * Other windows' writes are not listened for: Go's event moves every window.
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
