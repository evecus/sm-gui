import React, { useState } from 'react'
import './Modal.css'
import './EditNodeModal.css'
import { api } from '../lib/wails'

// 协议分组
const TRANSPORT_PROTOCOLS = ['vmess', 'vless', 'trojan']          // 有传输层设置
const TLS_PROTOCOLS = ['vmess', 'vless', 'trojan', 'http', 'anytls'] // 可配 TLS
const FIXED_TLS_PROTOCOLS = ['hysteria', 'hysteria2', 'tuic']     // TLS 必开(QUIC)
const SUPPORTED = [...TRANSPORT_PROTOCOLS, ...TLS_PROTOCOLS, ...FIXED_TLS_PROTOCOLS,
  'ss', 'socks', 'ssr', 'wireguard']

const PROTO_KEY = {
  vmess: 'vmess', vless: 'vless', trojan: 'trojan', ss: 'ss',
  hysteria: 'hysteria', hysteria2: 'hysteria2', tuic: 'tuic',
  socks: 'socks', http: 'http', anytls: 'anytls',
  ssr: 'ssr', wireguard: 'wireguard',
}

const TRANSPORT_TYPES = [
  { v: '', label: '无 (TCP / RAW)' },
  { v: 'ws', label: 'ws (WebSocket)' },
  { v: 'http', label: 'http (h2/h3)' },
  { v: 'grpc', label: 'grpc' },
  { v: 'httpupgrade', label: 'httpupgrade' },
  { v: 'quic', label: 'quic' },
  { v: 'xhttp', label: 'xhttp' },
]

const SECURITY_TYPES = ['auto', 'none', 'zero', 'aes-128-gcm', 'chacha20-poly1305']
const FLOW_TYPES = ['', 'xtls-rprx-vision']
const CC_TYPES = ['cubic', 'new_reno', 'bbr']
const UDP_MODES = ['native', 'quic']
const XHTTP_MODES = ['auto', 'packet-up', 'stream-up', 'stream-one']

const clone = (o) => JSON.parse(JSON.stringify(o))

function initForm(node) {
  const f = clone(node)
  // 确保协议配置对象存在
  const key = PROTO_KEY[f.protocol]
  if (key && !f[key]) f[key] = {}
  // 端口转数字
  f.port = Number(f.port) || 0
  return f
}

