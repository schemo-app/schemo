import { onScopeDispose } from 'vue'
import { useAsyncState } from '@vueuse/core'
import { CancelError, CancellablePromise } from '@wailsio/runtime'

export function useWorkspaceSnapshot<T>(initial: T) {
  let lifetime = new AbortController()
  const snapshot = useAsyncState(async (call: CancellablePromise<T>) => {
    const signal = lifetime.signal
    const answer = await call.cancelOn(signal)
    signal.throwIfAborted()
    return answer
  }, initial, { immediate: false, resetOnExecute: false, throwError: true, onError: () => {} })

  function take<R extends T>(call: CancellablePromise<R>): Promise<R>
  function take<R>(call: CancellablePromise<R>, select: (answer: R) => T): Promise<R>
  async function take<R>(call: CancellablePromise<R>, select?: (answer: R) => T): Promise<R> {
    const signal = lifetime.signal
    await snapshot.executeImmediate(call.then((answer) => select ? select(answer) : answer as unknown as T))
    const answer = await call
    signal.throwIfAborted()
    return answer
  }

  const replace = (value: T) => take(CancellablePromise.resolve(value))

  function reset(value: T = initial) {
    lifetime.abort(new CancelError())
    lifetime = new AbortController()
    snapshot.state.value = value
    snapshot.isLoading.value = false
  }

  onScopeDispose(() => reset())
  return { state: snapshot.state, loading: snapshot.isLoading, take, replace, reset, signal: () => lifetime.signal }
}
