import React, { useState } from 'react'
import './Modal.css'
import './SettingsModal.css'
import './SettingsPanel.css'

// ─── 选项常量 ────────────────────────────────────────────────────────────────

const CORES = [
  { value: 'sing-box', label: 'sing-box（配置为 JSON）' },
  { value: 'mihomo', label: 'mihomo / Clash.Meta（配置为 YAML）' },
]

const TUN_STACKS = [
  { value: 'gvisor', label: 'gvisor（默认，兼容性好）' },
  { value: 'system', label: 'system（性能好，需内核支持）' },
  { value: 'mixed', label: 'mixed（混合模式）' },
]

// 系统代理监听地址可选项（Windows 系统代理始终指向 127.0.0.1）
const LISTEN_ADDRS = ['127.0.0.1', '0.0.0.0', '::']

const LOG_LEVELS = ['debug', 'info', 'warning', 'error']

const DNS_MODES = [
  { value: 'redir-host', label: 'redir-host（真实 IP）' },
  { value: 'fake-ip', label: 'fake-ip（假 IP，白名单真实解析）' },
]

const SINGBOX_DNS_TYPES = ['tcp', 'udp', 'tls', 'https', 'quic']

const CLASH_API_LISTENS = ['127.0.0.1', '0.0.0.0']

// builtin 段默认值（与后端 DefaultBuiltin 一致）
const DEFAULT_BUILTIN = {
  log_level: 'warning',
  dns_mode: 'redir-host',
  ipv6: false,
  resolver_dns: '223.5.5.5',
  clash_api: { listen: '127.0.0.1', port: 9090, secret: '' },
  singbox_direct: { type: 'udp', address: '223.5.5.5', port: 53, path: '' },
  singbox_proxy: { type: 'udp', address: '8.8.8.8', port: 53, path: '' },
  mihomo_direct: ['223.5.5.5', '119.29.29.29'],
  mihomo_proxy: ['1.1.1.1', '8.8.8.8'],
}

const DEFAULTS = {
  core: 'sing-box',
  proxy_listen: '127.0.0.1',
  proxy_port: 2080,
  exit_disable_proxy: true,
  tun_stack: 'gvisor',
  tun_mtu: 9000,
  tun_strict_route: true,
  sub_user_agent: 'clash.meta',
  sub_timeout_sec: 30,
  log_max_lines: 500,
  poll_interval_ms: 2000,
}

