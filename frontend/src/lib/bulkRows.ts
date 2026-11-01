import type { Row } from '../../bindings/schemo/internal/domain'
import { mintRowId } from './draftRows'

export interface BulkRows {
    rows: Row[]
    errorLine: number | null
}

/** Disabled rows stay in the text; descriptions are kept when the edits are applied. */
export function rowsToText(rows: readonly Row[]): string {
    return rows.map(row => {
        const quoteName = row.name.trim() !== row.name || /[:=\r\n]/.test(row.name) || /^["#]|^$/.test(row.name)
        const escapeValue = row.value.trim() !== row.value || /[\r\n]/.test(row.value) || row.value.startsWith('@json ')
        const name = quoteName ? JSON.stringify(row.name) : row.name
        const value = escapeValue ? `@json ${JSON.stringify(row.value)}` : row.value

        return `${row.enabled ? '' : '# '}${name}: ${value}`
    }).join('\n')
}

/** Invalid input reports one line and never returns a partial replacement. */
export function parseRows(text: string): BulkRows {
    const rows: Row[] = []
    const lines = text.replaceAll('\r\n', '\n').split('\n')

    for (const [index, line] of lines.entries()) {
        if (!line.trim()) continue

        try {
            const row = parseRow(line)
            if (!row) return { rows: [], errorLine: index + 1 }
            rows.push(row)
        } catch {
            return { rows: [], errorLine: index + 1 }
        }
    }

    return { rows, errorLine: null }
}

/** Exact matches are reserved first so an edited duplicate cannot take another row's notes. */
export function mergeRows(rows: readonly Row[], existing: readonly Row[]): Row[] {
    const used = new Set<number>()
    const matches = rows.map(row => {
        const index = existing.findIndex((old, position) =>
            !used.has(position) && old.name === row.name && old.value === row.value
        )
        if (index >= 0) used.add(index)
        return index
    })

    return rows.map((row, position) => {
        let index = matches[position]
        if (index < 0) {
            index = existing.findIndex((old, candidate) => !used.has(candidate) && old.name === row.name)
            if (index >= 0) used.add(index)
        }

        const previous = existing[index]
        return { ...previous, ...row, id: previous?.id || mintRowId() }
    })
}

function parseRow(line: string): Row | null {
    let text = line.trimStart()
    const enabled = !text.startsWith('#')
    if (!enabled) text = text.slice(1).trimStart()

    let name: string
    if (text.startsWith('"')) {
        const match = text.match(/^"(?:[^"\\]|\\.)*"/)
        if (!match) return null
        name = JSON.parse(match[0])
        text = text.slice(match[0].length).trimStart()
    } else {
        const separator = text.search(/[:=]/)
        if (separator < 0) return null
        name = text.slice(0, separator).trim()
        text = text.slice(separator)
    }

    if (!/^[:=]/.test(text)) return null
    let value = text.slice(1).replace(/^ /, '')

    // ETag and If-Match quotes are literal; escaped values need an explicit marker.
    if (value.startsWith('@json ')) {
        const decoded: unknown = JSON.parse(value.slice(6))
        if (typeof decoded !== 'string') return null
        value = decoded
    }

    return { id: '', name, value, enabled }
}
