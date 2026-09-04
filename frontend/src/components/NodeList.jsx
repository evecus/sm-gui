import React, { useState, useEffect, useRef, useCallback } from 'react'
import './NodeList.css'
import EditNodeModal from './EditNodeModal'
import { api } from '../lib/wails'

// 每种协议一个专属颜色(徽章)
const PROTOCOL_COLORS = {
  vmess:     '#5b7cf6',
  vless:     '#3ddc84',
  trojan:    '#f59e0b',
  ss:        '#e879f9',
  hysteria:  '#fb7185',
  hysteria2: '#f05252',
  tuic:      '#22d3ee',
  socks:     '#94a3b8',
  http:      '#a3e635',
  anytls:    '#34d399',
  ssr:       '#f97316',
  wireguard: '#60a5fa',
  ssh:       '#c084fc',
  shadowtls: '#2dd4bf',
}

const PROTOCOL_LABELS = {
  vmess:     'VMess',
  vless:     'VLESS',
  trojan:    'Trojan',
  ss:        'SS',
  hysteria:  'Hy1',
  hysteria2: 'Hy2',
  tuic:      'TUIC',
  socks:     'SOCKS',
  http:      'HTTP',
  anytls:    'AnyTLS',
  ssr:       'SSR',
  wireguard: 'WG',
  ssh:       'SSH',
  shadowtls: 'STLS',
}

const TRANSPORT_LABELS = {
  ws: 'ws',
  http: 'h2',
  grpc: 'grpc',
  httpupgrade: 'upg',
  quic: 'quic',
  xhttp: 'xhttp',
}

// 从节点数据中提取传输层与 TLS 信息(兼容结构化配置与 raw sing-box 出站)
function getNodeMeta(node) {
  const cfg = node.vmess || node.vless || node.trojan
  let transport = cfg?.transport?.type || ''
  let tls = !!cfg?.tls
  let reality = !!node.vless?.public_key
  let ech = !!(node.vmess?.ech_config || node.vless?.ech_config || node.trojan?.ech_config)
  let utls = !!cfg?.fingerprint

  if (!cfg && node.raw_outbound) {
    const raw = node.raw_outbound
    transport = raw.transport?.type || ''
    const t = raw.tls
    tls = !!t?.enabled
    reality = !!t?.reality?.enabled
    ech = !!t?.ech?.enabled
    utls = !!t?.utls?.enabled
  }
  // 这些协议强制 TLS/QUIC
  if (['hysteria', 'hysteria2', 'tuic', 'anytls', 'trojan', 'shadowtls'].includes(node.protocol)) {
    tls = true
  }
  return { transport, tls, reality, ech, utls }
}

