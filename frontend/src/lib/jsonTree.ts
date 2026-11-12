/** What a JSON value is, in the words the lens uses — the tree and the inspector agree by this. */
export type JsonKind = 'object' | 'array' | 'string' | 'number' | 'boolean' | 'null'

export interface JsonNode {
  key: string | number
  /** The canonical address, in the same JSONPath Go writes: `$.a[0]['b c']`. */
  path: string
  parent: string | null
  depth: number
  kind: JsonKind
  value: unknown
  children: JsonNode[]
}

function jsonKindOf(value: unknown): JsonKind {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'array'
  const kind = typeof value
  if (kind === 'object') return 'object'
  if (kind === 'string' || kind === 'number' || kind === 'boolean') return kind
  return 'null'
}

export function isContainer(kind: JsonKind): boolean {
  return kind === 'object' || kind === 'array'
}

const identifier = /^[\p{L}_][\p{L}\p{Nd}_]*$/u

/** The address of a child, written the way Go writes it, so a path picked here round-trips. */
export function childPath(parent: string, key: string | number): string {
  if (typeof key === 'number') return `${parent}[${key}]`
  if (identifier.test(key)) return `${parent}.${key}`
  return `${parent}['${key.replaceAll('\\', '\\\\').replaceAll("'", "\\'")}']`
}

/** Nodes in document order, keyed by address, with the root first. */
export function buildJsonTree(value: unknown): Map<string, JsonNode> {
  const nodes = new Map<string, JsonNode>()

  const walk = (at: unknown, key: string | number, path: string, parent: string | null, depth: number) => {
    const kind = jsonKindOf(at)
    const node: JsonNode = { key, path, parent, depth, kind, value: at, children: [] }
    nodes.set(path, node)
    if (parent) nodes.get(parent)?.children.push(node)
    if (kind === 'array') {
      ;(at as unknown[]).forEach((item, i) => walk(item, i, childPath(path, i), path, depth + 1))
    } else if (kind === 'object') {
      for (const [k, item] of Object.entries(at as Record<string, unknown>)) {
        walk(item, k, childPath(path, k), path, depth + 1)
      }
    }
  }

  walk(value, '$', '$', null, 0)
  return nodes
}

/** A row's words: a primitive shows itself, a container shows how many things stand inside it. */
export interface JsonValueView {
  text: string
  /** Which of the palette's inks the value takes: a string, a number, a word, or the quiet grey. */
  ink: 'string' | 'number' | 'type' | 'quiet'
  count: number
}

export function jsonValueView(node: JsonNode, count: (kind: 'fields' | 'items', n: number) => string): JsonValueView {
  switch (node.kind) {
    case 'string':
      return { text: JSON.stringify(node.value), ink: 'string', count: 0 }
    case 'number':
      return { text: String(node.value), ink: 'number', count: 0 }
    case 'boolean':
    case 'null':
      return { text: String(node.value), ink: 'type', count: 0 }
    case 'array':
      return { text: `[ ${count('items', node.children.length)} ]`, ink: 'quiet', count: node.children.length }
    default:
      return { text: `{ ${count('fields', node.children.length)} }`, ink: 'quiet', count: node.children.length }
  }
}
