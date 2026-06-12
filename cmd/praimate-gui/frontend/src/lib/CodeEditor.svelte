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
  import { EditorState, Compartment } from '@codemirror/state'
  import { markdown } from '@codemirror/lang-markdown'
  import { yaml } from '@codemirror/lang-yaml'

  export let value = ''
  export let lang = 'markdown' // 'markdown' | 'yaml' | 'plain'

  const dispatch = createEventDispatcher()
  let host
  let view
  let applyingExternal = false

  function langExt() {
    if (lang === 'yaml') return [yaml()]
    if (lang === 'markdown') return [markdown()]
    return []
  }

  const theme = EditorView.theme({
    '&': { height: '100%', fontSize: '13px' },
    '.cm-scroller': { fontFamily: 'var(--mono, ui-monospace, monospace)' },
    '&.cm-focused': { outline: 'none' },
  })

  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          ...langExt(),
          theme,
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
  })

  onDestroy(() => view?.destroy())

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

  // --- formatting helpers for the studio toolbar ----------------------

  // wrapSelection surrounds the selection with prefix/suffix (toggling
  // off when already wrapped); with no selection inserts placeholder.
  export function wrapSelection(prefix, suffix, placeholder = 'text') {
    if (!view) return
    const sel = view.state.selection.main
    const selected = view.state.sliceDoc(sel.from, sel.to)
    let insert
    if (!selected) {
      insert = prefix + placeholder + suffix
    } else if (selected.startsWith(prefix) && selected.endsWith(suffix) && selected.length >= prefix.length + suffix.length) {
      insert = selected.slice(prefix.length, selected.length - suffix.length)
    } else {
      insert = prefix + selected + suffix
    }
    view.dispatch({
      changes: { from: sel.from, to: sel.to, insert },
      selection: { anchor: sel.from, head: sel.from + insert.length },
    })
    view.focus()
  }

  // toggleLinePrefix adds/removes a prefix on every selected line
  // (headings, lists, quotes).
  export function toggleLinePrefix(prefix) {
    if (!view) return
    const sel = view.state.selection.main
    const startLine = view.state.doc.lineAt(sel.from).number
    const endLine = view.state.doc.lineAt(sel.to).number
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
    if (changes.length) view.dispatch({ changes })
    view.focus()
  }

  // insertSnippet drops text at the cursor (tables, links, rules).
  export function insertSnippet(snippet) {
    if (!view) return
    const sel = view.state.selection.main
    view.dispatch({ changes: { from: sel.from, to: sel.to, insert: snippet } })
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
    background: var(--bg);
  }
  .cm-host :global(.cm-editor) { height: 100%; }
</style>
