import { useState, useEffect, useRef } from 'react';

export default function SingleSelect({ options, value, onChange, placeholder = 'Select…', disabled = false }: {
  options: { label: string; value: string }[];
  value: string | null;
  onChange: (v: string) => void;
  placeholder?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const selected = options.find(o => o.value === value);
  const label = selected?.label ?? placeholder;

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
          {options.length === 0 && (
            <div className="multiselect-empty">No options</div>
          )}
          {options.map(opt => (
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
