<script setup lang="ts">
import { ref } from "vue";

const copied = ref("");
function copy(text: string, id: string) {
  navigator.clipboard?.writeText(text).then(() => {
    copied.value = id;
    setTimeout(() => (copied.value = ""), 1600);
  });
}

const cloudCmd = "./installer/install.sh --mode cloud --enrollment-token <your-key>";
const selfCmd = "git clone https://github.com/frix-me/pulse.git && cd pulse && ./installer/install.sh";

const features = [
  { k: "01", t: "Auto-discovery", d: "Detects containers, reverse proxies, databases, processes, ports, TLS and domains — read-only, in seconds." },
  { k: "02", t: "Non-destructive", d: "Observe first, change nothing by default. SAFE MODE never touches your services, config, ports or firewall." },
  { k: "03", t: "Live metrics", d: "CPU, memory, disk, network and per-container stats stream in real time — no full-page reloads." },
  { k: "04", t: "Service topology", d: "A graph built from real discovery data — Nginx upstreams, Docker networks, dependencies. Never invented." },
  { k: "05", t: "Smart alerts", d: "Debounced, deduplicated and dependency-aware — root causes surfaced over symptom storms." },
  { k: "06", t: "Security view", d: "Flags exposed DB ports, weak/expired TLS and Docker exposure. Reports — never auto-changes." },
];
</script>

