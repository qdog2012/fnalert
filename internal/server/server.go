package server

import (
	"encoding/json"
	"net/http"
	"os"

	"fnmonitor/internal/config"
	"fnmonitor/internal/monitor"
)

func New(cfg config.ServerConfig, mon *monitor.Monitor, testResultPath string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(settingsHTML))
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, mon.State())
	})
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, mon.State().Snapshot)
	})
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, mon.State().Logs)
	})
	mux.HandleFunc("/api/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, mon.State().Alerts)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		state := mon.State()
		writeJSON(w, map[string]any{
			"ok":         state.LastCollectionError == "",
			"updated_at": state.UpdatedAt,
			"alerts":     len(state.Alerts),
		})
	})
	mux.HandleFunc("/api/test-notification", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, mon.SendTest(r.Context()))
	})
	mux.HandleFunc("/api/last-notification-test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if testResultPath == "" {
			writeJSON(w, map[string]any{"available": false})
			return
		}
		data, err := os.ReadFile(testResultPath)
		if err != nil {
			writeJSON(w, map[string]any{"available": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"available": true, "content": string(data)})
	})

	return &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout.Duration,
		WriteTimeout: cfg.WriteTimeout.Duration,
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>fnmonitor</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f5f7fb;
      --panel: #ffffff;
      --text: #152033;
      --muted: #617089;
      --line: #dfe5ee;
      --ok: #147d64;
      --warn: #a15c00;
      --bad: #bd2c2c;
      --info: #2764b7;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-width: 320px;
      background: var(--bg);
      color: var(--text);
      font: 14px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: 0;
    }
    header {
      position: sticky;
      top: 0;
      z-index: 2;
      display: flex;
      gap: 16px;
      align-items: center;
      justify-content: space-between;
      padding: 14px clamp(16px, 3vw, 28px);
      border-bottom: 1px solid var(--line);
      background: rgba(255,255,255,.96);
      backdrop-filter: blur(10px);
    }
    h1 {
      margin: 0;
      font-size: 18px;
      line-height: 1.2;
    }
    main {
      width: min(1480px, 100%);
      margin: 0 auto;
      padding: 18px clamp(12px, 2vw, 28px) 40px;
    }
    button {
      min-height: 34px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--text);
      padding: 6px 10px;
      cursor: pointer;
      white-space: nowrap;
    }
    button:hover { border-color: #b9c5d6; }
    a.buttonLink {
      display: inline-flex;
      min-height: 34px;
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--text);
      padding: 6px 10px;
      text-decoration: none;
      white-space: nowrap;
    }
    a.buttonLink:hover { border-color: #b9c5d6; }
    .toolbar {
      display: flex;
      gap: 10px;
      align-items: center;
      flex-wrap: wrap;
      color: var(--muted);
      font-size: 13px;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(12, 1fr);
      gap: 14px;
    }
    .panel {
      grid-column: span 12;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 14px;
      min-width: 0;
    }
    .panel h2 {
      margin: 0 0 12px;
      font-size: 15px;
      line-height: 1.3;
    }
    .stats {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
      gap: 10px;
    }
    .stat {
      min-height: 84px;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px;
      background: #fbfcfe;
    }
    .label {
      color: var(--muted);
      font-size: 12px;
      overflow-wrap: anywhere;
    }
    .value {
      margin-top: 6px;
      font-size: 24px;
      font-weight: 700;
      line-height: 1.15;
      overflow-wrap: anywhere;
    }
    .sub {
      margin-top: 4px;
      color: var(--muted);
      font-size: 12px;
      overflow-wrap: anywhere;
    }
    .tableScroll {
      width: 100%;
      overflow-x: auto;
    }
    .tableScroll table {
      width: max-content;
      max-width: none;
      border-collapse: collapse;
      table-layout: auto;
    }
    th, td {
      border-bottom: 1px solid var(--line);
      padding: 7px 10px;
      text-align: left;
      vertical-align: top;
      white-space: nowrap;
    }
    td.wrap {
      min-width: 180px;
      max-width: 560px;
      white-space: normal;
      overflow-wrap: anywhere;
    }
    td.path {
      min-width: 120px;
      max-width: 260px;
      white-space: normal;
      overflow-wrap: anywhere;
    }
    th {
      color: var(--muted);
      font-size: 12px;
      font-weight: 600;
    }
    .two { grid-column: span 6; }
    .status {
      display: inline-flex;
      align-items: center;
      min-height: 24px;
      padding: 2px 8px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: #fff;
      font-size: 12px;
      color: var(--muted);
    }
    .status.ok { color: var(--ok); border-color: rgba(20,125,100,.35); background: rgba(20,125,100,.08); }
    .status.warning { color: var(--warn); border-color: rgba(161,92,0,.35); background: rgba(161,92,0,.08); }
    .status.critical { color: var(--bad); border-color: rgba(189,44,44,.35); background: rgba(189,44,44,.08); }
    .logs {
      max-height: 520px;
      overflow: auto;
      border: 1px solid var(--line);
      border-radius: 8px;
    }
    .logs table { min-width: 720px; }
    .logs td.wrap { min-width: 320px; max-width: 900px; }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 12px;
    }
    .muted { color: var(--muted); }
    .empty {
      color: var(--muted);
      padding: 14px 0;
    }
    .actions {
      display: flex;
      gap: 10px;
      align-items: center;
      flex-wrap: wrap;
    }
    @media (max-width: 860px) {
      header { align-items: flex-start; flex-direction: column; }
      .two { grid-column: span 12; }
      .value { font-size: 20px; }
    }
  </style>
