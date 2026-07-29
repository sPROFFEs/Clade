import { Marked } from 'marked'

const escapeHTML = (value) =>
  String(value).replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[char])

function safeLink(href) {
  const value = String(href || '').trim()
  return /^(https?:|mailto:|#|\/)/i.test(value) ? value : ''
}

const markdown = new Marked({ gfm: true, breaks: true })
markdown.use({
  renderer: {
    // Model output is untrusted. Preserve raw HTML visibly instead of
    // letting it become executable markup inside the desktop WebView.
    html({ text }) {
      return escapeHTML(text)
    },
    link({ href, title, tokens }) {
      const label = this.parser.parseInline(tokens)
      const safeHref = safeLink(href)
      if (!safeHref) return label
      const safeTitle = title ? ` title="${escapeHTML(title)}"` : ''
      return `<a href="${escapeHTML(safeHref)}"${safeTitle} target="_blank" rel="noopener noreferrer">${label}</a>`
    },
    // Remote Markdown images would make network requests without an
    // explicit user action. Show their alt text instead.
    image({ text }) {
      return escapeHTML(text || '')
    },
  },
})

export function renderMarkdown(source) {
  let html = markdown.parse(String(source || ''))
  // Normalize task-list syntax that some model outputs leave literal.
  html = html.replace(/<li>\s*\[(\s|x|X)\]\s?/g, (_match, checked) =>
    `<li class="task-item"><input type="checkbox" disabled${checked.trim() ? ' checked' : ''}> `)
  return html
}
