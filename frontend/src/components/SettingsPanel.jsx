import React, { useState } from 'react'
import './Modal.css'
import './SettingsModal.css'
import './SettingsPanel.css'

// 设置项分组定义
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

export default function SettingsPanel({ settings, onSave }) {
  const [form, setForm] = useState(() => ({
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
    ...settings,
  }))
  const [saving, setSaving] = useState(false)

  const set = (key, value) => setForm(f => ({ ...f, [key]: value }))
  const setNum = (key, value) => {
    // 数字输入：允许暂时为空，保存时再校验
    if (value === '') { set(key, ''); return }
    const n = Number(value)
    set(key, Number.isNaN(n) ? value : n)
  }

  const handleReset = () => {
    setForm(f => ({
      ...f,
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
    setSaving(true)
    try {
      await onSave({
        ...form,
        proxy_listen: String(form.proxy_listen).trim(),
        sub_user_agent: String(form.sub_user_agent).trim(),
      })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="settings-panel">
      <div className="settings-panel-body">
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

        <div className="settings-note">
          提示：切换内核 / 代理端口 / TUN 相关设置在下次「开启系统代理 / 开启 TUN」或核心重启时生效；
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
