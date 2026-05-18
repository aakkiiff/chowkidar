import { useEffect, useRef } from 'react';

/**
 * LoginScene — animated background for the login page.
 *
 * Theme: chowkidar = guard/watchman. A dark "data vault" filled with hidden
 * infrastructure telemetry that is revealed only by a torch following the
 * cursor. The torch is the user; the data is the system they're about to
 * watch over. All scene markup + styles are scoped to this component.
 *
 * Performance notes:
 *  - One requestAnimationFrame loop drives torch position, mask, trail, and
 *    SVG feDisplacementMap scale.
 *  - feTurbulence is the heaviest cost; baseFrequency tuned low.
 *  - On reduced-motion / mobile the trail SVG is skipped.
 */
export default function LoginScene() {
  const sceneRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const scene = sceneRef.current;
    if (!scene) return;

    const torch = scene.querySelector<HTMLDivElement>('[data-torch]');
    const dataLayer = scene.querySelector<HTMLDivElement>('[data-layer]');
    if (!torch || !dataLayer) return;

    let mx = window.innerWidth * 0.55;
    let my = window.innerHeight * 0.5;
    let tx = mx, ty = my;

    const onMove = (e: MouseEvent) => { mx = e.clientX; my = e.clientY; };
    const onTouch = (e: TouchEvent) => {
      if (e.touches[0]) { mx = e.touches[0].clientX; my = e.touches[0].clientY; }
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('touchmove', onTouch, { passive: true });

    // Throttle to ~30fps. Mouse position updates are visually indistinguishable
    // from 60fps, but GPU compositing the mask + glow every frame is what
    // makes the scene laggy. Halving rAF work doubles available headroom.
    let raf = 0;
    let skip = false;
    const tick = () => {
      skip = !skip;
      if (!skip) {
        tx += (mx - tx) * 0.22;
        ty += (my - ty) * 0.22;
        // translate3d forces a GPU layer; transform-only updates don't trigger layout
        torch.style.transform = `translate3d(${tx}px, ${ty}px, 0)`;
        dataLayer.style.setProperty('--tx', (tx / window.innerWidth * 100) + '%');
        dataLayer.style.setProperty('--ty', (ty / window.innerHeight * 100) + '%');
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);

    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('touchmove', onTouch);
    };
  }, []);

  // Pre-seed sparingly — particles each have a glow which compositor blends
  // per frame. Halving particle counts is the cheapest visual-equivalent win.
  const embers = Array.from({ length: 10 }).map((_, i) => ({
    dx: (Math.random() * 120 - 60).toFixed(0),
    left: (Math.random() * 20 - 10).toFixed(0),
    delay: (Math.random() * 4).toFixed(2),
    duration: (3 + Math.random() * 3).toFixed(2),
    key: i,
  }));
  const dust = Array.from({ length: 20 }).map((_, i) => ({
    left: (Math.random() * 100).toFixed(1),
    top: (Math.random() * 100).toFixed(1),
    ddx: (Math.random() * 120 - 60).toFixed(0),
    ddy: -(80 + Math.random() * 220).toFixed(0),
    duration: (6 + Math.random() * 10).toFixed(2),
    delay: (Math.random() * 8).toFixed(2),
    opacity: (Math.random() * 0.7 + 0.3).toFixed(2),
    key: i,
  }));

  // Smaller mesh — composite cost scales with line count.
  const meshNodes: { x: number; y: number }[] = [];
  for (let i = 0; i < 10; i++) {
    meshNodes.push({ x: Math.random() * 1600, y: Math.random() * 900 });
  }
  const meshLines: [number, number, number, number][] = [];
  for (let i = 0; i < meshNodes.length; i++) {
    for (let j = i + 1; j < meshNodes.length; j++) {
      const a = meshNodes[i], b = meshNodes[j];
      if (Math.hypot(a.x - b.x, a.y - b.y) < 320) {
        meshLines.push([a.x, a.y, b.x, b.y]);
      }
    }
  }

  return (
    <div className="ls-root" ref={sceneRef} aria-hidden="true">
      {/* SVG filter defs — only the inexpensive ones remain.
          Heat distortion (feTurbulence + feDisplacementMap) was the main
          GPU cost; removed. Look stays 95% the same. */}
      <svg width="0" height="0" style={{ position: 'absolute' }}>
        <defs>
          <radialGradient id="ls-core" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#9ee9ff" stopOpacity=".9" />
            <stop offset="60%" stopColor="#33a2e5" stopOpacity=".4" />
            <stop offset="100%" stopColor="#0a1828" stopOpacity="0" />
          </radialGradient>
        </defs>
      </svg>

      <div className="ls-scene">
        <div className="ls-vault">
          <div className="ls-grid ls-grid-floor" />
          <div className="ls-grid ls-grid-ceiling" />
        </div>
        <div className="ls-fog" />

        {/* Hidden data — revealed only inside the torch radius */}
        <div className="ls-data" data-layer>
          <svg className="ls-net" viewBox="0 0 1600 900" preserveAspectRatio="none">
            {meshLines.map(([x1, y1, x2, y2], i) => (
              <line key={i} x1={x1} y1={y1} x2={x2} y2={y2} />
            ))}
            {meshNodes.map((n, i) => (
              <circle key={i} cx={n.x} cy={n.y} r={2.5} fill="#33a2e5" opacity={0.8} />
            ))}
          </svg>

          <div className="ls-panel" style={{ left: '6%', top: '14%', width: 220 }}>
            <h4>AGENT // node-01</h4>
            <div className="ls-metric"><span>CPU</span><b>84%</b></div>
            <div className="ls-bar"><i /></div>
            <div className="ls-metric"><span>MEM</span><b>61%</b></div>
            <div className="ls-bar"><i style={{ animationDelay: '.6s' }} /></div>
            <div className="ls-metric"><span>NET I/O</span><b>2.4 GB/s</b></div>
            <div className="ls-bar"><i style={{ animationDelay: '1.2s' }} /></div>
            <div className="ls-metric"><span>LAST SEEN</span><b>4s</b></div>
          </div>

          <div className="ls-panel" style={{ right: '5%', top: '12%', width: 240 }}>
            <h4>CLUSTER // prod-eks</h4>
            <div className="ls-metric"><span>containers</span><b>412</b></div>
            <div className="ls-metric"><span>agents</span><b>38</b></div>
            <div className="ls-metric"><span>endpoints</span><b>12</b></div>
            <div className="ls-metric"><span>uptime</span><b>99.998%</b></div>
            <div className="ls-bar"><i /></div>
          </div>

          <div className="ls-panel" style={{ left: '6%', top: '50%', width: 220 }}>
            <h4>VAULT // INTEGRITY</h4>
            <div className="ls-metric"><span>shards</span><b>8 / 8</b></div>
            <div className="ls-metric"><span>entropy</span><b>0.9997</b></div>
            <div className="ls-metric"><span>cipher</span><b>AES-512</b></div>
            <div className="ls-bar"><i style={{ animationDelay: '.3s' }} /></div>
          </div>

          <div className="ls-k8s" style={{ left: '10%', bottom: '14%' }}>
            {Array.from({ length: 12 }).map((_, i) => <div key={i} className="ls-node" />)}
          </div>

          <div className="ls-graph" style={{ right: '7%', bottom: '22%' }}>
            <div className="ls-graph-label">THROUGHPUT // 24H</div>
            <svg viewBox="0 0 280 100" preserveAspectRatio="none">
              <polyline className="ls-area" points="0,90 20,70 40,80 60,40 80,55 100,30 140,50 180,20 220,45 260,15 280,30 280,100 0,100" />
              <polyline points="0,90 20,70 40,80 60,40 80,55 100,30 140,50 180,20 220,45 260,15 280,30" />
            </svg>
          </div>

          <div className="ls-code" style={{ left: '32%', top: '8%' }}>
{`const agent = await chowkidar.watch();
for (const tick of agent.stream()) {
  if (tick.cpu > 0.85) alert(tick);
  emit('signal', tick.hash);
}
> agent agt_a17c..21d online
> handshake: OK
> tunnel: ESTABLISHED
> route: eu-west-7 ✓
> 412 containers reporting
> cipher: AES-512-GCM
const agent = await chowkidar.watch();
for (const tick of agent.stream()) {
  if (tick.cpu > 0.85) alert(tick);
  emit('signal', tick.hash);
}`}
          </div>

          <div className="ls-code" style={{ right: '24%', top: '55%', color: '#33a2e5', textShadow: '0 0 8px #33a2e5' }}>
{`[chowkidar] scanning region 7G
[chowkidar] anomaly detected
[chowkidar] decrypting payload...
[chowkidar] payload size: 4.2 PB
[chowkidar] integrity: 99.998%
[chowkidar] awakening protocol...
[chowkidar] hidden index found
[chowkidar] scanning region 7G`}
          </div>

          <div className="ls-runes" style={{ left: '30%', top: '48%' }}>ᚠᛇᛞ ᛒᚲᚺ ᛗᛃᚹ</div>
          <div className="ls-runes" style={{ right: '18%', top: '42%', color: '#33a2e5', textShadow: '0 0 10px #33a2e5' }}>◇△⌬⚙︎⌖</div>
          <div className="ls-runes" style={{ left: '50%', bottom: '14%' }}>⟁ ⟟ ⟒ ⌬ ⏃</div>

          <div className="ls-mega">
            <svg viewBox="0 0 900 600" width="100%" height="100%">
              <circle cx="450" cy="300" r="260" fill="url(#ls-core)" />
              <g fill="none" stroke="#33a2e5" strokeWidth="1" opacity=".8">
                <circle cx="450" cy="300" r="120" />
                <circle cx="450" cy="300" r="170" />
                <circle cx="450" cy="300" r="220" />
                <circle cx="450" cy="300" r="270" />
              </g>
              <g stroke="#33a2e5" strokeWidth="1" opacity=".6">
                <line x1="450" y1="30" x2="450" y2="570" />
                <line x1="180" y1="300" x2="720" y2="300" />
                <line x1="260" y1="110" x2="640" y2="490" />
                <line x1="640" y1="110" x2="260" y2="490" />
              </g>
              <g fill="#ffb347">
                <circle cx="450" cy="180" r="4" />
                <circle cx="570" cy="300" r="4" />
                <circle cx="450" cy="420" r="4" />
                <circle cx="330" cy="300" r="4" />
              </g>
              <text x="450" y="305" textAnchor="middle" fill="#fff6c2"
                fontFamily="JetBrains Mono, monospace" fontSize="14" letterSpacing="4"
                style={{ filter: 'drop-shadow(0 0 10px #ffae3c)' }}>CORE.WATCH</text>
            </svg>
          </div>
        </div>

        {/* Torch (follows cursor) */}
        <div className="ls-torch" data-torch>
          <div className="ls-torch-light" />
          <div className="ls-flame">
            <div className="ls-flame-l1" />
            <div className="ls-flame-l2" />
            <div className="ls-flame-l3" />
          </div>
          <div className="ls-embers">
            {embers.map(e => (
              <span key={e.key} className="ls-ember" style={{
                ['--dx' as string]: e.dx + 'px',
                left: e.left + 'px',
                animationDelay: e.delay + 's',
                animationDuration: e.duration + 's',
              }} />
            ))}
          </div>
        </div>

        <div className="ls-dust">
          {dust.map(d => (
            <span key={d.key} style={{
              left: d.left + '%',
              top: d.top + '%',
              ['--ddx' as string]: d.ddx + 'px',
              ['--ddy' as string]: d.ddy + 'px',
              animationDuration: d.duration + 's',
              animationDelay: d.delay + 's',
              opacity: Number(d.opacity),
            }} />
          ))}
        </div>

        <div className="ls-vignette" />
        <div className="ls-grain" />

        <div className="ls-tagline">
          <b>C H O W K I D A R</b>
        </div>
      </div>

      <style>{loginSceneCSS}</style>
    </div>
  );
}

