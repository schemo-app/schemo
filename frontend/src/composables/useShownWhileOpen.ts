import { shallowRef, toValue, watchEffect, type MaybeRefOrGetter, type Ref } from 'vue'

/** Keeps the last value while the dialog animates closed. */
export function useShownWhileOpen<T>(
  open: MaybeRefOrGetter<boolean>,
  read: () => T
): Ref<T> {
  const shown = shallowRef(read()) as Ref<T>

  watchEffect(() => {
    if (toValue(open)) {
      shown.value = read()
    }
  })

  return shown
}