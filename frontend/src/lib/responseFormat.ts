import { Indent } from '../../bindings/schemo/internal/domain'
import type { ViewerLanguage } from './viewerLanguage'

const javascript = [
  () => import('prettier/plugins/babel'),
  () => import('prettier/plugins/estree'),
]
const stylesheet = [() => import('prettier/plugins/postcss')]

const parsers = {
  'json-stringify': javascript,
  json5: javascript,
  babel: javascript,
  'babel-ts': javascript,
  html: [() => import('prettier/plugins/html')],
  xml: [() => import('@prettier/plugin-xml')],
  yaml: [() => import('prettier/plugins/yaml')],
  css: stylesheet,
  scss: stylesheet,
  less: stylesheet,
  graphql: [() => import('prettier/plugins/graphql')],
  markdown: [() => import('prettier/plugins/markdown')],
}

type Parser = keyof typeof parsers

const mediaParsers = new Map<string, Parser>([
  ['application/json', 'json-stringify'],
  ['text/json', 'json-stringify'],
  ['application/json5', 'json5'],
  ['text/html', 'html'],
  ['application/xml', 'xml'],
  ['text/xml', 'xml'],
  ['application/yaml', 'yaml'],
  ['application/x-yaml', 'yaml'],
  ['text/yaml', 'yaml'],
  ['text/x-yaml', 'yaml'],
  ['text/css', 'css'],
  ['text/scss', 'scss'],
  ['text/x-scss', 'scss'],
  ['text/less', 'less'],
  ['text/x-less', 'less'],
  ['application/javascript', 'babel'],
  ['application/ecmascript', 'babel'],
  ['application/x-javascript', 'babel'],
  ['text/javascript', 'babel'],
  ['text/ecmascript', 'babel'],
  ['text/typescript', 'babel-ts'],
  ['application/typescript', 'babel-ts'],
  ['application/graphql', 'graphql'],
  ['text/graphql', 'graphql'],
  ['text/markdown', 'markdown'],
  ['text/x-markdown', 'markdown'],
])

export function responseParser(contentType: string | undefined, language: ViewerLanguage): Parser | undefined {
  const media = (contentType ?? '').split(';')[0].trim().toLowerCase()
  const declared = mediaParsers.get(media)
  if (declared) return declared
  if (media.endsWith('+json')) return 'json-stringify'
  if (media.endsWith('+xml')) return 'xml'
  if (media.endsWith('+yaml')) return 'yaml'
  return language === 'json' ? 'json-stringify' : language ?? undefined
}

export async function formatResponse(text: string, parser: Parser, indent: Indent): Promise<string> {
  const [prettier, plugins] = await Promise.all([
    import('prettier/standalone'),
    Promise.all(parsers[parser].map(async load => (await load()).default)),
  ])
  return prettier.format(text, {
    parser,
    plugins,
    tabWidth: indent === Indent.IndentFour ? 4 : 2,
    useTabs: indent === Indent.IndentTab,
    embeddedLanguageFormatting: 'off',
    ...(parser === 'xml' && { xmlWhitespaceSensitivity: 'preserve' }),
  })
}
