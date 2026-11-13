import { shallowRef, toValue, watchEffect, type MaybeRefOrGetter, type Ref } from 'vue'

/**
 * Preserves the last visible value while the dialog is closing.
 * Updates the value only while the dialog is open.
 */
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