<template>
  <div class="lp">
    <div class="lp-grain" aria-hidden="true"></div>
    <div class="lp-grid" aria-hidden="true"></div>
    <div class="lp-glow lp-glow-a" aria-hidden="true"></div>
    <div class="lp-glow lp-glow-b" aria-hidden="true"></div>

    <!-- Nav -->
    <header class="nav">
      <div class="nav-in shell">
        <a class="brand" href="#top">
          <span class="brand-dot"></span> Pulse
        </a>
        <nav class="nav-links">
          <a href="#features">Features</a>
          <a href="#modes">Modes</a>
          <a href="#start">Get started</a>
        </nav>
        <RouterLink to="/login" class="btn btn-ghost-glass">Sign in</RouterLink>
      </div>
    </header>

    <main id="top" class="wrap">
      <!-- Hero -->
      <section class="hero">
        <span class="eyebrow">// non-destructive vps observability</span>
        <h1 class="h1">
          Observe everything.<br /><span class="grad">Change nothing.</span>
        </h1>
        <p class="lede">
          Clone Pulse onto any VPS and instantly understand everything running on it —
          services, containers, databases, domains and traffic — monitored continuously
          without disrupting a thing.
        </p>

        <div class="cta-row">
          <a href="#start" class="btn btn-lime">Deploy on your VPS</a>
          <a href="/api/v1/auth/google/start" class="btn btn-glass">Continue with Google</a>
          <a href="/api/v1/auth/telegram/start" class="btn btn-glass">Continue with Telegram</a>
        </div>

        <button class="cmd" :class="{ ok: copied === 'hero' }" @click="copy('curl -fsSL https://install.frix.me/install.sh | bash', 'hero')">
          <span class="cmd-prompt">$</span>
          <span class="cmd-text">curl -fsSL https://install.frix.me/install.sh | bash</span>
          <span class="cmd-copy">{{ copied === 'hero' ? 'copied' : 'copy' }}</span>
        </button>
      </section>

      <!-- Floating shell preview -->
      <section class="float-shell">
        <div class="shell-bar">
          <span class="dot r"></span><span class="dot y"></span><span class="dot g"></span>
          <span class="shell-title">pulse.frix.me · VPS-01</span>
          <span class="shell-live"><i></i> live</span>
        </div>
        <div class="shell-body">
          <div class="stat">
            <div class="stat-k">VPS HEALTH</div>
            <div class="badge">Healthy</div>
          </div>
          <div class="stat">
            <div class="stat-k">CPU</div>
            <div class="stat-v">42<small>%</small></div>
            <div class="bar"><i style="width:42%"></i></div>
          </div>
          <div class="stat">
            <div class="stat-k">MEMORY</div>
            <div class="stat-v">61<small>%</small></div>
            <div class="bar"><i style="width:61%"></i></div>
          </div>
          <div class="stat">
            <div class="stat-k">DISK</div>
            <div class="stat-v">72<small>%</small></div>
            <div class="bar"><i class="warn" style="width:72%"></i></div>
          </div>
          <div class="stat wide">
            <div class="stat-k">NETWORK · last hour</div>
            <div class="spark">
              <span v-for="(h, i) in [30,44,38,52,49,63,58,71,66,80,74,61,69,77,64]" :key="i" :style="{ height: h + '%' }"></span>
            </div>
          </div>
          <div class="stat">
            <div class="stat-k">SERVICES</div>
            <div class="mini"><b class="ok">17</b> healthy · <b class="warn">2</b> degraded · <b class="down">1</b> down</div>
            <div class="stat-k mt">CONTAINERS</div>
            <div class="mini"><b class="ok">21</b> running · <b class="down">1</b> unhealthy</div>
          </div>
        </div>
      </section>

      <!-- Features -->
      <section id="features" class="section">
        <h2 class="h2">Everything, discovered — then watched</h2>
        <div class="grid">
          <article v-for="f in features" :key="f.k" class="card">
            <span class="card-k">{{ f.k }}</span>
            <h3>{{ f.t }}</h3>
            <p>{{ f.d }}</p>
          </article>
        </div>
      </section>

      <!-- Two modes -->
      <section id="modes" class="section">
        <h2 class="h2">Two ways to run it</h2>
        <div class="modes">
          <article class="mode">
            <div class="mode-tag">Self-hosted</div>
            <h3>Your box, your domain</h3>
            <p>Clone the repo, run one command, pick your domain and a username/password. The dashboard lives entirely on your VPS — nothing leaves the machine.</p>
            <button class="cmd sm" :class="{ ok: copied === 'self' }" @click="copy(selfCmd, 'self')">
              <span class="cmd-prompt">$</span><span class="cmd-text">{{ selfCmd }}</span>
              <span class="cmd-copy">{{ copied === 'self' ? 'copied' : 'copy' }}</span>
            </button>
          </article>
          <article class="mode featured">
            <div class="mode-tag lime">Pulse Cloud</div>
            <h3>Sign in, drop in a key</h3>
            <p>Sign in with Google or Telegram, generate a tracking key, run the agent on your VPS with that key. Your servers and their metrics appear in your central dashboard here — the agent dials out, no inbound port.</p>
            <button class="cmd sm" :class="{ ok: copied === 'cloud' }" @click="copy(cloudCmd, 'cloud')">
              <span class="cmd-prompt">$</span><span class="cmd-text">{{ cloudCmd }}</span>
              <span class="cmd-copy">{{ copied === 'cloud' ? 'copied' : 'copy' }}</span>
            </button>
          </article>
        </div>
      </section>

      <!-- How it works -->
      <section id="start" class="section">
        <h2 class="h2">Live in three steps</h2>
        <div class="steps">
          <div class="step"><span class="step-n">1</span><h3>Clone</h3><p>Pull Pulse onto the VPS you want to watch.</p></div>
          <div class="step"><span class="step-n">2</span><h3>Key</h3><p>Sign in here, generate a tracking key, run the installer with it.</p></div>
          <div class="step"><span class="step-n">3</span><h3>Watch</h3><p>Discovery runs and your infrastructure appears — read-only, safe.</p></div>
        </div>
        <div class="start-cta">
          <RouterLink to="/login" class="btn btn-lime">Sign in to get your key</RouterLink>
          <a href="https://github.com/frix-me/pulse" class="btn btn-glass">View on GitHub</a>
        </div>
      </section>
    </main>

    <footer class="foot">
      <div class="shell foot-in">
        <span class="brand"><span class="brand-dot"></span> Pulse</span>
        <span class="foot-note">Observe first · Change nothing by default · GPL-3.0</span>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.lp {
  --ink: #06070a;
  --ink2: #0a0c11;
  --lime: #c7f542;
  --text: #eaf0f7;
  --muted: #8b97a9;
  --glass: rgba(255, 255, 255, 0.045);
  --glass-2: rgba(255, 255, 255, 0.07);
  --line: rgba(255, 255, 255, 0.09);
  position: relative;
  min-height: 100vh;
  background: var(--ink);
  color: var(--text);
  font-family: "JetBrains Mono", ui-monospace, monospace;
  overflow-x: hidden;
}
/* atmosphere */
.lp-grid {
  position: fixed; inset: 0; pointer-events: none; z-index: 0;
  background-image:
    linear-gradient(rgba(255,255,255,0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255,255,255,0.04) 1px, transparent 1px);
  background-size: 64px 64px;
  mask-image: radial-gradient(ellipse 90% 60% at 50% 0%, #000 40%, transparent 100%);
}
.lp-glow { position: fixed; border-radius: 50%; filter: blur(120px); opacity: 0.5; pointer-events: none; z-index: 0; }
.lp-glow-a { width: 560px; height: 560px; top: -160px; left: -120px; background: radial-gradient(circle, rgba(199,245,66,0.28), transparent 70%); }
.lp-glow-b { width: 620px; height: 620px; top: 240px; right: -200px; background: radial-gradient(circle, rgba(56,189,248,0.16), transparent 70%); }
.lp-grain {
  position: fixed; inset: 0; z-index: 1; pointer-events: none; opacity: 0.05; mix-blend-mode: overlay;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}
.shell { max-width: 1120px; margin: 0 auto; padding: 0 24px; }
.wrap { position: relative; z-index: 2; max-width: 1120px; margin: 0 auto; padding: 0 24px; }

/* nav */
.nav { position: sticky; top: 0; z-index: 20; backdrop-filter: blur(14px); background: rgba(6,7,10,0.6); border-bottom: 1px solid var(--line); }
.nav-in { display: flex; align-items: center; gap: 20px; height: 64px; }
.brand { font-family: "Space Grotesk", sans-serif; font-weight: 700; font-size: 18px; letter-spacing: -0.02em; color: var(--text); text-decoration: none; display: inline-flex; align-items: center; gap: 9px; }
.brand-dot { width: 9px; height: 9px; border-radius: 50%; background: var(--lime); box-shadow: 0 0 12px var(--lime); }
.nav-links { margin-left: auto; display: flex; gap: 26px; font-size: 13px; }
.nav-links a { color: var(--muted); text-decoration: none; transition: color 0.15s; }
.nav-links a:hover { color: var(--text); }

/* buttons */
.btn { display: inline-flex; align-items: center; justify-content: center; gap: 8px; padding: 11px 18px; border-radius: 10px; font-size: 13px; font-weight: 500; text-decoration: none; cursor: pointer; transition: transform 0.12s, background 0.15s, border-color 0.15s; white-space: nowrap; }
.btn:hover { transform: translateY(-1px); }
.btn-lime { background: var(--lime); color: #0a0c05; font-weight: 700; box-shadow: 0 0 0 1px rgba(199,245,66,0.5), 0 8px 30px rgba(199,245,66,0.18); }
.btn-glass { background: var(--glass); border: 1px solid var(--line); color: var(--text); backdrop-filter: blur(8px); }
.btn-glass:hover { background: var(--glass-2); }
.btn-ghost-glass { background: transparent; border: 1px solid var(--line); color: var(--text); padding: 9px 16px; border-radius: 10px; font-size: 13px; text-decoration: none; }
.btn-ghost-glass:hover { background: var(--glass); }

/* hero */
.hero { padding: 92px 0 40px; max-width: 820px; }
.eyebrow { font-size: 12px; color: var(--lime); letter-spacing: 0.06em; }
.h1 { font-family: "Space Grotesk", sans-serif; font-weight: 700; font-size: clamp(40px, 7vw, 78px); line-height: 1.02; letter-spacing: -0.03em; margin: 18px 0 0; }
.grad { background: linear-gradient(100deg, var(--lime), #eaffb0 60%, var(--lime)); -webkit-background-clip: text; background-clip: text; color: transparent; }
.lede { color: var(--muted); font-size: 16px; line-height: 1.7; margin: 22px 0 30px; max-width: 620px; }
.cta-row { display: flex; flex-wrap: wrap; gap: 12px; margin-bottom: 26px; }
.cmd { display: inline-flex; align-items: center; gap: 12px; width: 100%; max-width: 560px; padding: 13px 15px; border-radius: 12px; background: var(--glass); border: 1px solid var(--line); font-family: "JetBrains Mono", monospace; font-size: 13px; color: var(--text); cursor: pointer; text-align: left; backdrop-filter: blur(8px); transition: border-color 0.15s; }
.cmd:hover { border-color: rgba(199,245,66,0.5); }
.cmd.ok { border-color: var(--lime); }
.cmd-prompt { color: var(--lime); }
.cmd-text { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cmd-copy { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; }
.cmd.sm { max-width: 100%; font-size: 12px; }

/* floating shell */
.float-shell { position: relative; z-index: 2; margin: 30px 0 20px; border-radius: 18px; border: 1px solid var(--line); background: linear-gradient(180deg, rgba(255,255,255,0.06), rgba(255,255,255,0.02)); backdrop-filter: blur(18px); box-shadow: 0 40px 120px rgba(0,0,0,0.6), inset 0 1px 0 rgba(255,255,255,0.08); overflow: hidden; }
.shell-bar { display: flex; align-items: center; gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--line); background: rgba(255,255,255,0.02); }
.dot { width: 11px; height: 11px; border-radius: 50%; }
.dot.r { background: #ff5f57; } .dot.y { background: #febc2e; } .dot.g { background: #28c840; }
.shell-title { margin-left: 10px; font-size: 12px; color: var(--muted); }
.shell-live { margin-left: auto; font-size: 11px; color: var(--lime); display: inline-flex; align-items: center; gap: 6px; }
.shell-live i { width: 7px; height: 7px; border-radius: 50%; background: var(--lime); box-shadow: 0 0 10px var(--lime); animation: pulse 1.6s infinite; }
@keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.35; } }
.shell-body { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; padding: 18px; }
.stat { background: var(--glass); border: 1px solid var(--line); border-radius: 12px; padding: 14px; }
.stat.wide { grid-column: span 2; }
.stat-k { font-size: 10px; letter-spacing: 0.08em; color: var(--muted); text-transform: uppercase; }
.stat-k.mt { margin-top: 12px; }
.stat-v { font-family: "Space Grotesk", sans-serif; font-size: 30px; font-weight: 600; margin-top: 4px; }
.stat-v small { font-size: 15px; color: var(--muted); }
.badge { display: inline-block; margin-top: 8px; padding: 3px 10px; border-radius: 999px; font-size: 12px; color: var(--lime); background: rgba(199,245,66,0.12); border: 1px solid rgba(199,245,66,0.3); }
.bar { height: 6px; border-radius: 4px; background: rgba(255,255,255,0.08); margin-top: 10px; overflow: hidden; }
.bar i { display: block; height: 100%; background: var(--lime); box-shadow: 0 0 10px rgba(199,245,66,0.6); }
.bar i.warn { background: #febc2e; box-shadow: 0 0 10px rgba(254,188,46,0.5); }
.spark { display: flex; align-items: flex-end; gap: 5px; height: 66px; margin-top: 12px; }
.spark span { flex: 1; background: linear-gradient(180deg, var(--lime), rgba(199,245,66,0.25)); border-radius: 3px 3px 0 0; opacity: 0.85; }
.mini { font-size: 12px; color: var(--muted); margin-top: 6px; }
.mini b.ok { color: var(--lime); } .mini b.warn { color: #febc2e; } .mini b.down { color: #ff6b6b; }

/* sections */
.section { padding: 64px 0; position: relative; z-index: 2; }
.h2 { font-family: "Space Grotesk", sans-serif; font-weight: 600; font-size: clamp(26px, 4vw, 40px); letter-spacing: -0.02em; margin-bottom: 34px; }
.grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; }
.card { padding: 22px; border-radius: 14px; background: var(--glass); border: 1px solid var(--line); backdrop-filter: blur(10px); transition: transform 0.15s, border-color 0.15s; }
.card:hover { transform: translateY(-3px); border-color: rgba(199,245,66,0.35); }
.card-k { font-size: 12px; color: var(--lime); }
.card h3 { font-family: "Space Grotesk", sans-serif; font-size: 18px; font-weight: 600; margin: 10px 0 8px; }
.card p { font-size: 13px; line-height: 1.65; color: var(--muted); }

/* modes */
.modes { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.mode { padding: 26px; border-radius: 16px; background: var(--glass); border: 1px solid var(--line); backdrop-filter: blur(10px); }
.mode.featured { border-color: rgba(199,245,66,0.4); box-shadow: 0 0 0 1px rgba(199,245,66,0.15), 0 30px 80px rgba(199,245,66,0.06); }
.mode-tag { display: inline-block; font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); border: 1px solid var(--line); border-radius: 999px; padding: 4px 12px; }
.mode-tag.lime { color: var(--lime); border-color: rgba(199,245,66,0.4); background: rgba(199,245,66,0.08); }
.mode h3 { font-family: "Space Grotesk", sans-serif; font-size: 22px; font-weight: 600; margin: 14px 0 10px; }
.mode p { font-size: 14px; line-height: 1.7; color: var(--muted); margin-bottom: 16px; }

/* steps */
.steps { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 30px; }
.step { padding: 24px; border-radius: 14px; background: var(--glass); border: 1px solid var(--line); }
.step-n { display: inline-grid; place-items: center; width: 34px; height: 34px; border-radius: 10px; background: rgba(199,245,66,0.12); border: 1px solid rgba(199,245,66,0.35); color: var(--lime); font-family: "Space Grotesk", sans-serif; font-weight: 700; }
.step h3 { font-family: "Space Grotesk", sans-serif; font-size: 18px; margin: 12px 0 6px; }
.step p { font-size: 13px; color: var(--muted); line-height: 1.6; }
.start-cta { display: flex; flex-wrap: wrap; gap: 12px; }

/* footer */
.foot { position: relative; z-index: 2; border-top: 1px solid var(--line); margin-top: 40px; padding: 26px 0; }
.foot-in { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.foot-note { font-size: 12px; color: var(--muted); }

@media (max-width: 900px) {
  .shell-body { grid-template-columns: repeat(2, 1fr); }
  .grid, .modes, .steps { grid-template-columns: 1fr; }
  .nav-links { display: none; }
}
</style>
