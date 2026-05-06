import { useState, useEffect, useRef, useMemo } from 'react';

export default function SingleSelect({
  options,
  value,
  onChange,
  placeholder = 'Select…',
  disabled = false,
  searchable = true,
  searchThreshold = 6,
}: {
  options: { label: string; value: string }[];
  value: string | null;
  onChange: (v: string) => void;
  placeholder?: string;
  disabled?: boolean;
  /** Show search input. Defaults to true. */
  searchable?: boolean;
  /** Only show search when option count exceeds this. */
  searchThreshold?: number;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const ref = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  // Reset query and focus search when dropdown opens
  useEffect(() => {
    if (open) {
      setQuery('');
      // Defer focus until after dropdown renders
      requestAnimationFrame(() => searchRef.current?.focus());
    }
  }, [open]);

  const showSearch = searchable && options.length > searchThreshold;

  const filtered = useMemo(() => {
    if (!query.trim()) return options;
    const q = query.toLowerCase();
    return options.filter(o => o.label.toLowerCase().includes(q));
  }, [options, query]);

  const selected = options.find(o => o.value === value);
  const label = selected?.label ?? placeholder;

  const handleKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') {
      setOpen(false);
    } else if (e.key === 'Enter' && filtered.length > 0) {
      e.preventDefault();
      onChange(filtered[0].value);
      setOpen(false);
    }
  };

  return (
    <div ref={ref} style={{ position: 'relative', display: 'inline-block' }}>
      <button
        type="button"
        className={`multiselect-btn${open ? ' open' : ''}`}
        onClick={() => { if (!disabled) setOpen(o => !o); }}
        disabled={disabled}
      >
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</span>
        <span className="chevron">▾</span>
      </button>
      {open && (
        <div className="multiselect-dropdown">
          {showSearch && (
            <div style={{ padding: '6px 8px', borderBottom: '1px solid var(--border)' }}>
              <input
                ref={searchRef}
                type="text"
                value={query}
                onChange={e => setQuery(e.target.value)}
                onKeyDown={handleKey}
                placeholder="Search…"
                style={{
                  width: '100%',
                  padding: '6px 8px',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--r)',
                  background: 'var(--canvas)',
                  color: 'var(--text)',
                  fontFamily: 'var(--f-ui)',
                  fontSize: 12,
                  outline: 'none',
                }}
              />
            </div>
          )}
          {filtered.length === 0 && (
            <div className="multiselect-empty">
              {options.length === 0 ? 'No options' : 'No matches'}
            </div>
          )}
          {filtered.map(opt => (
            <div
              key={opt.value}
              className={`multiselect-option${value === opt.value ? ' multiselect-option-active' : ''}`}
              onClick={() => { onChange(opt.value); setOpen(false); }}
            >
              <span>{opt.label}</span>
              {value === opt.value && <span style={{ marginLeft: 'auto', color: 'var(--accent)', fontSize: 11 }}>✓</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
