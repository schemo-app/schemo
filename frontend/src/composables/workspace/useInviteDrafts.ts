import { ref } from 'vue'

/** A row somebody is filling in: an address, and the role it will be given. */
export interface InviteDraft {
  key: number
  email: string
  roleId: string
}

/**
 * The rows of invitations being written. They are kept here rather than sent one at a time: inviting
 * three people is three rows written one after another, and the space they are for may not exist yet.
 */
export function useInviteDrafts() {
  const drafts = ref<InviteDraft[]>([])
  let next = 1

  function addDraft() {
    drafts.value = [...drafts.value, { key: next++, email: '', roleId: '' }]
  }

  function dropDraft(key: number) {
    drafts.value = drafts.value.filter((one) => one.key !== key)
  }

  function clearDrafts() {
    drafts.value = []
  }

  return { drafts, addDraft, dropDraft, clearDrafts }
}