export default function SettingsPanel({ settings, onSave }) {
  const [form, setForm] = useState(() => ({
    ...DEFAULTS,
    ...settings,
    // settings 缺 builtin 段（旧版）时补默认
    builtin: { ...JSON.parse(JSON.stringify(DEFAULT_BUILTIN)), ...(settings?.builtin || {}) },
  }))
  const [view, setView] = useState('app') // app = 程序 | conf = 配置
  const [saving, setSaving] = useState(false)

  const set = (key, value) => setForm(f => ({ ...f, [key]: value }))
  const setNum = (key, value) => {
    // 数字输入：允许暂时为空，保存时再校验
    if (value === '') { set(key, ''); return }
    const n = Number(value)
    set(key, Number.isNaN(n) ? value : n)
  }

  // builtin 段的嵌套更新：setB('clash_api.port', 9090)
  const setB = (path, value) => {
    setForm(f => {
      const b = { ...f.builtin }
      const [k1, k2] = path.split('.')
      if (k2 !== undefined) {
        b[k1] = { ...b[k1], [k2]: value }
      } else {
        b[k1] = value
      }
      return { ...f, builtin: b }
    })
  }
  // sing-box DNS 服务器字段更新
  const setDNS = (key, field, value) => {
    setForm(f => ({
      ...f,
      builtin: { ...f.builtin, [key]: { ...f.builtin[key], [field]: value } },
    }))
  }
  // mihomo DNS 列表项更新（固定两个输入框）
  const setList = (key, idx, value) => {
    setForm(f => {
      const list = [...(f.builtin[key] || [])]
      while (list.length < 2) list.push('')
      list[idx] = value
      return { ...f, builtin: { ...f.builtin, [key]: list } }
    })
  }
  const listAt = (key, idx) => {
    const l = form.builtin[key] || []
    return l[idx] ?? ''
  }

  const handleReset = () => {
    setForm(f => ({
      ...DEFAULTS,
      builtin: JSON.parse(JSON.stringify(DEFAULT_BUILTIN)),
      core: f.core, // 重置保留内核选择，避免误切内核
    }))
  }

  const handleSave = async () => {
    // 前端预校验，给出即时反馈
    const port = Number(form.proxy_port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) return alert('代理端口必须是 1-65535 的整数')
    if (!String(form.sub_user_agent).trim()) return alert('订阅 User-Agent 不能为空')
    const checks = [
      ['sub_timeout_sec', 1, 600, '订阅超时'],
      ['log_max_lines', 50, 100000, '日志行数'],
      ['poll_interval_ms', 500, 60000, '轮询间隔'],
      ['tun_mtu', 576, 65535, 'TUN MTU'],
    ]
    for (const [key, min, max, label] of checks) {
      const v = Number(form[key])
      if (!Number.isInteger(v) || v < min || v > max) {
        return alert(`${label}必须是 ${min}-${max} 的整数`)
      }
    }
    const b = form.builtin
    if (!/^(\d{1,3}\.){3}\d{1,3}$|^[0-9a-fA-F:]+$/.test(String(b.resolver_dns).trim())) {
      return alert('解析 DNS 服务器必须是 IP 地址')
    }
    if (!Number.isInteger(Number(b.clash_api.port)) || Number(b.clash_api.port) < 1 || Number(b.clash_api.port) > 65535) {
      return alert('clash-api 端口必须是 1-65535 的整数')
    }
    if (!String(b.singbox_direct.address).trim()) return alert('sing-box 直连 DNS 地址不能为空')
    if (!String(b.singbox_proxy.address).trim()) return alert('sing-box 代理 DNS 地址不能为空')
    for (const v of b.mihomo_direct) if (!String(v).trim()) return alert('mihomo 直连 DNS 不能为空')
    for (const v of b.mihomo_proxy) if (!String(v).trim()) return alert('mihomo 代理 DNS 不能为空')
    setSaving(true)
    try {
      await onSave({
        ...form,
        proxy_listen: String(form.proxy_listen).trim(),
        sub_user_agent: String(form.sub_user_agent).trim(),
        builtin: {
          ...b,
          resolver_dns: String(b.resolver_dns).trim(),
          clash_api: { ...b.clash_api, secret: String(b.clash_api.secret) },
          singbox_direct: trimDNS(b.singbox_direct),
          singbox_proxy: trimDNS(b.singbox_proxy),
          mihomo_direct: b.mihomo_direct.map(v => String(v).trim()),
          mihomo_proxy: b.mihomo_proxy.map(v => String(v).trim()),
        },
      })
    } finally {
      setSaving(false)
    }
  }

  const trimDNS = d => ({
    ...d,
    address: String(d.address).trim(),
    path: String(d.path || '').trim(),
  })

  return (
    <div className="settings-panel">
      {/* ── 顶部视图切换 ── */}
      <div className="settings-tabs">
        <button
          className={`settings-tab-btn${view === 'app' ? ' active' : ''}`}
          onClick={() => setView('app')}
        >程序</button>
        <button
          className={`settings-tab-btn${view === 'conf' ? ' active' : ''}`}
          onClick={() => setView('conf')}
        >配置</button>
      </div>

      <div className="settings-panel-body">
        {view === 'app' && (
          <>
            {/* ── 内核 ── */}
            <div className="settings-section">
              <div className="settings-section-title">内核</div>
              <div className="settings-row">
                <label className="settings-label">代理内核</label>
                <select
                  className="modal-input settings-input"
                  value={form.core}
                  onChange={e => set('core', e.target.value)}
                >
                  {CORES.map(c => (
                    <option key={c.value} value={c.value}>{c.label}</option>
                  ))}
                </select>
                <span className="settings-hint">切换后使用各自记忆的配置文件（configs 目录 json / yaml）</span>
              </div>
            </div>

            {/* ── 订阅 ── */}
            <div className="settings-section">
              <div className="settings-section-title">订阅</div>
              <div className="settings-row">
                <label className="settings-label">User-Agent</label>
                <input
                  className="modal-input settings-input"
                  value={form.sub_user_agent}
                  onChange={e => set('sub_user_agent', e.target.value)}
                  placeholder="clash.meta"
                />
              </div>
              <div className="settings-row">
                <label className="settings-label">请求超时</label>
                <input
                  className="modal-input settings-input"
                  type="number"
                  min="1"
                  max="600"
                  value={form.sub_timeout_sec}
                  onChange={e => setNum('sub_timeout_sec', e.target.value)}
                />
                <span className="settings-hint">秒</span>
              </div>
            </div>

            {/* ── 日志与界面 ── */}
            <div className="settings-section">
              <div className="settings-section-title">日志与界面</div>
              <div className="settings-row">
                <label className="settings-label">日志保留行数</label>
                <input
                  className="modal-input settings-input"
                  type="number"
                  min="50"
                  max="100000"
                  value={form.log_max_lines}
                  onChange={e => setNum('log_max_lines', e.target.value)}
                />
              </div>
              <div className="settings-row">
                <label className="settings-label">状态轮询间隔</label>
                <input
                  className="modal-input settings-input"
                  type="number"
                  min="500"
                  max="60000"
                  step="100"
                  value={form.poll_interval_ms}
                  onChange={e => setNum('poll_interval_ms', e.target.value)}
                />
                <span className="settings-hint">毫秒</span>
              </div>
            </div>
          </>
        )}

        {view === 'conf' && (
          <>
            {/* ── 系统代理 ── */}
            <div className="settings-section">
              <div className="settings-section-title">系统代理</div>
              <div className="settings-row">
                <label className="settings-label">监听地址</label>
                <select
                  className="modal-input settings-input"
                  value={LISTEN_ADDRS.includes(form.proxy_listen) ? form.proxy_listen : '127.0.0.1'}
                  onChange={e => set('proxy_listen', e.target.value)}
                >
                  {LISTEN_ADDRS.map(addr => (
                    <option key={addr} value={addr}>{addr}</option>
                  ))}
                </select>
                <span className="settings-hint">0.0.0.0 / :: 允许局域网访问</span>
              </div>
              <div className="settings-row">
                <label className="settings-label">代理端口</label>
                <input
                  className="modal-input settings-input"
                  type="number"
                  min="1"
                  max="65535"
                  value={form.proxy_port}
                  onChange={e => setNum('proxy_port', e.target.value)}
                />
                <span className="settings-hint">mixed inbound 监听端口</span>
              </div>
              <div className="settings-row">
                <label className="settings-label">退出时关闭系统代理</label>
                <input
                  type="checkbox"
                  className="settings-check"
                  checked={!!form.exit_disable_proxy}
                  onChange={e => set('exit_disable_proxy', e.target.checked)}
                />
                <span className="settings-hint">退出程序前自动还原系统代理设置</span>
              </div>
            </div>

            {/* ── TUN 模式 ── */}
            <div className="settings-section">
              <div className="settings-section-title">TUN 模式</div>
              <div className="settings-row">
                <label className="settings-label">协议栈</label>
                <select
                  className="modal-input settings-input"
                  value={form.tun_stack}
                  onChange={e => set('tun_stack', e.target.value)}
                >
                  {TUN_STACKS.map(s => (
                    <option key={s.value} value={s.value}>{s.label}</option>
                  ))}
                </select>
              </div>
              <div className="settings-row">
                <label className="settings-label">MTU</label>
                <input
                  className="modal-input settings-input"
                  type="number"
                  min="576"
                  max="65535"
                  value={form.tun_mtu}
                  onChange={e => setNum('tun_mtu', e.target.value)}
                />
              </div>
              <div className="settings-row">
                <label className="settings-label">strict_route</label>
                <input
                  type="checkbox"
                  className="settings-check"
                  checked={!!form.tun_strict_route}
                  onChange={e => set('tun_strict_route', e.target.checked)}
                />
                <span className="settings-hint">严格路由，防止流量绕过 TUN（仅 sing-box 生效，mihomo 无此选项）</span>
              </div>
            </div>

            {/* ── 生成配置 ── */}
            <div className="settings-section">
              <div className="settings-section-title">生成配置（内置路由模式）</div>
              <div className="settings-row">
                <label className="settings-label">日志等级</label>
                <select
                  className="modal-input settings-input"
                  value={form.builtin.log_level}
                  onChange={e => setB('log_level', e.target.value)}
                >
                  {LOG_LEVELS.map(l => <option key={l} value={l}>{l}</option>)}
                </select>
              </div>
              <div className="settings-row">
                <label className="settings-label">DNS 模式</label>
                <select
                  className="modal-input settings-input"
                  value={form.builtin.dns_mode}
                  onChange={e => setB('dns_mode', e.target.value)}
                >
                  {DNS_MODES.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
                </select>
                <span className="settings-hint">fake-ip：fakeipfilter 白名单域名真实解析，其余返回假 IP</span>
              </div>
              <div className="settings-row">
                <label className="settings-label">IPv6</label>
                <input
                  type="checkbox"
                  className="settings-check"
                  checked={!!form.builtin.ipv6}
                  onChange={e => setB('ipv6', e.target.checked)}
                />
                <span className="settings-hint">mihomo 全局与 DNS 的 ipv6；sing-box 同步调整解析策略与 TUN 地址</span>
              </div>
              <div className="settings-row">
                <label className="settings-label">解析 DNS 服务器</label>
                <input
                  className="modal-input settings-input"
                  value={form.builtin.resolver_dns}
                  onChange={e => setB('resolver_dns', e.target.value)}
                  placeholder="必须是 IP，如 223.5.5.5"
                />
                <span className="settings-hint">用于解析 DNS 服务器自身的域名（mihomo 对应 default-nameserver）</span>
              </div>

              {/* sing-box DNS 服务器 */}
              <div className="settings-subsection">
                <div className="settings-row">
                  <label className="settings-label">sing-box 直连 DNS</label>
                  <div className="settings-inline">
                    <select
                      className="modal-input"
                      value={form.builtin.singbox_direct.type}
                      onChange={e => setDNS('singbox_direct', 'type', e.target.value)}
                    >
                      {SINGBOX_DNS_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                    </select>
                    <input
                      className="modal-input"
                      value={form.builtin.singbox_direct.address}
                      onChange={e => setDNS('singbox_direct', 'address', e.target.value)}
                      placeholder="地址（必填）"
                    />
                    <input
                      className="modal-input num"
                      type="number"
                      value={form.builtin.singbox_direct.port ?? ''}
                      onChange={e => setDNS('singbox_direct', 'port', e.target.value === '' ? 0 : Number(e.target.value))}
                      placeholder="端口"
                      title="选填，留空使用默认端口"
                    />
                    <input
                      className="modal-input"
                      value={form.builtin.singbox_direct.path || ''}
                      onChange={e => setDNS('singbox_direct', 'path', e.target.value)}
                      placeholder="路径"
                      title="选填，https 类型的 URL 路径"
                    />
                  </div>
                </div>
                <div className="settings-row">
                  <label className="settings-label">sing-box 代理 DNS</label>
                  <div className="settings-inline">
                    <select
                      className="modal-input"
                      value={form.builtin.singbox_proxy.type}
                      onChange={e => setDNS('singbox_proxy', 'type', e.target.value)}
                    >
                      {SINGBOX_DNS_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                    </select>
                    <input
                      className="modal-input"
                      value={form.builtin.singbox_proxy.address}
                      onChange={e => setDNS('singbox_proxy', 'address', e.target.value)}
                      placeholder="地址（必填）"
                    />
                    <input
                      className="modal-input num"
                      type="number"
                      value={form.builtin.singbox_proxy.port ?? ''}
                      onChange={e => setDNS('singbox_proxy', 'port', e.target.value === '' ? 0 : Number(e.target.value))}
                      placeholder="端口"
                      title="选填，留空使用默认端口"
                    />
                    <input
                      className="modal-input"
                      value={form.builtin.singbox_proxy.path || ''}
                      onChange={e => setDNS('singbox_proxy', 'path', e.target.value)}
                      placeholder="路径"
                      title="选填，https 类型的 URL 路径"
                    />
                  </div>
                  <span className="settings-hint">直连与代理 DNS 自动携带 domain_resolver → 解析 DNS</span>
                </div>
              </div>

              {/* mihomo DNS */}
              <div className="settings-subsection">
                <div className="settings-row">
                  <label className="settings-label">mihomo 直连 DNS</label>
                  <div className="settings-inline">
                    <input
                      className="modal-input"
                      value={listAt('mihomo_direct', 0)}
                      onChange={e => setList('mihomo_direct', 0, e.target.value)}
                      placeholder="第一个"
                    />
                    <input
                      className="modal-input"
                      value={listAt('mihomo_direct', 1)}
                      onChange={e => setList('mihomo_direct', 1, e.target.value)}
                      placeholder="第二个"
                    />
                  </div>
                </div>
                <div className="settings-row">
                  <label className="settings-label">mihomo 代理 DNS</label>
                  <div className="settings-inline">
                    <input
                      className="modal-input"
                      value={listAt('mihomo_proxy', 0)}
                      onChange={e => setList('mihomo_proxy', 0, e.target.value)}
                      placeholder="第一个"
                    />
                    <input
                      className="modal-input"
                      value={listAt('mihomo_proxy', 1)}
                      onChange={e => setList('mihomo_proxy', 1, e.target.value)}
                      placeholder="第二个"
                    />
                  </div>
                  <span className="settings-hint">生成时自动追加 #PROXY（查询经代理组出站）</span>
                </div>
              </div>
            </div>

            {/* ── clash-api ── */}
            <div className="settings-section">
              <div className="settings-section-title">clash-api</div>
              <div className="settings-row">
                <label className="settings-label">监听地址</label>
                <select
                  className="modal-input settings-input"
                  value={CLASH_API_LISTENS.includes(form.builtin.clash_api.listen) ? form.builtin.clash_api.listen : '127.0.0.1'}
                  onChange={e => setB('clash_api.listen', e.target.value)}
                >
                  {CLASH_API_LISTENS.map(a => <option key={a} value={a}>{a}</option>)}
                </select>
              </div>
              <div className="settings-row">
                <label className="settings-label">端口</label>
                <input
                  className="modal-input settings-input"
                  type="number"
                  min="1"
                  max="65535"
                  value={form.builtin.clash_api.port}
                  onChange={e => setB('clash_api.port', e.target.value === '' ? '' : Number(e.target.value))}
                />
                <span className="settings-hint">面板地址 http://{form.builtin.clash_api.listen}:{form.builtin.clash_api.port}/ui</span>
              </div>
              <div className="settings-row">
                <label className="settings-label">密码</label>
                <input
                  className="modal-input settings-input"
                  value={form.builtin.clash_api.secret}
                  onChange={e => setB('clash_api.secret', e.target.value)}
                  placeholder="留空表示无密码"
                />
              </div>
            </div>
          </>
        )}

        <div className="settings-note">
          提示：「生成配置」仅作用于内置路由模式（绕过大陆 / GFW列表 / 全局代理）合成的配置文件；
          切换内核 / 代理端口 / TUN / 生成配置相关设置在下次「开启系统代理 / 开启 TUN / 启动核心」时生效；
          日志行数与轮询间隔保存后立即生效。
        </div>
      </div>

      <div className="settings-panel-footer">
        <button className="btn-cancel settings-reset" onClick={handleReset} disabled={saving}>
          恢复默认
        </button>
        <button className="btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? '保存中…' : '保存'}
        </button>
      </div>
    </div>
  )
}
