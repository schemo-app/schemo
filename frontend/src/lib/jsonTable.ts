import type { JsonNode } from './jsonTree'

export interface TableColumn {
    key: string | null
    id: string
}

export interface TableSort {
    column: TableColumn
    descending: boolean
}

function isObject(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value)
}

/** Keys are collected from every item, including fields absent from the first object. */
export function tableColumns(array: JsonNode): TableColumn[] {
    const keys = new Set<string>()
    let primitives = false
    for (const row of array.children) {
        if (isObject(row.value)) Object.keys(row.value).forEach(key => keys.add(key))
        else primitives = true
    }
    const columns: TableColumn[] = [...keys].map(key => ({ key, id: JSON.stringify(key) }))
    if (primitives) columns.unshift({ key: null, id: '$value' })
    return columns
}

export function tableCell(row: JsonNode, column: TableColumn): unknown {
    if (column.key === null) return isObject(row.value) ? undefined : row.value
    return isObject(row.value) && Object.hasOwn(row.value, column.key) ? row.value[column.key] : undefined
}

export function cellText(value: unknown): string {
    if (value === undefined) return ''
    if (typeof value === 'string') return value
    return JSON.stringify(value)
}

/** Rows retain their original paths after filtering and sorting, so the inspector opens the source. */
export function tableRows(
    array: JsonNode,
    columns: readonly TableColumn[],
    queries: readonly string[],
    sort: TableSort | null
): JsonNode[] {
    const needles = queries.map(query => query.trim().toLowerCase()).filter(Boolean)
    const rows = array.children.filter(row => {
        if (!needles.length) return true
        const text = columns.map(column => cellText(tableCell(row, column))).join('\n').toLowerCase()
        return needles.every(needle => text.includes(needle))
    })
    if (!sort) return rows
    return rows.sort((first, second) => {
        const left = tableCell(first, sort.column)
        const right = tableCell(second, sort.column)
        const order = compareValues(left, right)
        return sort.descending ? -order : order
    })
}

/** The exported selection is all matching rows in display order, regardless of the visible page. */
export function tableValues(rows: readonly JsonNode[], columns: readonly TableColumn[]): string[][] {
    return rows.map(row => columns.map(column => cellText(tableCell(row, column))))
}

function compareValues(left: unknown, right: unknown): number {
    const leftEmpty = left === undefined || left === null
    const rightEmpty = right === undefined || right === null
    if (leftEmpty || rightEmpty) return Number(leftEmpty) - Number(rightEmpty)
    if (typeof left === 'number' && typeof right === 'number') return left - right
    if (typeof left === 'boolean' && typeof right === 'boolean') return Number(left) - Number(right)
    return cellText(left).localeCompare(cellText(right), undefined, { numeric: true })
}