export default function NodeList({ nodes, groups, activeGroupId, appliedId, onSelectGroup, onGroupsChanged, onApply, onDelete, onRefresh }) {
  const [nodeMenu, setNodeMenu] = useState(null)      // 节点右键菜单
  const [groupMenu, setGroupMenu] = useState(null)    // 分组右键菜单
  const [selectedId, setSelectedId] = useState(null)
  const [editingNode, setEditingNode] = useState(null)
  const [groupModal, setGroupModal] = useState(null)  // { mode: 'create'|'rename', group, afterID }
  const menuRef = useRef(null)

  // 只显示当前分组的节点(空 group_id 归入默认分组)
  const activeNodes = nodes.filter(n => (n.group_id || 'default') === activeGroupId)

  const closeMenus = useCallback(() => {
    setNodeMenu(null)
    setGroupMenu(null)
  }, [])

  useEffect(() => {
    const handler = (e) => {
      if (menuRef.current && !menuRef.current.contains(e.target)) {
        closeMenus()
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [closeMenus])

  // ─── 节点菜单动作 ───
  const withNodeMenu = (fn) => async () => {
    if (!nodeMenu) return
    const { node } = nodeMenu
    closeMenus()
    await fn(node)
  }

  const handleApply = withNodeMenu(async (node) => { await onApply(node.id) })
  const handleDelete = withNodeMenu(async (node) => { await onDelete(node.id) })
  const handleEdit = withNodeMenu(async (node) => { setEditingNode(node) })

  const handleExport = withNodeMenu(async (node) => {
    try {
      const uri = await api.ExportNodeURI(node.id)
      if (!uri) throw new Error('导出为空')
      try {
        await navigator.clipboard.writeText(uri)
      } catch (err) {
        // Wails WebView 兼容回退
        const ta = document.createElement('textarea')
        ta.value = uri
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
      }
      alert('分享链接已复制到剪贴板:\n' + uri)
    } catch (e) {
      alert('导出失败: ' + (e?.message || e))
    }
  })

  const handleMoveUp = withNodeMenu(async (node) => {
    try {
      await api.MoveNodeUp(node.id)
      await onRefresh()
    } catch (e) { /* 已在顶部等 */ }
  })

  const handleMoveDown = withNodeMenu(async (node) => {
    try {
      await api.MoveNodeDown(node.id)
      await onRefresh()
    } catch (e) { /* 已在底部等 */ }
  })

  // ─── 分组菜单动作 ───
  const withGroupMenu = (fn) => async () => {
    if (!groupMenu) return
    const { group } = groupMenu
    closeMenus()
    await fn(group)
  }

  const handleGroupCreate = withGroupMenu(async (group) => {
    setGroupModal({ mode: 'create', group, afterID: group.id })
  })

  const handleGroupRename = withGroupMenu(async (group) => {
    setGroupModal({ mode: 'rename', group })
  })

  const handleGroupDelete = withGroupMenu(async (group) => {
    if (!window.confirm(`确认删除分组「${group.name}」？\n该分组内的节点将移入「默认」分组。`)) return
    try {
      await api.DeleteGroup(group.id)
      await onGroupsChanged()
    } catch (e) {
      alert('删除分组失败: ' + (e?.message || e))
    }
  })

  // 分组命名弹窗确认
  const handleGroupModalConfirm = async (name) => {
    if (!groupModal) return
    try {
      if (groupModal.mode === 'create') {
        await api.AddGroup(name, groupModal.afterID)
      } else {
        await api.RenameGroup(groupModal.group.id, name)
      }
      setGroupModal(null)
      await onGroupsChanged()
    } catch (e) {
      alert((groupModal.mode === 'create' ? '新建分组失败: ' : '重命名失败: ') + (e?.message || e))
    }
  }

  // 上下移边界: 基于当前分组列表内位置
  const groupIndex = nodeMenu ? activeNodes.findIndex(n => n.id === nodeMenu.node.id) : -1
  const atTop = groupIndex <= 0
  const atBottom = groupIndex < 0 || groupIndex >= activeNodes.length - 1

  const activeGroup = groups.find(g => g.id === activeGroupId)

  return (
    <div className="node-list-wrap">
      {/* 分组页签栏(横向) */}
      <div className="group-tabs-bar">
        {groups.map(g => (
          <button
            key={g.id}
            className={`group-tab${g.id === activeGroupId ? ' active' : ''}${g.is_default ? ' builtin' : ''}`}
            onClick={() => { onSelectGroup(g.id); closeMenus() }}
            onContextMenu={(e) => {
              e.preventDefault()
              onSelectGroup(g.id)
              setGroupMenu({ x: e.clientX, y: e.clientY, group: g })
              setNodeMenu(null)
            }}
            title={g.is_default ? '默认分组（不可删除/重命名）' : '右键管理分组'}
          >
            <span className="group-tab-name">{g.name}</span>
            <span className="group-tab-count">
              {nodes.filter(n => (n.group_id || 'default') === g.id).length}
            </span>
          </button>
        ))}
      </div>

      {/* 列表 */}
      {activeNodes.length === 0 ? (
        <div className="node-list-empty">
          <div className="empty-icon">◈</div>
          <div className="empty-title">「{activeGroup?.name || '默认'}」分组暂无节点</div>
          <div className="empty-desc">点击上方「导入节点」或「订阅」，获取的节点将导入当前分组</div>
        </div>
      ) : (
        <div className="node-list">
          {activeNodes.map(node => (
            <NodeRow
              key={node.id}
              node={node}
              applied={node.id === appliedId}
              selected={selectedId === node.id}
              onClick={() => { setSelectedId(node.id); closeMenus() }}
              onContextMenu={(e) => {
                e.preventDefault()
                setSelectedId(node.id)
                setNodeMenu({ x: e.clientX, y: e.clientY, node })
                setGroupMenu(null)
              }}
            />
          ))}
        </div>
      )}

      {/* 节点右键菜单 */}
      {nodeMenu && (
        <div ref={menuRef} className="context-menu" style={{ left: nodeMenu.x, top: nodeMenu.y }}>
          <div className="ctx-node-name">{nodeMenu.node.name}</div>
          <div className="ctx-divider" />
          <button className="ctx-item primary" onClick={handleApply}>
            <span>▶</span> 应用此节点
          </button>
          <button className="ctx-item" onClick={handleEdit}>
            <span>✎</span> 编辑节点
          </button>
          <button className="ctx-item" onClick={handleExport}>
            <span>⧉</span> 导出分享链接
          </button>
          <div className="ctx-divider" />
          <button className={`ctx-item${atTop ? ' disabled' : ''}`} onClick={handleMoveUp} disabled={atTop}>
            <span>↑</span> 上移
          </button>
          <button className={`ctx-item${atBottom ? ' disabled' : ''}`} onClick={handleMoveDown} disabled={atBottom}>
            <span>↓</span> 下移
          </button>
          <div className="ctx-divider" />
          <button className="ctx-item danger" onClick={handleDelete}>
            <span>⊗</span> 删除节点
          </button>
        </div>
      )}

      {/* 分组右键菜单 */}
      {groupMenu && (
        <div ref={menuRef} className="context-menu" style={{ left: groupMenu.x, top: groupMenu.y }}>
          <div className="ctx-node-name">分组：{groupMenu.group.name}</div>
          <div className="ctx-divider" />
          <button className="ctx-item primary" onClick={handleGroupCreate}>
            <span>⊞</span> 新建分组
          </button>
          <button
            className={`ctx-item${groupMenu.group.is_default ? ' disabled' : ''}`}
            onClick={handleGroupRename}
            disabled={groupMenu.group.is_default}
          >
            <span>✎</span> 重命名
          </button>
          <button
            className={`ctx-item danger${groupMenu.group.is_default ? ' disabled' : ''}`}
            onClick={handleGroupDelete}
            disabled={groupMenu.group.is_default}
          >
            <span>⊗</span> 删除分组
          </button>
        </div>
      )}

      {/* 分组命名弹窗 */}
      {groupModal && (
        <GroupNameModal
          mode={groupModal.mode}
          initialName={groupModal.mode === 'rename' ? groupModal.group.name : ''}
          onConfirm={handleGroupModalConfirm}
          onClose={() => setGroupModal(null)}
        />
      )}

      {/* 节点编辑弹窗 */}
      {editingNode && (
        <EditNodeModal
          node={editingNode}
          onSaved={async () => { setEditingNode(null); await onRefresh() }}
          onClose={() => setEditingNode(null)}
        />
      )}
    </div>
  )
}

function NodeRow({ node, applied, selected, onClick, onContextMenu }) {
  const color = PROTOCOL_COLORS[node.protocol] || '#9ea3c0'
  const label = PROTOCOL_LABELS[node.protocol] || node.protocol?.toUpperCase()
  const meta = getNodeMeta(node)

  return (
    <div
      className={`node-row${applied ? ' applied' : ''}${selected ? ' selected' : ''}`}
      onClick={onClick}
      onContextMenu={onContextMenu}
    >
      <span className="node-proto-badge" style={{ color, borderColor: color + '40', background: color + '12' }}>
        {label}
      </span>
      <span className="node-name">
        {node.name || '未命名节点'}
        {applied && <span className="applied-chip" title="此节点已应用到配置文件">已应用</span>}
      </span>
      {/* 传输层 / TLS 标识(参考 v2rayN 的 流类型/安全 列) */}
      {meta.transport && <span className="meta-chip">{TRANSPORT_LABELS[meta.transport] || meta.transport}</span>}
      {meta.reality ? (
        <span className="meta-chip tls">REALITY</span>
      ) : meta.tls ? (
        <span className={`meta-chip tls${meta.ech ? ' ech' : ''}`}>{meta.ech ? 'TLS·ECH' : 'TLS'}</span>
      ) : null}
      {meta.utls && !meta.reality && <span className="meta-chip utls">uTLS</span>}
      <span className="node-addr">{node.address}:{node.port}</span>
    </div>
  )
}

// 分组命名弹窗(新建 / 重命名)
function GroupNameModal({ mode, initialName, onConfirm, onClose }) {
  const [name, setName] = useState(initialName)
  const [saving, setSaving] = useState(false)

  const handleConfirm = async () => {
    if (!name.trim()) return
    setSaving(true)
    await onConfirm(name.trim())
    setSaving(false)
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal group-name-modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">{mode === 'create' ? '新建分组' : '重命名分组'}</span>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="modal-body">
          <input
            className="modal-input"
            autoFocus
            placeholder="输入分组名称…"
            value={name}
            onChange={e => setName(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && name.trim() && !saving && handleConfirm()}
          />
        </div>
        <div className="modal-footer">
          <button className="btn-cancel" onClick={onClose} disabled={saving}>取消</button>
          <button className="btn-primary" onClick={handleConfirm} disabled={!name.trim() || saving}>
            {saving ? '保存中…' : '确认'}
          </button>
        </div>
      </div>
    </div>
  )
}