</head>
<body>
  <header>
    <h1>fnmonitor</h1>
    <div class="toolbar">
      <span id="updated">等待数据</span>
      <span id="health" class="status">loading</span>
      <button id="refresh" type="button">刷新</button>
      <button id="testNotify" type="button">测试通知</button>
      <a class="buttonLink" href="/settings">设置</a>
    </div>
  </header>
  <main class="grid">
    <section class="panel">
      <h2>系统指标</h2>
      <div id="stats" class="stats"></div>
    </section>

    <section class="panel">
      <h2>当前告警</h2>
      <div id="alerts"></div>
    </section>

    <section class="panel">
      <h2>通知测试</h2>
      <div class="actions">
        <button id="testNotifyPanel" type="button">测试通知</button>
        <span id="notifyResult" class="muted"></span>
      </div>
    </section>

    <section class="panel two">
      <h2>文件系统</h2>
      <div id="filesystems"></div>
    </section>

    <section class="panel two">
      <h2>网络接口</h2>
      <div id="network"></div>
    </section>

    <section class="panel two">
      <h2>温度</h2>
      <div id="temperatures"></div>
    </section>

    <section class="panel two">
      <h2>Top 内存进程</h2>
      <div id="processes"></div>
    </section>

    <section class="panel">
      <h2>系统日志</h2>
      <div id="logs" class="logs"></div>
    </section>

    <section class="panel">
      <h2>采集提示</h2>
      <div id="warnings"></div>
    </section>
  </main>
  <script>
    const els = {
      updated: document.getElementById('updated'),
      health: document.getElementById('health'),
      stats: document.getElementById('stats'),
      alerts: document.getElementById('alerts'),
      filesystems: document.getElementById('filesystems'),
      network: document.getElementById('network'),
      temperatures: document.getElementById('temperatures'),
      processes: document.getElementById('processes'),
      logs: document.getElementById('logs'),
      warnings: document.getElementById('warnings'),
      notifyResult: document.getElementById('notifyResult')
    };

    document.getElementById('refresh').addEventListener('click', load);
    document.getElementById('testNotify').addEventListener('click', testNotification);
    document.getElementById('testNotifyPanel').addEventListener('click', testNotification);

    function fmtBytes(n) {
      if (!n) return '0 B';
      const units = ['B','KB','MB','GB','TB','PB'];
      let v = n, i = 0;
      while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	      return v.toFixed(v >= 10 || i === 0 ? 0 : 1) + ' ' + units[i];
	    }
	    function pct(v) { return Number(v || 0).toFixed(1) + '%'; }
    function text(v) { return (v === undefined || v === null || v === '') ? '-' : String(v); }
    function esc(v) {
      return text(v).replace(/[&<>"']/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
    }
	    function table(headers, rows, className) {
	      if (!rows || rows.length === 0) return '<div class="empty">无数据</div>';
	      const cls = className ? ' class="' + esc(className) + '"' : '';
	      return '<div class="tableScroll"><table' + cls + '><thead><tr>' + headers.map(h => '<th>' + esc(h) + '</th>').join('') + '</tr></thead><tbody>' + rows.join('') + '</tbody></table></div>';
	    }
	    function stat(label, value, sub) {
	      return '<div class="stat"><div class="label">' + esc(label) + '</div><div class="value">' + esc(value) + '</div><div class="sub">' + esc(sub || '') + '</div></div>';
	    }
    function severityClass(sev) {
      if (sev === 'critical') return 'critical';
      if (sev === 'warning') return 'warning';
      return 'ok';
    }
    async function load() {
      const res = await fetch('/api/state', { cache: 'no-store' });
      const state = await res.json();
      render(state);
    }
    async function testNotification() {
      els.notifyResult.textContent = '发送中';
      try {
        const res = await fetch('/api/test-notification', { method: 'POST' });
        const data = await res.json();
        const ok = data.every(x => x.ok);
        els.notifyResult.textContent = data.map(x => x.channel + ': ' + (x.ok ? 'OK' : x.error)).join(' | ');
        els.notifyResult.style.color = ok ? 'var(--ok)' : 'var(--bad)';
      } catch (err) {
        els.notifyResult.textContent = String(err);
        els.notifyResult.style.color = 'var(--bad)';
      }
    }
    function render(state) {
      const s = state.snapshot || {};
      const host = s.host || {};
      const cpu = s.cpu || {};
      const load = s.load || {};
      const mem = s.memory || {};
      const swap = s.swap || {};
      const net = s.network || {};
      const procs = s.processes || {};
      const alerts = state.alerts || [];
	      els.updated.textContent = state.updated_at ? '更新 ' + new Date(state.updated_at).toLocaleString() : '等待数据';
	      els.health.textContent = alerts.length ? alerts.length + ' alerts' : 'ok';
	      els.health.className = 'status ' + (alerts.some(a => a.severity === 'critical') ? 'critical' : alerts.length ? 'warning' : 'ok');

	      els.stats.innerHTML = [
	        stat('主机', host.hostname, text(host.platform) + ' ' + text(host.platform_version)),
	        stat('CPU', pct(cpu.percent), (cpu.logical_cores || 0) + ' 逻辑核 / ' + (cpu.physical_cores || 0) + ' 物理核'),
	        stat('Load 1/5/15', text(load.load1) + ' / ' + text(load.load5) + ' / ' + text(load.load15), 'Load1/CPU ' + text(load.load1_per_cpu)),
	        stat('内存', pct(mem.used_percent), fmtBytes(mem.used) + ' / ' + fmtBytes(mem.total)),
	        stat('Swap', pct(swap.used_percent), fmtBytes(swap.used) + ' / ' + fmtBytes(swap.total)),
	        stat('网络入站', text(net.rx_mbps || 0) + ' Mbps', fmtBytes(net.total_rx_bytes) + ' total'),
	        stat('网络出站', text(net.tx_mbps || 0) + ' Mbps', fmtBytes(net.total_tx_bytes) + ' total'),
	        stat('进程', procs.count || 0, '启动 ' + (host.uptime_seconds ? Math.floor(host.uptime_seconds / 3600) + 'h' : '-'))
	      ].join('');

	      els.alerts.innerHTML = alerts.length ? table(['级别','标题','内容'], alerts.map(a =>
	        '<tr><td><span class="status ' + severityClass(a.severity) + '">' + esc(a.severity) + '</span></td><td class="wrap">' + esc(a.title) + '</td><td class="wrap">' + esc(a.message) + '</td></tr>'
	      )) : '<div class="empty">无当前告警</div>';

	      els.filesystems.innerHTML = table(['挂载点','设备','类型','使用率','空间','inode'], (s.filesystems || []).map(fs =>
	        '<tr><td class="mono path">' + esc(fs.mountpoint) + '</td><td class="mono path">' + esc(fs.device) + '</td><td>' + esc(fs.fstype) + '</td><td>' + pct(fs.used_percent) + '</td><td>' + fmtBytes(fs.used) + ' / ' + fmtBytes(fs.total) + '</td><td>' + pct(fs.inodes_used_percent) + '</td></tr>'
	      ));

	      els.network.innerHTML = table(['接口','接收','发送','包','错误/丢弃'], (net.interfaces || []).map(n =>
	        '<tr><td class="mono">' + esc(n.name) + '</td><td>' + fmtBytes(n.bytes_recv) + '</td><td>' + fmtBytes(n.bytes_sent) + '</td><td>' + esc(n.packets_recv) + ' / ' + esc(n.packets_sent) + '</td><td>' + esc(n.err_in + n.err_out) + ' / ' + esc(n.drop_in + n.drop_out) + '</td></tr>'
	      ));

	      els.temperatures.innerHTML = table(['传感器','温度','High','Critical'], (s.temperatures || []).map(t =>
	        '<tr><td class="mono path">' + esc(t.sensor_key) + '</td><td>' + esc(t.temperature_c) + '°C</td><td>' + esc(t.high || '-') + '</td><td>' + esc(t.critical || '-') + '</td></tr>'
	      ));

	      els.processes.innerHTML = table(['PID','名称','RSS','VMS','CPU'], (procs.top_memory || []).map(p =>
	        '<tr><td class="mono">' + esc(p.pid) + '</td><td class="path">' + esc(p.name) + '</td><td>' + fmtBytes(p.rss) + '</td><td>' + fmtBytes(p.vms) + '</td><td>' + pct(p.cpu_percent) + '</td></tr>'
	      ));

	      const logEntries = ((state.logs || {}).entries || []).slice(-300).reverse();
	      els.logs.innerHTML = table(['时间','来源','级别','消息'], logEntries.map(l =>
	        '<tr><td class="mono">' + esc(l.time) + '</td><td class="path">' + esc(l.source) + '</td><td><span class="status ' + (l.matched ? severityClass(l.level) : '') + '">' + esc(l.level) + '</span></td><td class="mono wrap">' + esc(l.message) + '</td></tr>'
	      ));

	      const warnings = [...(s.warnings || []), ...((state.logs || {}).warnings || [])];
	      els.warnings.innerHTML = warnings.length ? table(['来源'], warnings.map(w => '<tr><td class="mono wrap">' + esc(w) + '</td></tr>')) : '<div class="empty">无采集提示</div>';
	    }
    load();
    setInterval(load, 5000);
  </script>
</body>
</html>`

const settingsHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>fnmonitor 设置</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f5f7fb;
      --panel: #ffffff;
      --text: #152033;
      --muted: #617089;
      --line: #dfe5ee;
      --ok: #147d64;
      --bad: #bd2c2c;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-width: 320px;
      background: var(--bg);
      color: var(--text);
      font: 14px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: 0;
    }
    header {
      display: flex;
      gap: 16px;
      align-items: center;
      justify-content: space-between;
      padding: 14px clamp(16px, 3vw, 28px);
      border-bottom: 1px solid var(--line);
      background: #fff;
    }
    h1 { margin: 0; font-size: 18px; line-height: 1.2; }
    main {
      width: min(920px, 100%);
      margin: 0 auto;
      padding: 18px clamp(12px, 2vw, 28px) 40px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
    }
    .actions {
      display: flex;
      gap: 10px;
      align-items: center;
      flex-wrap: wrap;
    }
    button, a.buttonLink {
      display: inline-flex;
      min-height: 36px;
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--text);
      padding: 7px 12px;
      text-decoration: none;
      cursor: pointer;
      white-space: nowrap;
    }
    button.primary { background: #152033; color: #fff; border-color: #152033; }
    .muted { color: var(--muted); }
    pre {
      margin: 14px 0 0;
      padding: 12px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #fbfcfe;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 12px;
    }
  </style>
</head>
<body>
  <header>
    <h1>fnmonitor 设置</h1>
    <div class="actions">
      <a class="buttonLink" href="/">监控面板</a>
    </div>
  </header>
  <main>
    <section class="panel">
      <h2>测试通知</h2>
      <p class="muted">点击按钮会立即使用当前通知配置发送测试通知，不会保存配置，也不会重启服务。</p>
      <div class="actions">
        <button id="sendTest" class="primary" type="button">发送测试通知</button>
        <span id="result" class="muted"></span>
      </div>
      <pre id="detail">等待操作</pre>
    </section>
  </main>
  <script>
    const result = document.getElementById('result');
    const detail = document.getElementById('detail');
    async function loadLastResult() {
      try {
        const res = await fetch('/api/last-notification-test', { cache: 'no-store' });
        const data = await res.json();
        if (data.available) {
          detail.textContent = data.content;
        }
      } catch (err) {
        detail.textContent = String(err);
      }
    }
    document.getElementById('sendTest').addEventListener('click', async () => {
      result.textContent = '发送中';
      result.style.color = 'var(--muted)';
      detail.textContent = '请求发送测试通知...';
      try {
        const res = await fetch('/api/test-notification', { method: 'POST' });
        const data = await res.json();
        const ok = data.every(x => x.ok);
        result.textContent = ok ? '发送成功' : '发送失败';
        result.style.color = ok ? 'var(--ok)' : 'var(--bad)';
        detail.textContent = JSON.stringify(data, null, 2);
      } catch (err) {
        result.textContent = '发送失败';
        result.style.color = 'var(--bad)';
        detail.textContent = String(err);
      }
    });
    loadLastResult();
  </script>
</body>
</html>`
