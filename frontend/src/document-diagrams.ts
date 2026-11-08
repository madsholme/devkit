import DOMPurify from 'dompurify'

export function flowchartError(source: string): string {
  if (source.length > 20000) return 'Diagram exceeds 20,000 characters.'
  if (!/^\s*(?:flowchart|graph)\s+(?:TB|TD|BT|RL|LR)\b/.test(source)) return 'Only Mermaid flowcharts are supported.'
  if (/%%\{|@\{|(?:https?:|data:|javascript:|file:|url\s*\(|href\s*=)/i.test(source))
    return 'Diagram configuration, image shapes and external resources are not supported.'
  return ''
}
let nextID = 0
let queue: Promise<void> = Promise.resolve()

export function renderDiagrams(root: HTMLElement, current: () => boolean): Promise<void> {
  // Mermaid has global configuration and temporary DOM: serialize across readers.
  const work = async () => {
    const blocks = [...root.querySelectorAll<HTMLElement>('[data-mermaid]')]
    if (!blocks.length || !current()) return
    const { default: mermaid } = await import('mermaid')
    mermaid.initialize({
      startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true,
      maxTextSize: 20000, maxEdges: 250, htmlLabels: false,
      flowchart: { htmlLabels: false }, theme: 'neutral',
      secure: ['secure', 'securityLevel', 'startOnLoad', 'maxTextSize', 'maxEdges', 'suppressErrorRendering', 'htmlLabels', 'flowchart', 'theme', 'themeCSS', 'dompurifyConfig'],
    })
    for (const [index, block] of blocks.entries()) {
      if (!current()) return
      const source = block.querySelector('code')?.textContent || ''
      let error = index >= 20 ? 'Only the first 20 diagrams are rendered.' : flowchartError(source)
      if (!error) {
        try {
          const { svg } = await mermaid.render(`document-flowchart-${++nextID}`, source)
          if (!current()) return
          const picture = document.createElement('div')
          picture.className = 'document-diagram-rendered'
          picture.setAttribute('role', 'img')
          picture.setAttribute('aria-label', 'Mermaid flowchart')
          picture.innerHTML = DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true }, FORBID_TAGS: ['foreignObject', 'image', 'a'] })
          const details = document.createElement('details')
          const summary = document.createElement('summary')
          summary.textContent = 'Diagram source'
          details.append(summary, block.firstElementChild!)
          block.replaceChildren(picture, details)
        } catch { error = 'Could not render this flowchart. Check its Mermaid syntax.' }
      }
      if (error && current()) {
        const message = document.createElement('p')
        message.className = 'note'
        message.textContent = error
        block.prepend(message)
      }
    }
  }
  const result = queue.then(work)
  queue = result.catch(() => {})
  return result
}