export default function EditNodeModal({ node, onSaved, onClose }) {
  const [form, setForm] = useState(() => initForm(node))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const pk = PROTO_KEY[form.protocol]
  const proto = pk ? (form[pk] || {}) : null
  const supported = SUPPORTED.includes(form.protocol)
  const hasTransport = TRANSPORT_PROTOCOLS.includes(form.protocol)
  const hasTLSSection = TLS_PROTOCOLS.includes(form.protocol)
  const hasFixedTLS = FIXED_TLS_PROTOCOLS.includes(form.protocol)
  const isVless = form.protocol === 'vless'
  const isReality = hasTLSSection && proto?.tls && isVless && !!proto?.public_key

  // 更新嵌套字段的通用方法
  const setBase = (k, v) => setForm(f => ({ ...f, [k]: v }))
  const setProtoField = (k, v) => setForm(f => ({ ...f, [pk]: { ...f[pk], [k]: v } }))
  const setTransport = (k, v) => setForm(f => {
    const t = { ...(f[pk].transport || {}), [k]: v }
    return { ...f, [pk]: { ...f[pk], transport: t } }
  })

  // ALPN: JSON 里是数组, 表单里用逗号分隔字符串
  const alpnText = Array.isArray(proto?.alpn) ? proto.alpn.join(',') : (proto?.alpn || '')

  const handleTransportType = (v) => {
    if (v === '') {
      // 移除传输层
      setForm(f => {
        const p = { ...f[pk] }
        delete p.transport
        return { ...f, [pk]: p }
      })
    } else {
      setTransport('type', v)
    }
  }

  const handleSave = async () => {
    if (!form.name?.trim()) { setError('请输入节点名称'); return }
    if (!form.address?.trim()) { setError('请输入服务器地址'); return }
    if (!form.port || form.port <= 0 || form.port > 65535) { setError('端口无效'); return }
    setSaving(true)
    setError('')
    try {
      const out = clone(form)
      const p = out[pk] || {}
      // alpn 字符串 → 数组
      const toALPN = (s) => (s || '').split(',').map(x => x.trim()).filter(Boolean)
      if (p.alpn !== undefined || alpnText) p.alpn = toALPN(alpnText)
      if (p.alpn !== undefined && (!Array.isArray(p.alpn) || p.alpn.length === 0)) delete p.alpn
      // 数值字段
      out.port = Number(out.port) || 0
      if (pk === 'vmess') p.alter_id = Number(p.alter_id) || 0
      for (const k of ['up_mbps', 'down_mbps']) if (p[k] !== undefined) p[k] = Number(p[k]) || 0
      if (p.transport) {
        if (p.transport.max_early_data !== undefined) p.transport.max_early_data = Number(p.transport.max_early_data) || 0
        if (!p.transport.type) delete p.transport
      }
      // wireguard: 地址/reserved 处理
      if (pk === 'wireguard') {
        if (typeof p.reserved_text !== 'undefined') delete p.reserved_text
        p.mtu = Number(p.mtu) || 0
      }
      out[pk] = p
      // 结构化协议保存后清除 raw 透传, 让编辑内容生效
      if (SUPPORTED.includes(out.protocol)) delete out.raw_outbound
      await api.UpdateNode(out)
      onSaved()
    } catch (e) {
      setError('保存失败: ' + (e?.message || e))
    } finally {
      setSaving(false)
    }
  }

  const transportType = proto?.transport?.type || ''

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal edit-node-modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">编辑节点 — {form.protocol?.toUpperCase()}</span>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>

        <div className="modal-body edit-node-body">
          {!supported && (
            <div className="edit-note">
              该节点 ({form.protocol}) 来自 sing-box 完整配置(raw)，仅支持编辑基本信息。
            </div>
          )}
          {error && <div className="edit-error">{error}</div>}

          {/* ─── 常规 ─── */}
          <div className="section-title">常规设置</div>
          <div className="form-grid">
            <label>别名</label>
            <input className="f-input" value={form.name || ''} onChange={e => setBase('name', e.target.value)} />
            <label>协议</label>
            <input className="f-input" value={form.protocol || ''} disabled />
            <label>地址 (服务器)</label>
            <input className="f-input mono" value={form.address || ''} onChange={e => setBase('address', e.target.value)} />
            <label>端口</label>
            <input className="f-input mono" type="number" value={form.port || ''} onChange={e => setBase('port', e.target.value)} />
          </div>

          {/* ─── 协议参数 ─── */}
          {supported && (
            <>
              <div className="section-title">协议参数</div>
              <div className="form-grid">
                {(form.protocol === 'vmess' || form.protocol === 'vless' || form.protocol === 'tuic') && (
                  <>
                    <label>UUID / 用户 ID</label>
                    <input className="f-input mono" value={proto.uuid || ''} onChange={e => setProtoField('uuid', e.target.value)} />
                  </>
                )}
                {form.protocol === 'vmess' && (
                  <>
                    <label>加密方式 (security)</label>
                    <select className="f-input" value={proto.security || 'auto'} onChange={e => setProtoField('security', e.target.value)}>
                      {SECURITY_TYPES.map(s => <option key={s} value={s}>{s}</option>)}
                    </select>
                    <label>alterId</label>
                    <input className="f-input mono" type="number" value={proto.alter_id ?? 0} onChange={e => setProtoField('alter_id', e.target.value)} />
                  </>
                )}
                {form.protocol === 'vless' && (
                  <>
                    <label>流控 (flow)</label>
                    <select className="f-input" value={proto.flow || ''} onChange={e => setProtoField('flow', e.target.value)}>
                      {FLOW_TYPES.map(s => <option key={s} value={s}>{s || '(无)'}</option>)}
                    </select>
                  </>
                )}
                {(form.protocol === 'trojan' || form.protocol === 'anytls') && (
                  <>
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                  </>
                )}
                {form.protocol === 'ss' && (
                  <>
                    <label>加密方法 (method)</label>
                    <input className="f-input mono" value={proto.method || ''} onChange={e => setProtoField('method', e.target.value)} />
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                    <label>插件 (plugin)</label>
                    <input className="f-input mono" placeholder="obfs-local / v2ray-plugin" value={proto.plugin || ''} onChange={e => setProtoField('plugin', e.target.value)} />
                    <label>插件参数 (plugin_opts)</label>
                    <input className="f-input mono" placeholder="obfs=http;obfs-host=xxx" value={proto.plugin_opts || ''} onChange={e => setProtoField('plugin_opts', e.target.value)} />
                  </>
                )}
                {form.protocol === 'hysteria' && (
                  <>
                    <label>认证 (auth_str)</label>
                    <input className="f-input mono" value={proto.auth_str || ''} onChange={e => setProtoField('auth_str', e.target.value)} />
                    <label>上传限速 (Mbps)</label>
                    <input className="f-input mono" type="number" value={proto.up_mbps ?? ''} onChange={e => setProtoField('up_mbps', e.target.value)} />
                    <label>下载限速 (Mbps)</label>
                    <input className="f-input mono" type="number" value={proto.down_mbps ?? ''} onChange={e => setProtoField('down_mbps', e.target.value)} />
                    <label>混淆 (obfs)</label>
                    <input className="f-input mono" value={proto.obfs || ''} onChange={e => setProtoField('obfs', e.target.value)} />
                  </>
                )}
                {form.protocol === 'hysteria2' && (
                  <>
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                    <label>混淆类型 (obfs)</label>
                    <input className="f-input mono" placeholder="salamander" value={proto.obfs || ''} onChange={e => setProtoField('obfs', e.target.value)} />
                    <label>混淆密码</label>
                    <input className="f-input mono" value={proto.obfs_password || ''} onChange={e => setProtoField('obfs_password', e.target.value)} />
                    <label>上传限速 (Mbps)</label>
                    <input className="f-input mono" type="number" value={proto.up_mbps ?? ''} onChange={e => setProtoField('up_mbps', e.target.value)} />
                    <label>下载限速 (Mbps)</label>
                    <input className="f-input mono" type="number" value={proto.down_mbps ?? ''} onChange={e => setProtoField('down_mbps', e.target.value)} />
                  </>
                )}
                {form.protocol === 'tuic' && (
                  <>
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                    <label>拥塞控制</label>
                    <select className="f-input" value={proto.congestion_control || 'cubic'} onChange={e => setProtoField('congestion_control', e.target.value)}>
                      {CC_TYPES.map(s => <option key={s} value={s}>{s}</option>)}
                    </select>
                    <label>UDP 中继模式</label>
                    <select className="f-input" value={proto.udp_relay_mode || 'native'} onChange={e => setProtoField('udp_relay_mode', e.target.value)}>
                      {UDP_MODES.map(s => <option key={s} value={s}>{s}</option>)}
                    </select>
                  </>
                )}
                {form.protocol === 'socks' && (
                  <>
                    <label>版本</label>
                    <select className="f-input" value={proto.version || '5'} onChange={e => setProtoField('version', e.target.value)}>
                      <option value="5">5</option>
                      <option value="4a">4a</option>
                    </select>
                    <label>用户名</label>
                    <input className="f-input mono" value={proto.username || ''} onChange={e => setProtoField('username', e.target.value)} />
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                  </>
                )}
                {form.protocol === 'http' && (
                  <>
                    <label>用户名</label>
                    <input className="f-input mono" value={proto.username || ''} onChange={e => setProtoField('username', e.target.value)} />
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                  </>
                )}
                {form.protocol === 'ssr' && (
                  <>
                    <label>加密方法</label>
                    <input className="f-input mono" value={proto.method || ''} onChange={e => setProtoField('method', e.target.value)} />
                    <label>密码</label>
                    <input className="f-input mono" value={proto.password || ''} onChange={e => setProtoField('password', e.target.value)} />
                    <label>协议 (protocol)</label>
                    <input className="f-input mono" value={proto.protocol || ''} onChange={e => setProtoField('protocol', e.target.value)} />
                    <label>协议参数</label>
                    <input className="f-input mono" value={proto.protocol_param || ''} onChange={e => setProtoField('protocol_param', e.target.value)} />
                    <label>混淆 (obfs)</label>
                    <input className="f-input mono" value={proto.obfs || ''} onChange={e => setProtoField('obfs', e.target.value)} />
                    <label>混淆参数</label>
                    <input className="f-input mono" value={proto.obfs_param || ''} onChange={e => setProtoField('obfs_param', e.target.value)} />
                  </>
                )}
                {form.protocol === 'wireguard' && (
                  <>
                    <label>本机私钥 (private_key)</label>
                    <input className="f-input mono" value={proto.private_key || ''} onChange={e => setProtoField('private_key', e.target.value)} />
                    <label>对端公钥 (peer_public_key)</label>
                    <input className="f-input mono" value={proto.peer_public_key || ''} onChange={e => setProtoField('peer_public_key', e.target.value)} />
                    <label>预共享密钥</label>
                    <input className="f-input mono" value={proto.pre_shared_key || ''} onChange={e => setProtoField('pre_shared_key', e.target.value)} />
                    <label>本机地址 (逗号分隔)</label>
                    <input className="f-input mono" placeholder="172.16.0.2/32" value={Array.isArray(proto.local_address) ? proto.local_address.join(',') : (proto.local_address || '')}
                      onChange={e => setProtoField('local_address', e.target.value.split(',').map(x => x.trim()).filter(Boolean))} />
                    <label>MTU</label>
                    <input className="f-input mono" type="number" value={proto.mtu ?? ''} onChange={e => setProtoField('mtu', e.target.value)} />
                    <label>保留字段 (reserved, 逗号分隔)</label>
                    <input className="f-input mono" placeholder="1,2,3" value={Array.isArray(proto.reserved) ? proto.reserved.join(',') : ''}
                      onChange={e => setProtoField('reserved', e.target.value.split(',').map(x => parseInt(x.trim(), 10)).filter(x => !isNaN(x)))} />
                  </>
                )}
              </div>
            </>
          )}

          {/* ─── 传输层 ─── */}
          {hasTransport && (
            <>
              <div className="section-title">传输层配置 (transport)</div>
              <div className="form-grid">
                <label>传输类型</label>
                <select className="f-input" value={transportType} onChange={e => handleTransportType(e.target.value)}>
                  {TRANSPORT_TYPES.map(t => <option key={t.v} value={t.v}>{t.label}</option>)}
                </select>
                {['ws', 'http', 'httpupgrade', 'xhttp'].includes(transportType) && (
                  <>
                    <label>路径 (path)</label>
                    <input className="f-input mono" value={proto.transport?.path || ''} onChange={e => setTransport('path', e.target.value)} />
                    <label>Host</label>
                    <input className="f-input mono" value={proto.transport?.host || ''} onChange={e => setTransport('host', e.target.value)} />
                  </>
                )}
                {transportType === 'ws' && (
                  <>
                    <label>早期数据上限 (max_early_data)</label>
                    <input className="f-input mono" type="number" value={proto.transport?.max_early_data ?? ''} onChange={e => setTransport('max_early_data', e.target.value)} />
                    <label>早期数据头 (early_data_header_name)</label>
                    <input className="f-input mono" placeholder="Sec-WebSocket-Protocol" value={proto.transport?.early_data_header_name || ''} onChange={e => setTransport('early_data_header_name', e.target.value)} />
                  </>
                )}
                {transportType === 'grpc' && (
                  <>
                    <label>服务名称 (service_name)</label>
                    <input className="f-input mono" value={proto.transport?.service_name || ''} onChange={e => setTransport('service_name', e.target.value)} />
                  </>
                )}
                {transportType === 'xhttp' && (
                  <>
                    <label>模式 (mode)</label>
                    <select className="f-input" value={proto.transport?.mode || 'auto'} onChange={e => setTransport('mode', e.target.value)}>
                      {XHTTP_MODES.map(s => <option key={s} value={s}>{s}</option>)}
                    </select>
                  </>
                )}
              </div>
            </>
          )}

          {/* ─── TLS ─── */}
          {hasTLSSection && (
            <>
              <div className="section-title">TLS 安全设置</div>
              <div className="form-grid">
                {form.protocol !== 'trojan' && (
                  <>
                    <label>启用 TLS</label>
                    <input type="checkbox" className="f-check" checked={!!proto.tls} onChange={e => setProtoField('tls', e.target.checked)} />
                  </>
                )}
                {form.protocol === 'trojan' && (
                  <div className="edit-note full">Trojan 必须使用 TLS。</div>
                )}
                {!!proto.tls && (
                  <>
                    <label>SNI (server_name)</label>
                    <input className="f-input mono" value={proto.sni || ''} onChange={e => setProtoField('sni', e.target.value)} />
                    <label>ALPN (逗号分隔)</label>
                    <input className="f-input mono" placeholder="h2, http/1.1" value={alpnText} onChange={e => setProtoField('alpn', e.target.value)} />
                    <label>uTLS 指纹 (fingerprint)</label>
                    <input className="f-input mono" placeholder="chrome / firefox / safari / ios …" value={proto.fingerprint || ''} onChange={e => setProtoField('fingerprint', e.target.value)} />
                    <label>跳过证书验证 (insecure)</label>
                    <input type="checkbox" className="f-check" checked={!!proto.insecure} onChange={e => setProtoField('insecure', e.target.checked)} />
                    <label>ECH 配置 (ech.config)</label>
                    <input className="f-input mono" value={proto.ech_config || ''} onChange={e => setProtoField('ech_config', e.target.value)} />
                  </>
                )}
                {isReality && (
                  <>
                    <div className="edit-note full" style={{ color: '#3ddc84' }}>Reality 模式</div>
                    <label>公钥 (public_key / pbk)</label>
                    <input className="f-input mono" value={proto.public_key || ''} onChange={e => setProtoField('public_key', e.target.value)} />
                    <label>短 ID (short_id / sid)</label>
                    <input className="f-input mono" value={proto.short_id || ''} onChange={e => setProtoField('short_id', e.target.value)} />
                  </>
                )}
              </div>
            </>
          )}

          {/* 固定 TLS 协议的 TLS 子集 */}
          {hasFixedTLS && (
            <>
              <div className="section-title">TLS 安全设置 ({form.protocol === 'tuic' ? 'TLS 必需' : 'QUIC / TLS 必需'})</div>
              <div className="form-grid">
                <label>SNI (server_name)</label>
                <input className="f-input mono" value={proto.sni || ''} onChange={e => setProtoField('sni', e.target.value)} />
                <label>ALPN (逗号分隔)</label>
                <input className="f-input mono" value={alpnText} onChange={e => setProtoField('alpn', e.target.value)} />
                <label>跳过证书验证 (insecure)</label>
                <input type="checkbox" className="f-check" checked={!!proto.insecure} onChange={e => setProtoField('insecure', e.target.checked)} />
                {form.protocol === 'hysteria2' && (
                  <>
                    <label>ECH 配置 (ech.config)</label>
                    <input className="f-input mono" value={proto.ech_config || ''} onChange={e => setProtoField('ech_config', e.target.value)} />
                  </>
                )}
              </div>
            </>
          )}
        </div>

        <div className="modal-footer">
          <button className="btn-cancel" onClick={onClose} disabled={saving}>取消</button>
          <button className="btn-primary" onClick={handleSave} disabled={saving}>
            {saving ? '保存中…' : '保存'}
          </button>
        </div>
      </div>
    </div>
  )
}
