import { createThemeController } from '@madsholme/themes'
import { store } from './format'
// Preserve the previous light/dark/system choice when adopting the shared theme registry.
try {
  if (!localStorage.getItem('devkit.swift-theme')) {
    const old = store.get('theme')
    localStorage.setItem(
      'devkit.swift-theme',
      old === 'light' ? 'rn-light' : old === 'auto' ? 'system' : 'rn'
    )
  }
} catch {
  /* Storage is optional. */
}
export const themeController = createThemeController({
  storageKey: 'devkit.swift-theme',
  fallbackTheme: 'rn',
})
themeController.init()
