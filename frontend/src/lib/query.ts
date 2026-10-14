/** Whether any of the fields holds the query, in any case; an empty query is held by everything. */
export function matchesQuery(query: string, ...fields: (string | null | undefined)[]): boolean {
  const needle = query.trim().toLowerCase()
  return !needle || fields.some((field) => field?.toLowerCase().includes(needle))
}
