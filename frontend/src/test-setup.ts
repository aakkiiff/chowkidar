/// <reference types="@testing-library/jest-dom" />
import '@testing-library/jest-dom';

// jsdom lacks matchMedia; LoginScene uses it for prefers-reduced-motion.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  // @ts-ignore — test-env stub
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}
