import type { Problem } from '../../bindings/schemo/internal/domain'

/** `/items/0/name` as `items[0].name`, which is how a person writes a member. */
function fieldLabel(field: string): string {
  return field
    .split('/')
    .filter(Boolean)
    .map((part) => part.replace(/~1/g, '/').replace(/~0/g, '~'))
    .reduce((out, part) => (/^\d+$/.test(part) ? `${out}[${part}]` : out ? `${out}.${part}` : part), '')
}

/** What a problem points at in the request, as it reads: a member of the body, or a name. */
export const pointedAt = (problem: Problem): string =>
  problem.where === 'body' ? fieldLabel(problem.field) : problem.field

/** Whether the problem says where in the request the fault is. */
export const isAboutRequest = (problem: Problem): boolean => !!problem.where && !!problem.field
