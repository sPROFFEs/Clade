<script>
  // CodeMirror 6 wrapper — the text editor used by the Agents YAML
  // editor and the document studio. Two contracts matter:
  //
  //   on:change  — fires with the full document on every user edit.
  //   setExternal(content) — merges externally-changed content (the
  //     agent wrote the file) into the live view with a MINIMAL change
  //     span (common prefix/suffix diff), so the user's cursor and
  //     scroll position survive instead of the whole buffer reloading.
  import { onMount, onDestroy, createEventDispatcher } from 'svelte'
  import { EditorView, basicSetup } from 'codemirror'
  import { EditorState, StateField, StateEffect, Compartment } from '@codemirror/state'
  import { showTooltip } from '@codemirror/view'
  import { StreamLanguage, syntaxHighlighting, HighlightStyle } from '@codemirror/language'
  import { tags as t } from '@lezer/highlight'
  import { markdown } from '@codemirror/lang-markdown'
  import { yaml } from '@codemirror/lang-yaml'
  import { javascript } from '@codemirror/lang-javascript'
  import { html as htmlLang } from '@codemirror/lang-html'
  import { css as cssLang } from '@codemirror/lang-css'
  import { python } from '@codemirror/lang-python'
  import { rust } from '@codemirror/lang-rust'
  import { json as jsonLang } from '@codemirror/lang-json'
  // Legacy stream-mode grammars for languages that don't have a
  // first-class @codemirror/lang-* pack (shell, go, toml, sql, ini,
  // dockerfile). Highlighting is coarser than the lezer-based packs but
  // covers keywords, strings, comments, numbers — enough for an IDE-ish
  // editor.
  import { shell } from '@codemirror/legacy-modes/mode/shell'
  import { go as goMode } from '@codemirror/legacy-modes/mode/go'
  import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile'
  import { toml } from '@codemirror/legacy-modes/mode/toml'
  import { sql as sqlMode } from '@codemirror/legacy-modes/mode/sql'
  import { properties } from '@codemirror/legacy-modes/mode/properties'

  export let value = ''
  // 'markdown' | 'yaml' | 'javascript' | 'html' | 'css' | 'python' |
  // 'shell' | 'go' | 'rust' | 'dockerfile' | 'toml' | 'sql' | 'ini' | 'plain'
  export let lang = 'markdown'

  const dispatch = createEventDispatcher()
  let host
  let view
  let applyingExternal = false

  // Ask-menu tooltip — rendered through CodeMirror's OWN tooltip layer.
  // On this app's webkit targets, arbitrary overlays (any z-index, even
  // portaled to <body>) can be painted UNDER the editor text by buggy
  // GPU paths; CM tooltips are the one overlay channel the editor
  // guarantees to layer above its content everywhere.
  const setAskTooltip = StateEffect.define()
  const askTooltipField = StateField.define({
    create: () => null,
    update(value, tr) {
      for (const e of tr.effects) if (e.is(setAskTooltip)) value = e.value
      return value
    },
    provide: (f) => showTooltip.from(f),
  })

  function langExt() {
    switch (lang) {
      case 'yaml':       return [yaml()]
      case 'markdown':   return [markdown()]
      case 'javascript': return [javascript({ jsx: true, typescript: true })]
      case 'html':       return [htmlLang()]
      case 'css':        return [cssLang()]
      case 'python':     return [python()]
      case 'rust':       return [rust()]
      case 'json':       return [jsonLang()]
      case 'shell':      return [StreamLanguage.define(shell)]
      case 'go':         return [StreamLanguage.define(goMode)]
      case 'dockerfile': return [StreamLanguage.define(dockerFile)]
      case 'toml':       return [StreamLanguage.define(toml)]
      case 'sql':        return [StreamLanguage.define(sqlMode)]
      case 'ini':        return [StreamLanguage.define(properties)]
      default:           return []
    }
  }

  // Dark / light syntax palettes. Chosen for AA-readable contrast on
  // this app's near-black / near-white backgrounds: avoids dark blue on
  // dark bg (a common "unreadable" combo), uses warm hues for keywords
  // on dark, dark blues only on light. Edit either palette in one place.
  const DARK = {
    fg: '#e6edf3', caret: '#e6edf3', selBg: '#264f78',
    keyword: '#ff7b72', operator: '#ff7b72',
    string:  '#a5d6ff', number:  '#79c0ff',
    comment: '#8b949e',
    func:    '#d2a8ff', type:    '#ffa657', variable: '#e6edf3',
    prop:    '#79c0ff', tag:     '#7ee787', attr:    '#d2a8ff',
    heading: '#f0883e', meta:    '#8b949e',
    link:    '#a5d6ff',
  }
  const LIGHT = {
    fg: '#1f2328', caret: '#1f2328', selBg: '#b6d2f0',
    keyword: '#cf222e', operator: '#cf222e',
    string:  '#0a3069', number:  '#0550ae',
    comment: '#6e7781',
    func:    '#8250df', type:    '#953800', variable: '#1f2328',
    prop:    '#0550ae', tag:     '#116329', attr:    '#8250df',
    heading: '#953800', meta:    '#6e7781',
    link:    '#0550ae',
  }

  function buildHighlight(p) {
    return HighlightStyle.define([
      { tag: [t.keyword, t.controlKeyword, t.modifier, t.self, t.null], color: p.keyword, fontWeight: 600 },
      { tag: [t.operator, t.operatorKeyword, t.compareOperator, t.logicOperator, t.bitwiseOperator, t.arithmeticOperator], color: p.operator },
      { tag: [t.string, t.special(t.string), t.regexp], color: p.string },
      { tag: [t.number, t.bool, t.atom], color: p.number },
      { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: p.comment, fontStyle: 'italic' },
      { tag: [t.function(t.variableName), t.function(t.propertyName), t.macroName], color: p.func },
      { tag: [t.typeName, t.className, t.namespace], color: p.type },
      { tag: [t.variableName, t.definition(t.variableName)], color: p.variable },
      { tag: [t.propertyName, t.definition(t.propertyName)], color: p.prop },
      { tag: [t.tagName, t.angleBracket], color: p.tag },
      { tag: [t.attributeName], color: p.attr },
      { tag: [t.attributeValue], color: p.string },
      { tag: [t.heading, t.heading1, t.heading2, t.heading3, t.heading4, t.heading5, t.heading6], color: p.heading, fontWeight: 700 },
      { tag: [t.strong], fontWeight: 700 },
      { tag: [t.emphasis], fontStyle: 'italic' },
      { tag: [t.link, t.url], color: p.link, textDecoration: 'underline' },
      { tag: [t.meta, t.processingInstruction, t.escape], color: p.meta },
      { tag: [t.invalid], color: '#ff5555', textDecoration: 'underline' },
    ])
  }

  // Editor chrome (background, gutter, selection, caret). Tracks the
  // host CSS vars so the editor matches the panel even when the user
  // tweaks the accent. Syntax colours come from buildHighlight().
  function buildChrome(p, dark) {
    return EditorView.theme({
      '&': { height: '100%', fontSize: '13px', backgroundColor: 'var(--bg-input)', color: p.fg },
      '.cm-scroller': { fontFamily: 'var(--mono, ui-monospace, monospace)' },
      '&.cm-focused': { outline: 'none' },
      '.cm-content': { caretColor: p.caret },
      '.cm-cursor, .cm-dropCursor': { borderLeftColor: p.caret },
      '&.cm-focused .cm-selectionBackground, ::selection, .cm-selectionBackground': { backgroundColor: p.selBg },
      '.cm-gutters': {
        backgroundColor: 'var(--bg-panel)',
        color: p.comment,
        border: 'none',
        borderRight: '1px solid var(--border)',
      },
      '.cm-activeLine': { backgroundColor: dark ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.035)' },
      '.cm-activeLineGutter': { backgroundColor: dark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.05)', color: p.fg },
      '.cm-matchingBracket, .cm-nonmatchingBracket': {
        backgroundColor: dark ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.06)',
        outline: '1px solid ' + (dark ? '#3b4252' : '#cad1d8'),
      },
      '.cm-tooltip': {
        backgroundColor: 'var(--bg-raised)',
        color: p.fg,
        border: '1px solid var(--border)',
      },
      '.cm-panels': { backgroundColor: 'var(--bg-panel)', color: p.fg },
    }, { dark })
  }

  // Compartments let us swap language (when `lang` changes) and theme
  // (when the user toggles dark/light at runtime) without rebuilding
  // the EditorState — keeps cursor/scroll/history intact.
  const langComp = new Compartment()
  const themeComp = new Compartment()

  function isLightMode() {
    return typeof document !== 'undefined' && document.documentElement.classList.contains('light')
  }
  function themeExts() {
    const dark = !isLightMode()
    const palette = dark ? DARK : LIGHT
    return [buildChrome(palette, dark), syntaxHighlighting(buildHighlight(palette))]
  }

  let themeObserver
  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          askTooltipField,
          langComp.of(langExt()),
          themeComp.of(themeExts()),
          EditorView.lineWrapping,
          EditorView.updateListener.of((u) => {
            if (u.docChanged && !applyingExternal) {
              dispatch('change', u.state.doc.toString())
            }
          }),
          // Right-click → "ask the agent about this text". Uses the
          // selection, or the current line when nothing is selected.
          EditorView.domEventHandlers({
            contextmenu: (e, v) => {
              const sel = v.state.selection.main
              let from = sel.from
              let to = sel.to
              if (from === to) {
                const line = v.state.doc.lineAt(from)
                from = line.from
                to = line.to
              }
              const text = v.state.sliceDoc(from, to)
              if (!text.trim()) return false
              e.preventDefault()
              dispatch('askctx', {
                text,
                fromLine: v.state.doc.lineAt(from).number,
                toLine: v.state.doc.lineAt(to).number,
                x: e.clientX,
                y: e.clientY,
              })
              return true
            },
          }),
        ],
      }),
    })
    // Live-react to theme toggles: lib/theme.js flips the `.light`
    // class on <html>; we re-dispatch chrome+highlight into the
    // theme compartment so syntax stays legible without remounting.
    if (typeof MutationObserver !== 'undefined') {
      themeObserver = new MutationObserver(() => {
        view?.dispatch({ effects: themeComp.reconfigure(themeExts()) })
      })
      themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    }
  })

  // Reconfigure the language compartment when the bound `lang` prop
  // changes (e.g. user switches tabs in the studio).
  $: if (view) view.dispatch({ effects: langComp.reconfigure(langExt()) })

  onDestroy(() => { themeObserver?.disconnect(); view?.destroy() })

  // Minimal single-span diff: common prefix + common suffix. Enough to
  // keep the cursor stable for the typical agent edit (one region of
  // the file changes); a multi-hunk diff is a later refinement.
  export function setExternal(content) {
    if (!view) return
    const cur = view.state.doc.toString()
    if (cur === content) return
    let p = 0
    const minLen = Math.min(cur.length, content.length)
    while (p < minLen && cur[p] === content[p]) p++
    let sa = cur.length
    let sb = content.length
    while (sa > p && sb > p && cur[sa - 1] === content[sb - 1]) { sa--; sb-- }
    applyingExternal = true
    view.dispatch({ changes: { from: p, to: sa, insert: content.slice(p, sb) } })
    applyingExternal = false
  }

  export function getValue() {
    return view ? view.state.doc.toString() : value
  }

  // openAskMenu shows makeDOM()'s element as a CM tooltip anchored at
  // the end of the current selection (or cursor line). closeAskMenu
  // hides it. The caller owns the DOM contents and wiring.
  export function openAskMenu(makeDOM) {
    if (!view) return
    const pos = view.state.selection.main.to
    view.dispatch({
      effects: setAskTooltip.of({
        pos,
        above: false,
        strictSide: false,
        arrow: false,
        create: () => {
          const dom = makeDOM()
          // Tag the .cm-tooltip wrapper CodeMirror puts around our DOM so
          // we can theme it by class instead of :has() (which older
          // WebKitGTK doesn't support — leaving the menu on CM's default
          // light tooltip styling).
          return { dom, mount: () => dom.parentElement?.classList.add('ask-tooltip') }
        },
      }),
    })
  }

  export function closeAskMenu() {
    if (!view) return
    view.dispatch({ effects: setAskTooltip.of(null) })
  }

  // --- formatting helpers for the studio toolbar ----------------------

  // wrapSelection surrounds the selection with prefix/suffix (toggling
  // off when already wrapped). With no selection it inserts the
  // placeholder and SELECTS it, so the user just types over it; with a
  // selection the cursor lands after the wrapped text.
  export function wrapSelection(prefix, suffix, placeholder = 'text') {
    if (!view) return
    const sel = view.state.selection.main
    const selected = view.state.sliceDoc(sel.from, sel.to)
    let insert
    let selection
    if (!selected) {
      insert = prefix + placeholder + suffix
      selection = { anchor: sel.from + prefix.length, head: sel.from + prefix.length + placeholder.length }
    } else if (selected.startsWith(prefix) && selected.endsWith(suffix) && selected.length >= prefix.length + suffix.length) {
      insert = selected.slice(prefix.length, selected.length - suffix.length)
      selection = { anchor: sel.from + insert.length }
    } else {
      insert = prefix + selected + suffix
      selection = { anchor: sel.from + insert.length }
    }
    view.dispatch({ changes: { from: sel.from, to: sel.to, insert }, selection })
    view.focus()
  }

  // toggleLinePrefix adds/removes a prefix on every selected line
  // (headings, lists, quotes). The cursor ends at the END of its line
  // — CodeMirror's default mapping leaves it BEFORE a prefix inserted
  // at the cursor position (the empty-line heading case), which forced
  // users to reposition by hand.
  export function toggleLinePrefix(prefix) {
    if (!view) return
    const sel = view.state.selection.main
    const startLine = view.state.doc.lineAt(sel.from).number
    const endLine = view.state.doc.lineAt(sel.to).number
    const headLineNo = view.state.doc.lineAt(sel.head).number
    const changes = []
    let allHave = true
    for (let n = startLine; n <= endLine; n++) {
      if (!view.state.doc.line(n).text.startsWith(prefix)) { allHave = false; break }
    }
    for (let n = startLine; n <= endLine; n++) {
      const line = view.state.doc.line(n)
      if (allHave) {
        changes.push({ from: line.from, to: line.from + prefix.length, insert: '' })
      } else if (!line.text.startsWith(prefix)) {
        changes.push({ from: line.from, insert: prefix })
      }
    }
    if (changes.length) {
      view.dispatch({ changes })
      const line = view.state.doc.line(Math.min(headLineNo, view.state.doc.lines))
      view.dispatch({ selection: { anchor: line.to } })
    }
    view.focus()
  }

  // insertSnippet drops text at the cursor, leaving the cursor after it.
  export function insertSnippet(snippet) {
    if (!view) return
    const sel = view.state.selection.main
    view.dispatch({
      changes: { from: sel.from, to: sel.to, insert: snippet },
      selection: { anchor: sel.from + snippet.length },
    })
    view.focus()
  }
</script>

<div class="cm-host" bind:this={host}></div>

<style>
  .cm-host {
    height: 100%;
    min-height: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    background: var(--bg-input);
  }
  .cm-host :global(.cm-editor) { height: 100%; }
</style>
