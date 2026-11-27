/** One numbered line of one side of a diff. */
export interface DiffLine {
  n: number
  text: string
}

/** A row of a two-sided diff: what stood and what comes, either of which may be missing. */
export interface DiffRow {
  left: DiffLine | null
  right: DiffLine | null
  changed: boolean
}

/** A row of a folded diff: a pair of lines, or how many unchanged ones are left out. */
export type FoldedRow = DiffRow | { skipped: number }

const linesOf = (text: string) => (text ? text.split('\n') : [])

/** Aligns two texts line by line. Lines that go and lines that come in one place share rows. */
export function sideBySide(before: string, after: string): DiffRow[] {
  const a = linesOf(before)
  const b = linesOf(after)
  // The longest run the two share from each pair of positions on.
  const common = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1))
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      common[i][j] = a[i] === b[j] ? common[i + 1][j + 1] + 1 : Math.max(common[i + 1][j], common[i][j + 1])
    }
  }

  const rows: DiffRow[] = []
  let gone: DiffLine[] = []
  let come: DiffLine[] = []
  const flush = () => {
    for (let at = 0; at < Math.max(gone.length, come.length); at++) {
      rows.push({ left: gone[at] ?? null, right: come[at] ?? null, changed: true })
    }
    gone = []
    come = []
  }

  let i = 0
  let j = 0
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      flush()
      rows.push({ left: { n: i + 1, text: a[i] }, right: { n: j + 1, text: b[j] }, changed: false })
      i++
      j++
    } else if (j >= b.length || (i < a.length && common[i + 1][j] >= common[i][j + 1])) {
      gone.push({ n: i + 1, text: a[i] })
      i++
    } else {
      come.push({ n: j + 1, text: b[j] })
      j++
    }
  }
  flush()
  return rows
}

/** Leaves a few unchanged lines around each change and counts the others. */
export function fold(rows: DiffRow[], context = 3): FoldedRow[] {
  const near = rows.map((_, at) =>
    rows.slice(Math.max(0, at - context), at + context + 1).some((row) => row.changed)
  )
  const out: FoldedRow[] = []
  for (let at = 0; at < rows.length; ) {
    let end = at
    while (end < rows.length && !near[end]) end++
    // One line takes no more room than saying it was left out.
    if (end - at > 1) {
      out.push({ skipped: end - at })
      at = end
    } else {
      out.push(rows[at])
      at++
    }
  }
  return out
}
