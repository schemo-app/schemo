import { Protocol, type SchemaSource } from '../../bindings/schemo/internal/domain'
import { Drafts, type GraphQLSchema, type RPCMethod, type RPCSchema } from '../../bindings/schemo/internal/request'
import { t, textOf, type MessageKey } from '../i18n'
import type { RequestSource } from './requestSource'

export type SchemaOperation = {
  key: string
  name: string
  disabled: boolean
  selected: boolean
  select: () => void
}

export type OperationGroup = { name: string; short: string; items: SchemaOperation[] }

export type EditorSchema = {
  descriptors: string
  sources: SchemaSource[]
  warnings: string[]
  summary: () => string
  loaded: boolean
  groups: () => OperationGroup[]
  input?: { input: string; example: string }
}

type SchemaEditor = {
  load: (draft: NonNullable<ReturnType<RequestSource['draftSnapshot']>>, sources: SchemaSource[], signal: AbortSignal) => Promise<EditorSchema>
  describe: (descriptors: string, signal: AbortSignal) => Promise<EditorSchema>
  body: 'graphql' | 'protobuf'
  picked: (source: RequestSource) => string
  unfolded?: boolean
  placeholder: MessageKey
  search: MessageKey
  server: MessageKey
  hint: MessageKey
  fileSummary?: (count: number) => string
}

export function schemaEditor(protocol: Protocol, source: RequestSource): SchemaEditor | undefined {
  switch (protocol) {
    case Protocol.ProtocolGraphQL:
      return {
        load: async (draft, sources, signal) => graphQLSchema(source, await Drafts.LoadGraphQLSchema(draft, sources).cancelOn(signal)),
        describe: async (stored, signal) => graphQLSchema(source, await Drafts.DescribeGraphQLSchema(stored).cancelOn(signal)),
        body: 'graphql',
        picked: draft => graphQLOperation(draft.body)?.[2] ?? '',
        unfolded: true,
        placeholder: 'request.protocol.operation',
        search: 'request.protocol.findOperation',
        server: 'request.protocol.introspection',
        hint: 'request.protocol.gqlSchemaHint',
      }
    case Protocol.ProtocolGRPC:
      return {
        load: async (draft, sources, signal) => rpcSchema(source, await Drafts.LoadRPCSchema(draft, sources).cancelOn(signal)),
        describe: async (stored, signal) => rpcSchema(source, await Drafts.DescribeRPCSchema(stored).cancelOn(signal)),
        body: 'protobuf',
        picked: draft => shortName(draft.options?.rpcMethod ?? ''),
        placeholder: 'request.protocol.method',
        search: 'request.protocol.findMethod',
        server: 'request.protocol.reflection',
        hint: 'request.protocol.rpcSchemaHint',
        fileSummary: count => count ? t('request.protocol.methodsCount', count) : t('request.protocol.noServices'),
      }
  }
}

function graphQLOperation(body: string) {
  return /\b(query|mutation|subscription)\s+([_A-Za-z]\w*)/.exec(body)
}

function graphQLSchema(source: RequestSource, schema: GraphQLSchema): EditorSchema {
  const groups = schema.groups ?? []
  return {
    descriptors: schema.descriptors,
    sources: schema.sources ?? [],
    warnings: [],
    loaded: groups.length > 0,
    summary: () => t('request.protocol.operationsCount', groups.reduce((count, group) => count + (group.fields?.length ?? 0), 0)),
    groups() {
      const selected = graphQLOperation(source.body)
      return groups.filter(group => group.name !== 'subscription').map(group => {
        const name = textOf(`request.protocol.schemaGroups.${group.name}`)
        return {
          name,
          short: name,
          items: (group.fields ?? []).map(field => ({
            key: `${group.name}/${field.name}`, name: field.name, disabled: false,
            selected: selected?.[1] === group.name && selected?.[2] === field.name,
            select() {
              source.setBody(field.query ?? '')
              source.setOptions({ variables: field.variables ?? '', operationName: '' })
              source.setOpenChip('body')
            },
          })),
        }
      }).filter(group => group.items.length)
    },
  }
}

function methodKey(method: RPCMethod) {
  return `${method.service}/${method.name}`
}

function shortName(name: string) {
  return name.split('.').at(-1) ?? name
}

function rpcSchema(source: RequestSource, schema: RPCSchema): EditorSchema {
  const methods = schema.methods ?? []
  const services = Map.groupBy(methods, method => method.service)
  return {
    descriptors: schema.descriptors,
    sources: schema.sources ?? [],
    warnings: schema.warnings ?? [],
    loaded: methods.length > 0,
    summary: () => t('request.protocol.methodsCount', methods.length),
    get input() { return methods.find(method => methodKey(method) === source.options?.rpcMethod) },
    groups: () => Array.from(services, ([name, methods]) => ({
      name,
      short: shortName(name),
      items: methods.map(method => ({
        key: methodKey(method), name: method.name, disabled: !!method.clientStreaming,
        selected: methodKey(method) === source.options?.rpcMethod,
        select() {
          source.setBody(method.example)
          source.setOptions({ rpcMethod: methodKey(method) })
          source.setOpenChip('body')
        },
      })),
    })),
  }
}