// Scoped CSS for the scene. Kept inline to avoid polluting the global
// stylesheet — the login page is the only place these classes are used.
const loginSceneCSS = `
.ls-root, .ls-root * { box-sizing: border-box; }
.ls-root {
  position: fixed; inset: 0; z-index: 0; pointer-events: none;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  color: #9ccfff;
}
.ls-scene {
  position: absolute; inset: 0;
  background:
    radial-gradient(ellipse at 50% 60%, #06121f 0%, #02060d 45%, #000 100%),
    #000;
  perspective: 1400px; overflow: hidden;
}
.ls-vault {
  position: absolute; inset: 0;
  transform-style: preserve-3d;
  animation: ls-cam 22s ease-in-out infinite;
}
@keyframes ls-cam {
  0%, 100% { transform: translateZ(0) translateY(0); }
  50%      { transform: translateZ(420px) translateY(-10px); }
}
.ls-grid {
  position: absolute; left: -50%; width: 200%; height: 160%;
  background-image:
    linear-gradient(rgba(51, 162, 229, 0.18) 1px, transparent 1px),
    linear-gradient(90deg, rgba(51, 162, 229, 0.12) 1px, transparent 1px);
  background-size: 80px 80px;
  transform-origin: 50% 0;
}
.ls-grid-floor {
  bottom: -40%; transform: rotateX(75deg);
  -webkit-mask-image: radial-gradient(ellipse at 50% 0%, #000 0%, transparent 70%);
          mask-image: radial-gradient(ellipse at 50% 0%, #000 0%, transparent 70%);
}
.ls-grid-ceiling {
  top: -40%; transform: rotateX(-75deg); opacity: .6;
  -webkit-mask-image: radial-gradient(ellipse at 50% 100%, #000 0%, transparent 70%);
          mask-image: radial-gradient(ellipse at 50% 100%, #000 0%, transparent 70%);
}
.ls-fog {
  position: absolute; inset: 0;
  background:
    radial-gradient(circle at 30% 40%, rgba(20, 40, 80, .25), transparent 50%),
    radial-gradient(circle at 70% 60%, rgba(10, 20, 40, .4), transparent 60%);
  animation: ls-fog 30s linear infinite;
}
@keyframes ls-fog {
  0%, 100% { transform: translate(0, 0); }
  50%      { transform: translate(-40px, 20px); }
}

/* HIDDEN DATA: shown only inside the torch mask. No heat distortion filter —
   feTurbulence/feDisplacementMap is the most expensive operation on the page
   and we don't need it for the effect. */
.ls-data {
  position: absolute; inset: 0;
  -webkit-mask-image: radial-gradient(circle at var(--tx, 50%) var(--ty, 55%),
    #000 0%, rgba(0,0,0,.85) 14%, rgba(0,0,0,.35) 26%, transparent 38%);
          mask-image: radial-gradient(circle at var(--tx, 50%) var(--ty, 55%),
    #000 0%, rgba(0,0,0,.85) 14%, rgba(0,0,0,.35) 26%, transparent 38%);
  will-change: mask-position;
}

.ls-panel {
  position: absolute;
  border: 1px solid rgba(51, 162, 229, .6);
  background: linear-gradient(180deg, rgba(20, 60, 120, .25), rgba(5, 15, 30, .55));
  box-shadow: 0 0 30px rgba(51, 162, 229, .25), inset 0 0 25px rgba(51, 162, 229, .15);
  padding: 10px 12px;
  font-size: 11px; color: #9ee9ff;
  border-radius: 2px;
  opacity: .95;
}
.ls-panel::before {
  content: ''; position: absolute; left: 0; top: 0; width: 6px; height: 100%;
  background: linear-gradient(180deg, #33a2e5, #1f6feb);
  box-shadow: 0 0 10px #33a2e5;
}
.ls-panel h4 {
  font-size: 10px; letter-spacing: 2px;
  color: #33a2e5; margin-bottom: 6px; text-transform: uppercase;
}
.ls-bar {
  height: 4px; background: rgba(51, 162, 229, .15);
  margin: 3px 0; position: relative; overflow: hidden;
}
.ls-bar i {
  position: absolute; left: 0; top: 0; height: 100%;
  background: linear-gradient(90deg, #73bf69, #33a2e5);
  animation: ls-bar 2.6s ease-in-out infinite;
}
@keyframes ls-bar { 0%, 100% { width: 30%; } 50% { width: 88%; } }
.ls-metric { display: flex; justify-content: space-between; color: #ffd28a; }
.ls-metric b { color: #73bf69; }

.ls-net { position: absolute; inset: 0; pointer-events: none; }
.ls-net line { stroke: rgba(51, 162, 229, .25); stroke-width: 1; }
.ls-pulse { fill: #33a2e5; filter: drop-shadow(0 0 6px #33a2e5); }

.ls-code {
  position: absolute;
  font-size: 11px; line-height: 1.4; color: #73bf69;
  text-shadow: 0 0 8px rgba(115, 191, 105, .7);
  white-space: pre; opacity: .85;
  animation: ls-code 14s linear infinite;
}
@keyframes ls-code { from { transform: translateY(0); } to { transform: translateY(-50%); } }

.ls-k8s { position: absolute; display: grid; grid-template-columns: repeat(4, 28px); gap: 8px; }
.ls-node {
  width: 28px; height: 28px;
  border: 1px solid rgba(51, 162, 229, .7);
  background: radial-gradient(circle, rgba(51, 162, 229, .35), rgba(20, 60, 120, .1));
  position: relative;
}
.ls-node::after {
  content: ''; position: absolute; inset: 6px;
  border: 1px solid rgba(255, 255, 255, .4); transform: rotate(45deg);
}
.ls-node:nth-child(odd) { animation-delay: 1.2s; }
@keyframes ls-node {
  0%, 100% { box-shadow: 0 0 8px rgba(51, 162, 229, .4); }
  50%      { box-shadow: 0 0 22px rgba(51, 162, 229, .9), 0 0 40px rgba(51, 162, 229, .4); }
}

.ls-runes {
  position: absolute; font-family: 'Times New Roman', serif;
  font-size: 26px; letter-spacing: 6px; color: #ffb347;
  text-shadow: 0 0 10px rgba(255, 140, 40, .7), 0 0 22px rgba(255, 90, 30, .4);
  opacity: .85;
  animation: ls-rune 3.4s ease-in-out infinite;
}
@keyframes ls-rune { 0%, 100% { opacity: .5; } 50% { opacity: 1; } }

.ls-graph {
  position: absolute; width: 280px; height: 120px;
  border: 1px solid rgba(51, 162, 229, .5);
  background: rgba(5, 15, 30, .5); padding: 8px;
}
.ls-graph svg { width: 100%; height: 100%; }
.ls-graph polyline { fill: none; stroke: #73bf69; stroke-width: 2; filter: drop-shadow(0 0 4px #73bf69); }
.ls-graph .ls-area { fill: rgba(115, 191, 105, .15); stroke: none; }
.ls-graph-label {
  position: absolute; top: 4px; right: 8px;
  font-size: 9px; letter-spacing: 2px; color: #33a2e5;
}

.ls-mega {
  position: absolute; left: 50%; top: 50%;
  transform: translate(-50%, -50%) scale(.4);
  width: 900px; height: 600px; opacity: 0;
  animation: ls-mega 22s ease-in-out infinite;
}
@keyframes ls-mega {
  0%, 55% { opacity: 0; transform: translate(-50%, -50%) scale(.3); }
  75%     { opacity: .95; transform: translate(-50%, -50%) scale(.7); }
  90%     { opacity: .7; transform: translate(-50%, -50%) scale(.95); }
  100%    { opacity: 0; transform: translate(-50%, -50%) scale(1.1); }
}

/* TORCH — translate3d on .ls-torch keeps it on a GPU layer.
   Removed mix-blend-mode + filter:blur from the light (compositor heavy).
   Flicker switched from per-frame steps() to slower opacity animation. */
.ls-torch {
  position: fixed; left: 0; top: 0; width: 0; height: 0;
  z-index: 4; pointer-events: none;
  will-change: transform; transform: translate3d(0, 0, 0);
}
.ls-torch-light {
  position: absolute; left: -500px; top: -500px; width: 1000px; height: 1000px;
  background: radial-gradient(circle,
    rgba(255, 200, 90, .65) 0%,
    rgba(255, 140, 40, .32) 16%,
    rgba(255, 90, 20, .12) 36%,
    transparent 62%);
  opacity: .92;
  animation: ls-flicker 1s ease-in-out infinite;
}
@keyframes ls-flicker {
  0%, 100% { opacity: .88; }
  50%      { opacity: 1; }
}
.ls-flame { position: absolute; left: -26px; top: -78px; width: 52px; height: 90px; pointer-events: none; }
.ls-flame > div {
  position: absolute; left: 50%; bottom: 0; transform: translateX(-50%);
  border-radius: 50% 50% 35% 35% / 60% 60% 40% 40%;
  mix-blend-mode: screen; filter: blur(1px);
  animation: ls-fdance 0.4s ease-in-out infinite alternate;
}
.ls-flame-l1 { width: 50px; height: 86px; background: radial-gradient(circle at 50% 80%, #ff5a1f, rgba(255,90,30,0) 70%); }
.ls-flame-l2 { width: 34px; height: 68px; background: radial-gradient(circle at 50% 80%, #ffae3c, rgba(255,174,60,0) 70%); animation-duration: .3s !important; }
.ls-flame-l3 { width: 20px; height: 48px; background: radial-gradient(circle at 50% 80%, #fff6c2, rgba(255,246,194,0) 70%); animation-duration: .22s !important; }
@keyframes ls-fdance {
  0%   { transform: translateX(-50%) scaleY(1)    skewX(-2deg); }
  100% { transform: translateX(-50%) scaleY(1.08) skewX(3deg); }
}
.ls-embers { position: absolute; left: 0; top: -80px; width: 0; height: 0; }
.ls-ember {
  position: absolute; width: 3px; height: 3px; border-radius: 50%;
  background: #ffb347;
  box-shadow: 0 0 8px #ff8a2c, 0 0 14px #ff5a1f;
  animation: ls-ember 4s ease-out infinite; opacity: 0;
}
@keyframes ls-ember {
  0%   { transform: translate(0, 0)         scale(1);   opacity: 1; }
  100% { transform: translate(var(--dx, 20px), -260px) scale(.2); opacity: 0; }
}

/* Trail SVG removed entirely — see comment in component. */

.ls-dust { position: absolute; inset: 0; pointer-events: none; z-index: 5; }
.ls-dust span {
  position: absolute; width: 2px; height: 2px; border-radius: 50%;
  background: rgba(255, 210, 150, .7);
  box-shadow: 0 0 4px rgba(255, 180, 90, .6);
  animation: ls-dust linear infinite;
}
@keyframes ls-dust {
  0%   { transform: translate(0, 0); opacity: 0; }
  20%  { opacity: .9; }
  100% { transform: translate(var(--ddx, -40px), var(--ddy, -200px)); opacity: 0; }
}

.ls-vignette {
  position: absolute; inset: 0; pointer-events: none;
  background: radial-gradient(ellipse at center, transparent 30%, rgba(0,0,0,.75) 75%, #000 100%);
  z-index: 6;
}
.ls-grain {
  position: absolute; inset: 0; pointer-events: none; z-index: 7; opacity: .07;
  background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='160' height='160'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2'/></filter><rect width='100%25' height='100%25' filter='url(%23n)' opacity='0.6'/></svg>");
  mix-blend-mode: overlay;
}

.ls-tagline {
  position: absolute; left: 50%; bottom: 28px; transform: translateX(-50%);
  z-index: 8; text-align: center; letter-spacing: 6px;
  color: rgba(255, 180, 90, .7);
  font-size: 11px; text-transform: uppercase;
  text-shadow: 0 0 12px rgba(255, 120, 40, .5);
}
.ls-tagline b { display: block; font-size: 18px; color: #ffd28a; letter-spacing: 14px; margin-bottom: 6px; }

@media (prefers-reduced-motion: reduce) {
  .ls-vault, .ls-fog, .ls-mega, .ls-code, .ls-flame > div, .ls-torch-light,
  .ls-bar i, .ls-node, .ls-runes { animation: none !important; }
}
`;
