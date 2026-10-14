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

/** Whether a key reads after a dot, by the same rule the Go lens writes its paths with. */
function identifier(key: string): boolean {
  if (key === '') return false
  for (let i = 0; i < key.length; i++) {
    const ch = key[i]
    const letter = /[A-Za-z_$]/.test(ch)
    const digit = /[0-9]/.test(ch)
    if (letter || (i > 0 && digit)) continue
    return false
  }
  return true
}

/** The address of a child, written the way Go writes it, so a path picked here round-trips. */
function childPath(parent: string, key: string | number): string {
  if (typeof key === 'number') return `${parent}[${key}]`
  if (identifier(key)) return `${parent}.${key}`
  return `${parent}['${key.replaceAll('\\', '\\\\').replaceAll("'", "\\'")}']`
}

/** Every node of a value, keyed by its address; the root is `$` and reads whole. */
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

/** The nodes in the order the tree draws them: a parent before its children, siblings in place. */
export function jsonOrder(nodes: Map<string, JsonNode>): JsonNode[] {
  const out: JsonNode[] = []
  const dive = (node: JsonNode) => {
    out.push(node)
    for (const child of node.children) dive(child)
  }
  const root = nodes.get('$')
  if (root) dive(root)
  return out
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
