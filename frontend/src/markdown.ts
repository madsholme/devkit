import MarkdownIt from 'markdown-it'
import { renderImage, renderMediaHTML } from './document-media'

// Raw HTML tokens are reconstructed through the media-only allowlist below.
const markdown = new MarkdownIt({ html: true, linkify: false, typographer: false })
export function documentLink(
  href: string,
  current: string
): { path?: string; anchor?: string; external?: string } | null {
  if (/^https?:\/\//i.test(href)) {
    try {
      const url = new URL(href)
      return url.username || url.password ? null : { external: url.href }
    } catch {
      return null
    }
  }
  if (/^[a-z][a-z\d+.-]*:/i.test(href) || href.startsWith('/') || href.includes('\\')) return null
  try {
    const [raw, anchor = ''] = href.split('#')
    if (!raw) return { anchor: decodeURIComponent(anchor) }
    const parts = current.split('/').slice(0, -1)
    for (const part of decodeURIComponent(raw).split('/')) {
      if (part === '..') {
        if (!parts.length) return null
        parts.pop()
      } else if (part !== '.') parts.push(part)
    }
    const path = parts.join('/')
    if (!/^docs\/(?:[^/.][^/]*\/)*[^/.][^/]*\.md$/i.test(path) && !/^(?:agents?|readme)\.md$/i.test(path))
      return null
    return { path, anchor: decodeURIComponent(anchor) }
  } catch {
    return null
  }
}
markdown.renderer.rules.image = (tokens, idx, _options, env) =>
  renderImage(String(tokens[idx].attrGet('src') || ''), tokens[idx].content, String(env?.path || ''), String(env?.checkout || ''))
for (const rule of ['html_block', 'html_inline']) {
  markdown.renderer.rules[rule] = (tokens, idx, _options, env) =>
    renderMediaHTML(tokens[idx].content, String(env?.path || ''), String(env?.checkout || ''))
}
const fence = markdown.renderer.rules.fence!
markdown.renderer.rules.fence = (tokens, idx, options, env, self) => {
  if (tokens[idx].info.trim().toLowerCase() !== 'mermaid') return fence(tokens, idx, options, env, self)
  return `<div class="document-diagram" data-mermaid><pre><code>${markdown.utils.escapeHtml(tokens[idx].content)}</code></pre></div>`
}
markdown.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  const token = tokens[idx]
  const link = documentLink(String(token.attrGet('href') || ''), String(env?.path || ''))
  if (link?.external) {
    token.attrSet('href', link.external)
    token.attrSet('target', '_blank')
    token.attrSet('rel', 'noopener noreferrer')
  } else if (link) {
    token.attrSet('href', '#')
    if (link.path) token.attrSet('data-document', link.path)
    token.attrSet('data-anchor', link.anchor || '')
  } else {
    token.attrs = (token.attrs || []).filter(([name]) => name !== 'href')
    token.attrSet('aria-disabled', 'true')
  }
  return self.renderToken(tokens, idx, options)
}
export function renderMarkdown(content: string, path: string, checkout = '') {
  const tokens = markdown.parse(content, { path, checkout })
  const headings: { id: string; text: string; level: number }[] = []
  const used = new Map<string, number>()
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type !== 'heading_open') continue
    const text = tokens[i + 1]?.content || ''
    const base =
      text
        .toLowerCase()
        .replace(/[^\p{L}\p{N}\s-]/gu, '')
        .trim()
        .replace(/\s+/g, '-') || 'section'
    const count = used.get(base) || 0
    used.set(base, count + 1)
    const id = count ? `${base}-${count}` : base
    // Prefix IDs so document content cannot collide with dashboard controls.
    tokens[i].attrSet('id', `document-${id}`)
    headings.push({ id, text, level: Number(tokens[i].tag.slice(1)) })
  }
  return { html: markdown.renderer.render(tokens, markdown.options, { path, checkout }), headings }
}
