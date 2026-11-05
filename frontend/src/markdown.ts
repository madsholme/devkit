import MarkdownIt from 'markdown-it'

// HTML is deliberately disabled. Images are text, so reading a plan never
// contacts tracking endpoints or requests arbitrary project assets.
const markdown = new MarkdownIt({ html: false, linkify: false, typographer: false })
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
    if (!/^docs\/(?:[^/.][^/]*\/)*[^/.][^/]*\.md$/i.test(path) && !/^agents?\.md$/i.test(path))
      return null
    return { path, anchor: decodeURIComponent(anchor) }
  } catch {
    return null
  }
}
markdown.renderer.rules.image = (tokens, idx) =>
  `<span class="document-image-note">[Image: ${markdown.utils.escapeHtml(
    tokens[idx].content
  )}]</span>`
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
export function renderMarkdown(content: string, path: string) {
  const tokens = markdown.parse(content, { path })
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
  return { html: markdown.renderer.render(tokens, markdown.options, { path }), headings }
}
