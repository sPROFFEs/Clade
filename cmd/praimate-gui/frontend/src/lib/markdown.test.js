import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { renderMarkdown } from './markdown.js'

test('renders assistant Markdown structure instead of raw syntax', () => {
  const html = renderMarkdown([
    '# Local File Inclusion',
    '',
    '**CWE-22**',
    '',
    '```php',
    'include($_GET["file"]);',
    '```',
    '',
    '| Aspect | Detail |',
    '| --- | --- |',
    '| Risk | High |',
  ].join('\n'))

  assert.match(html, /<h1>Local File Inclusion<\/h1>/)
  assert.match(html, /<strong>CWE-22<\/strong>/)
  assert.match(html, /<pre><code class="language-php">/)
  assert.match(html, /<table>/)
  assert.doesNotMatch(html, /```php/)
})

test('blocks active HTML, unsafe links, and remote images from model output', () => {
  const html = renderMarkdown([
    '<script>alert("x")</script>',
    '[unsafe](javascript:alert(1))',
    '[safe](https://example.com/reference)',
    '![tracking pixel](https://example.com/pixel.png)',
  ].join('\n\n'))

  assert.doesNotMatch(html, /<script>/)
  assert.doesNotMatch(html, /javascript:/)
  assert.doesNotMatch(html, /<img/)
  assert.match(html, /&lt;script&gt;/)
  assert.match(html, /href="https:\/\/example.com\/reference"/)
  assert.match(html, /tracking pixel/)
})

test('all assistant conversation surfaces use the shared renderer', async () => {
  const surfaceURLs = [
    new URL('./WorkflowRunner.svelte', import.meta.url),
    new URL('../pages/Chats.svelte', import.meta.url),
    new URL('../pages/AgentStudio.svelte', import.meta.url),
    new URL('../pages/Editor.svelte', import.meta.url),
  ]
  const sources = await Promise.all(surfaceURLs.map((url) => readFile(url, 'utf8')))

  for (const source of sources) {
    assert.match(source, /import \{ renderMarkdown \} from /)
    assert.match(source, /\{@html renderMarkdown\(/)
  }
  assert.doesNotMatch(sources[3], /import \{ marked \} from 'marked'/)
})
