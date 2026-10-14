import { reactive } from 'vue'
import type { Editor } from '@milkdown/kit/core'
import type { EditorState } from '@milkdown/kit/prose/state'

/** What the caret stands in, for the marks the toolbar lights. */
export interface Standing {
  bold: boolean
  italic: boolean
  ul: boolean
  ol: boolean
  /** `p`, `h1`…`h6`, `blockquote` or `pre`: the block the caret is in. */
  block: string
}

export type DocCommand = 'bold' | 'italic' | 'code' | 'ul' | 'ol' | 'quote' | 'pre' | 'hr' | 'p' | 'h1' | 'h2' | 'h3'

// Milkdown is the visual mode of a description: ProseMirror with the markdown as its document, so
// what is edited and what is stored are one text, serialised. It is loaded when a description is first
// written and not before — reading a page does not need an editor.
function load() {
  return Promise.all([
    import('@milkdown/kit/core'),
    import('@milkdown/kit/ctx'),
    import('@milkdown/kit/preset/commonmark'),
    import('@milkdown/kit/preset/gfm'),
    import('@milkdown/kit/plugin/history'),
    import('@milkdown/kit/plugin/listener'),
    import('@milkdown/kit/plugin/clipboard'),
    import('@milkdown/kit/plugin/trailing'),
    import('@milkdown/kit/prose/commands'),
    import('@milkdown/kit/prose/state'),
    import('@milkdown/kit/utils'),
  ]).then(([core, ctx, commonmark, gfm, history, listener, clipboard, trailing, commands, state, utils]) => ({
    core,
    ctx,
    commonmark,
    gfm,
    history,
    listener,
    clipboard,
    trailing,
    commands,
    state,
    utils,
  }))
}

type Kit = Awaited<ReturnType<typeof load>>

/**
 * The visual editor of a description. `onChange` hears the markdown as the document stands after a
 * change the person made; text put in from outside (`setText`) is not a change and is not heard.
 */
