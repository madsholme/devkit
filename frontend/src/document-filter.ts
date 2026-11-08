export const documentCategories = [
  { value: 'all', label: 'All documents' },
  { value: 'milestones', label: 'Milestones' },
  { value: 'plans', label: 'Plans' },
  { value: 'specs', label: 'Specs' },
  { value: 'readme', label: 'README' },
]

export function matchesDocumentCategory(path: string, category: string): boolean {
  const tokens = path.toLowerCase().split(/[^a-z0-9]+/)
  switch (category) {
    case 'milestones':
      return tokens.some((token) => /^milestones?\d*$/.test(token) || /^m\d+$/.test(token))
    case 'plans':
      return tokens.some((token) => /^plans?$/.test(token))
    case 'specs':
      return tokens.some((token) => /^(specs?|specifications?)$/.test(token))
    case 'readme':
      return /^readme\.md$/i.test(path.split('/').pop() || '')
    default:
      return true
  }
}
