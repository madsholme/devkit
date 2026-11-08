const escape = (text: string) => text.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!))

export function imageTarget(href: string, current: string, checkout: string): { local?: string; external?: string } | null {
  if (/^https?:\/\//i.test(href)) {
    try {
      const url = new URL(href)
      return url.username || url.password ? null : { external: url.href }
    } catch { return null }
  }
  if (!checkout || /^[a-z][a-z\d+.-]*:/i.test(href) || href.startsWith('/') || /[\\?\x00-\x1f]/.test(href)) return null
  try {
    const parts = current.split('/').slice(0, -1)
    for (const part of decodeURIComponent(href.split('#')[0]).split('/')) {
      if (part === '..') {
        if (!parts.length) return null
        parts.pop()
      } else if (part !== '.') {
        if (!part || part.startsWith('.') || /[\\\x00-\x1f]/.test(part)) return null
        parts.push(part)
      }
    }
    const path = parts.join('/')
    if (!/\.(png|jpe?g|gif|webp|svg)$/i.test(path)) return null
    return { local: '/api/document-image?' + new URLSearchParams({ worktree: checkout, path }) }
  } catch { return null }
}

export function renderImage(src: string, alt: string, path: string, checkout: string, size = ''): string {
  const target = imageTarget(src, path, checkout)
  if (target?.local) return `<img src="${escape(target.local)}" alt="${escape(alt)}" loading="lazy" decoding="async" referrerpolicy="no-referrer"${size}>`
  if (target?.external) return `<button type="button" class="document-external-image" data-external-image="${escape(target.external)}" data-image-alt="${escape(alt)}">Load external image: ${escape(alt || new URL(target.external).hostname)}</button>`
  return `<span class="document-image-note">[Image unavailable: ${escape(alt || src)}]</span>`
}

// Reconstruct only the three media tags and a tiny attribute allowlist. Never
// insert author HTML directly; template parsing is inert (no image requests).
export function renderMediaHTML(html: string, path: string, checkout: string): string {
  if (/^<\/picture\s*>$/i.test(html.trim())) return '</picture>'
  if (!/^\s*<(picture|source|img)(?:\s|>)/i.test(html) || typeof document === 'undefined') return escape(html)
  const template = document.createElement('template')
  template.innerHTML = html
  function nodeHTML(node: Node): string {
    if (node.nodeType === Node.TEXT_NODE) return escape(node.textContent || '')
    if (!(node instanceof Element)) return ''
    if (node.tagName === 'PICTURE') return `<picture>${Array.from(node.childNodes).map(nodeHTML).join('')}</picture>`
    if (node.tagName === 'IMG') {
      const width = node.getAttribute('width') || ''
      const size = /^\d{1,4}$/.test(width) && Number(width) > 0 ? ` width="${width}"` : ''
      return renderImage(node.getAttribute('src') || '', node.getAttribute('alt') || '', path, checkout, size)
    }
    if (node.tagName === 'SOURCE') {
      const candidates = (node.getAttribute('srcset') || '').split(',').map((candidate) => {
        const [src, descriptor = '', ...extra] = candidate.trim().split(/\s+/)
        const target = imageTarget(src, path, checkout)
        if (!target?.local || extra.length || (descriptor && !/^(?:[1-9]\d*w|(?:[1-9]\d*(?:\.\d+)?|0\.\d+)x)$/.test(descriptor))) return ''
        return target.local + (descriptor ? ' ' + descriptor : '')
      })
      if (!candidates.length || candidates.some((c) => !c)) return ''
      return `<source srcset="${escape(candidates.join(', '))}" media="${escape(node.getAttribute('media') || '')}">`
    }
    return escape(node.outerHTML)
  }
  // Inline <picture> opens a container whose closing token comes separately.
  if (/^<picture\s*>$/i.test(html.trim())) return '<picture>'
  return Array.from(template.content.childNodes).map(nodeHTML).join('')
}
