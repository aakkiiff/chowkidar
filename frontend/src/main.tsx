import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { applyStoredTheme } from './theme'

applyStoredTheme()

// Inject Google Fonts after first paint. Render-blocking <link> in index.html
// triggers Firefox's "Layout was forced before page fully loaded" warning;
// loading it via JS post-mount keeps the critical path text-only (fallback
// system fonts) for ~1 frame, then swaps once the stylesheet arrives.
requestIdleCallback?.(loadFonts) ?? setTimeout(loadFonts, 0)
function loadFonts() {
  const link = document.createElement('link')
  link.rel = 'stylesheet'
  link.href = 'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600;700&display=swap'
  document.head.appendChild(link)
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
