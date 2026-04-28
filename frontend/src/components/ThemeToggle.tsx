import { useState, useCallback, type ReactElement } from 'react';
import { getTheme, setTheme as persistTheme, type Theme } from '../theme';

const OPTIONS: { value: Theme; title: string; icon: ReactElement }[] = [
  {
    value: 'light',
    title: 'Light',
    icon: (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
      </svg>
    ),
  },
  {
    value: 'dark',
    title: 'Dark',
    icon: (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
      </svg>
    ),
  },
];

export default function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => getTheme());

  const pick = useCallback((t: Theme) => {
    persistTheme(t);
    setTheme(t);
  }, []);

  return (
    <div className="theme-toggle" role="group" aria-label="Theme">
      {OPTIONS.map(o => (
        <button
          key={o.value}
          type="button"
          className="theme-btn"
          aria-pressed={theme === o.value}
          aria-label={`${o.title} theme`}
          title={o.title}
          onClick={() => pick(o.value)}
        >
          {o.icon}
        </button>
      ))}
    </div>
  );
}