export function useMarkdownEditor(onChange: (markdown: string) => void) {
  const standing = reactive<Standing>({ bold: false, italic: false, ul: false, ol: false, block: 'p' })

  let kit: Kit | null = null
  let editor: Editor | null = null
  // A newer `open` or a `close` makes an older one that is still loading give up what it made.
  let turn = 0
  // The markdown last seen, so that what the editor serialises again after a text put in from outside
  // is not taken for a change.
  let seen = ''

  async function open(host: HTMLElement, markdown: string) {
    const mine = ++turn
    await close()
    kit ??= await load()
    if (mine !== turn) return
    const { core, commonmark, gfm, history, listener, clipboard, trailing, state } = kit
    seen = markdown

    const made = await core.Editor.make()
      .config((ctx) => {
        ctx.set(core.rootCtx, host)
        ctx.set(core.defaultValueCtx, markdown)
        ctx.update(core.editorViewOptionsCtx, (options) => ({
          ...options,
          attributes: { 'data-md': '', spellcheck: 'true' },
        }))
        ctx
          .get(listener.listenerCtx)
          .markdownUpdated((_, text) => {
            if (text === seen) return
            seen = text
            onChange(text)
          })
          .selectionUpdated(() => queueMicrotask(read))
      })
      .use(commonmark.commonmark)
      .use(gfm.gfm)
      .use(history.history)
      .use(listener.listener)
      .use(clipboard.clipboard)
      .use(trailing.trailing)
      .create()

    if (mine !== turn) {
      await made.destroy()
      return
    }
    editor = made
    made.action((ctx) => {
      const view = ctx.get(core.editorViewCtx)
      view.dispatch(view.state.tr.setSelection(state.Selection.atEnd(view.state.doc)))
      view.focus()
    })
    read()
  }

  async function close() {
    const was = editor
    editor = null
    await was?.destroy()
  }

  /** The markdown the document stands at now, read at once and not as the listener hears it. */
  function markdown(): string {
    if (!editor || !kit) return seen
    return editor.action(kit.utils.getMarkdown())
  }

  /** Puts in a text from outside; what a caller does after the person cancelled. */
  function setText(text: string) {
    if (!editor || !kit) return
    editor.action(kit.utils.replaceAll(text))
    seen = markdown()
  }

  function viewState(): EditorState | null {
    if (!editor || !kit) return null
    const { core } = kit
    return editor.action((ctx) => ctx.get(core.editorViewCtx).state)
  }

  function read() {
    const state = viewState()
    if (!state) return
    const { schema, selection } = state
    const { $from, from, to, empty } = selection

    const marked = (name: string) => {
      const type = schema.marks[name]
      if (!type) return false
      return empty ? !!type.isInSet(state.storedMarks ?? $from.marks()) : state.doc.rangeHasMark(from, to, type)
    }
    standing.bold = marked('strong')
    standing.italic = marked('emphasis')

    let ul = false
    let ol = false
    let quote = false
    for (let depth = $from.depth; depth > 0; depth--) {
      const name = $from.node(depth).type.name
      if (name === 'bullet_list') ul = true
      if (name === 'ordered_list') ol = true
      if (name === 'blockquote') quote = true
    }
    standing.ul = ul
    standing.ol = ol

    const parent = $from.parent
    if (parent.type.name === 'heading') standing.block = `h${parent.attrs.level}`
    else if (parent.type.name === 'code_block') standing.block = 'pre'
    else standing.block = quote ? 'blockquote' : 'p'
  }

  /** The address of the link the caret is in, or nothing. */
  function linkAtCaret(): string {
    const state = viewState()
    const type = state?.schema.marks.link
    if (!state || !type) return ''
    const { from, to, empty, $from } = state.selection
    if (empty) return String(type.isInSet($from.marks())?.attrs.href ?? '')
    let href = ''
    state.doc.nodesBetween(from, to, (node) => {
      href ||= String(type.isInSet(node.marks)?.attrs.href ?? '')
    })
    return href
  }

  /** Makes the selection a link to `href`; with nothing selected, writes the address as the link. */
  function link(href: string) {
    if (!editor || !kit) return
    const { commonmark, utils } = kit
    const there = linkAtCaret() !== ''
    const empty = viewState()?.selection.empty ?? true
    if (!href) {
      if (there) editor.action(utils.callCommand(commonmark.toggleLinkCommand.key, {}))
    } else if (there) {
      editor.action(utils.callCommand(commonmark.updateLinkCommand.key, { href }))
    } else if (empty) {
      editor.action(utils.insert(`[${href}](${href})`, true))
    } else {
      editor.action(utils.callCommand(commonmark.toggleLinkCommand.key, { href }))
    }
    read()
  }

  /** Writes markdown in at the caret. */
  function insert(text: string) {
    if (!editor || !kit) return
    editor.action(kit.utils.insert(text))
    read()
  }

  /** `fallback` is what a mark that needs a selection is made of when there is none. */
  function run(command: DocCommand, fallback = '') {
    if (!editor || !kit) return
    const { core, commonmark, commands, utils } = kit
    const call = (key: Parameters<typeof utils.callCommand>[0], payload?: unknown) =>
      editor!.action(utils.callCommand(key, payload))
    // A block the caret is already in is the one that is left, which is how the marks of the
    // toolbar behave too.
    const lift = () => editor!.action((ctx) => {
      const view = ctx.get(core.editorViewCtx)
      commands.lift(view.state, view.dispatch)
    })

    switch (command) {
      case 'bold':
        call(commonmark.toggleStrongCommand.key)
        break
      case 'italic':
        call(commonmark.toggleEmphasisCommand.key)
        break
      case 'code':
        if (fallback && viewState()?.selection.empty) editor.action(utils.insert(`\`${fallback}\``, true))
        else call(commonmark.toggleInlineCodeCommand.key)
        break
      case 'ul':
        if (standing.ul) call(commonmark.liftListItemCommand.key)
        else call(commonmark.wrapInBulletListCommand.key)
        break
      case 'ol':
        if (standing.ol) call(commonmark.liftListItemCommand.key)
        else call(commonmark.wrapInOrderedListCommand.key)
        break
      case 'quote':
        if (standing.block === 'blockquote') lift()
        else call(commonmark.wrapInBlockquoteCommand.key)
        break
      case 'pre':
        if (standing.block === 'pre') call(commonmark.turnIntoTextCommand.key)
        else call(commonmark.createCodeBlockCommand.key)
        break
      case 'hr':
        call(commonmark.insertHrCommand.key)
        break
      case 'p':
        call(commonmark.turnIntoTextCommand.key)
        break
      default:
        if (standing.block === command) call(commonmark.turnIntoTextCommand.key)
        else call(commonmark.wrapInHeadingCommand.key, Number(command.slice(1)))
    }
    read()
  }

  function focus() {
    if (!editor || !kit) return
    const { core } = kit
    editor.action((ctx) => ctx.get(core.editorViewCtx).focus())
  }

  return { standing, open, close, markdown, setText, linkAtCaret, link, insert, run, focus }
}
