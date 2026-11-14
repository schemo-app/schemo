import { BodyKind, Protocol, type ProtocolOptions } from '../../bindings/schemo/internal/domain'
import type { ChipName } from './requestSource'
import type { MessageKey } from '../i18n'

type StreamPresentation = {
  count: MessageKey
  stop: MessageKey
  opened: MessageKey
  closed: MessageKey
  empty: MessageKey
  search: MessageKey
  outgoing: boolean
  accepts: (response: { status: number; contentType?: string }) => boolean
}

type ProtocolPresentation = {
  label?: string
  address: MessageKey
  send: MessageKey
  copyCommands?: boolean
  chipLabels?: Partial<Record<ChipName, MessageKey>>
  // gRPC metadata are lowercase keys, not HTTP's Header-Name.
  headerName?: MessageKey
  bodyKind?: BodyKind
  stream?: StreamPresentation
  responseHeaders?: MessageKey
  status?: 'grpc'
  operationRequired?: (options?: ProtocolOptions | null) => MessageKey | undefined
  chips: readonly [ChipName, ...ChipName[]]
}

const presentation: Record<Protocol, ProtocolPresentation> = {
  [Protocol.$zero]: {
    address: 'request.urlPlaceholder',
    send: 'request.send',
    copyCommands: true,
    chips: ['params', 'headers', 'body', 'auth', 'scripts', 'docs', 'examples'],
  },
  [Protocol.ProtocolGraphQL]: {
    label: 'GQL',
    address: 'request.protocol.gqlAddress',
    send: 'request.send',
    copyCommands: true,
    chipLabels: { body: 'request.protocol.query' },
    chips: ['body', 'headers', 'auth', 'schema', 'scripts', 'docs', 'examples'],
  },
  [Protocol.ProtocolWebSocket]: {
    label: 'WS',
    address: 'request.protocol.wsAddress',
    send: 'request.protocol.connect',
    chips: ['messages', 'params', 'headers', 'auth', 'scripts', 'docs'],
    stream: {
      count: 'request.protocol.messages',
      stop: 'request.protocol.disconnect',
      opened: 'request.protocol.connected',
      closed: 'request.protocol.disconnected',
      empty: 'request.protocol.emptyLog',
      search: 'request.protocol.messagePlaceholder',
      outgoing: true,
      accepts: response => response.status === 101,
    },
  },
  [Protocol.ProtocolSSE]: {
    label: 'SSE',
    address: 'request.protocol.sseAddress',
    send: 'request.protocol.connect',
    chips: ['params', 'headers', 'auth', 'docs'],
    stream: {
      count: 'request.protocol.events',
      stop: 'request.protocol.stop',
      opened: 'request.protocol.streaming',
      closed: 'request.protocol.stopped',
      empty: 'request.protocol.emptyEvents',
      search: 'request.protocol.eventSearch',
      outgoing: false,
      accepts: response => response.status === 200 && response.contentType === 'text/event-stream',
    },
  },
  [Protocol.ProtocolGRPC]: {
    label: 'gRPC',
    address: 'request.protocol.grpcAddress',
    send: 'request.protocol.invoke',
    chipLabels: { headers: 'request.protocol.metadata' },
    headerName: 'request.placeholderKey',
    bodyKind: BodyKind.BodyJSON,
    responseHeaders: 'request.protocol.metadata',
    status: 'grpc',
    operationRequired: options => options?.rpcMethod ? undefined : 'request.protocol.chooseMethod',
    chips: ['body', 'headers', 'schema', 'scripts', 'docs', 'examples'],
  },
}

export const PROTOCOLS = Object.entries(presentation).flatMap(([value, item]) =>
  item.label ? [{ value: value as Exclude<Protocol, Protocol.$zero>, label: item.label }] : [],
)

export function protocolOf(protocol: Protocol | undefined): ProtocolPresentation {
  return presentation[protocol ?? Protocol.$zero] ?? presentation[Protocol.$zero]
}

export function requestLabel(request: { protocol?: Protocol; method?: string }): string {
  return protocolOf(request.protocol).label ?? request.method ?? 'GET'
}

export function isLiveProtocol(protocol?: Protocol): boolean {
  return !!protocolOf(protocol).stream
}

export function firstChip(protocol?: Protocol): ChipName {
  return protocolOf(protocol).chips[0]
}
