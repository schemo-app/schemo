import { markdownHeadings } from './markdown'
import type { MessageKey } from '../i18n'

const sections: { title: MessageKey; text: MessageKey; matches: RegExp }[] = [
  {
    title: 'collections.docsAuthorization', text: 'collections.docsAuthorizationHint',
    matches: /^(authorization|authentication|auth|авторизация)$/iu,
  },
  {
    title: 'collections.docsErrors', text: 'collections.docsErrorsHint',
    matches: /^(errors|ошибки)$/iu,
  },
  {
    title: 'collections.docsLimits', text: 'collections.docsLimitsHint',
    matches: /^(limits|rate limits|limitations|лимиты|ограничения)$/iu,
  },
]

export function missingDescriptionSections(markdown: string) {
  const headings = markdownHeadings(markdown, 2).filter(heading => heading.level === 2)
  return sections.filter(section => !headings.some(heading => section.matches.test(heading.label.trim().replace(/\s+/g, ' '))))
}
