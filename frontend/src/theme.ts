export type Theme = 'light' | 'dark';

const KEY = 'chowkidar_theme';

export function getTheme(): Theme {
  return localStorage.getItem(KEY) === 'light' ? 'light' : 'dark';
}

export function setTheme(t: Theme) {
  localStorage.setItem(KEY, t);
  document.documentElement.setAttribute('data-theme', t);
}

export function applyStoredTheme() {
  document.documentElement.setAttribute('data-theme', getTheme());
}
