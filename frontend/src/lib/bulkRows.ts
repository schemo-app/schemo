import type { Row } from '../../bindings/schemo/internal/domain'
import { mintRowId, paramsFromUrl } from './draftRows'

export interface BulkRows {
    rows: Row[]
    errorLine: number | null
}

/** Disabled rows stay in the text; descriptions are kept when the edits are applied. */
export function rowsToText(rows: readonly Row[]): string {
    return rows.map(row => {
        const quoteName = row.name.trim() !== row.name || /[:=\r\n]/.test(row.name) || /^["#]|^\/\/|^$/.test(row.name)
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
        const commented = /^\s*(#|\/\/)/.test(line)

        try {
            const row = parseRow(line)
            if (!row) {
                if (commented) continue
                return { rows: [], errorLine: index + 1 }
            }
            rows.push(row)
        } catch {
            if (commented) continue
            return { rows: [], errorLine: index + 1 }
        }
    }

    return { rows, errorLine: null }
}

/** A table accepts a row list, spreadsheet columns, or the parameters of a query or URL. */
export function parsePastedRows(text: string, kind: 'params' | 'headers'): BulkRows {
    const trimmed = text.trim()
    if (kind === 'params' && (/^https?:\/\//i.test(trimmed) || trimmed.startsWith('?') ||
        (!/[\r\n]/.test(trimmed) && /^[^:\s=&]+[=&]/.test(trimmed)))) {
        const address = /^https?:\/\//i.test(trimmed) || trimmed.startsWith('?') ? trimmed : `?${trimmed}`
        return { rows: paramsFromUrl(address), errorLine: null }
    }
    if (!text.includes('\t')) return parseRows(text)

    const rows: Row[] = []
    for (const [index, line] of text.replaceAll('\r\n', '\n').split('\n').entries()) {
        if (!line.trim()) continue
        const separator = line.indexOf('\t')
        if (separator < 0) return { rows: [], errorLine: index + 1 }
        rows.push({id: '', name: line.slice(0, separator).trim(), value: line.slice(separator + 1), enabled: true})
    }
    return { rows, errorLine: null }
}

/** Parameters append; pasted headers replace existing names without case-sensitive duplicates. */
export function insertRows(existing: readonly Row[], pasted: readonly Row[], kind: 'params' | 'headers'): Row[] {
    const names = new Set(pasted.map(row => row.name.toLowerCase()))
    const kept = kind === 'headers' ? existing.filter(row => !names.has(row.name.toLowerCase())) : existing
    return [...kept, ...mergeRows(pasted, kind === 'headers' ? existing : [])]
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

/** Toggles complete selected lines without changing blank lines or unselected text. */
export function toggleRows(text: string, start: number, end: number): { text: string; start: number; end: number } {
    if (end > start && text[end - 1] === '\n') end--
    start = text.slice(0, start).lastIndexOf('\n') + 1
    const newline = text.indexOf('\n', end)
    end = newline < 0 ? text.length : newline

    const lines = text.slice(start, end).split('\n')
    const comment = /^(\s*)(#|\/\/) ?/
    const uncomment = lines.every(line => !line.trim() || comment.test(line))
    const replacement = lines.map(line => {
        if (!line.trim()) return line
        return uncomment ? line.replace(comment, '$1') : `# ${line}`
    }).join('\n')

    return {
        text: text.slice(0, start) + replacement + text.slice(end),
        start,
        end: start + replacement.length,
    }
}

function parseRow(line: string): Row | null {
    let text = line.trimStart()
    const enabled = !text.startsWith('#') && !text.startsWith('//')
    if (!enabled) text = text.replace(/^(#|\/\/)/, '').trimStart()

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
