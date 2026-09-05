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

export default function NodeList({ nodes, groups, activeGroupId, appliedId, onSelectGroup, onGroupsChanged, onApply, onUnapply, onDelete, onRefresh }) {
  const [nodeMenu, setNodeMenu] = useState(null)      // 节点右键菜单
  const [groupMenu, setGroupMenu] = useState(null)    // 分组右键菜单
  const [selectedId, setSelectedId] = useState(null)
  const [editingNode, setEditingNode] = useState(null)
  const [groupModal, setGroupModal] = useState(null)  // { mode: 'create'|'edit', group }
  const [testResults, setTestResults] = useState({})  // nodeId -> {status:'testing'|'done'|'error', latency, speed}
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
  const handleUnapply = withNodeMenu(async () => { await onUnapply() })
  const handleDelete = withNodeMenu(async (node) => { await onDelete(node.id) })
  const handleEdit = withNodeMenu(async (node) => { setEditingNode(node) })

  // ─── 节点测试（延迟 / 速度，逻辑参考 v2rayN：点击即测，结果显示在列表行）───
  const setTestState = (id, state) => setTestResults(prev => ({ ...prev, [id]: state }))

  const handleTestLatency = withNodeMenu(async (node) => {
    setTestState(node.id, { status: 'testing' })
    try {
      const ms = await api.TestNodeLatency(node.id)
      setTestState(node.id, { status: 'done', latency: ms })
    } catch (e) {
      setTestState(node.id, { status: 'error', error: e?.message || String(e) })
    }
  })

  const handleTestSpeed = withNodeMenu(async (node) => {
    setTestState(node.id, { status: 'testing' })
    try {
      const mbps = await api.TestNodeSpeed(node.id)
      setTestState(node.id, { status: 'done', speed: mbps })
    } catch (e) {
      setTestState(node.id, { status: 'error', error: e?.message || String(e) })
    }
  })

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

  // 编辑分组：名称（默认分组置灰）+ 订阅链接 + 自动更新设置
  const handleGroupEdit = withGroupMenu(async (group) => {
    setGroupModal({ mode: 'edit', group })
  })

  // 更新分组订阅（仅设置了订阅链接的分组显示此按钮）
  const handleGroupRefresh = withGroupMenu(async (group) => {
    try {
      const count = await api.RefreshGroupSubscription(group.id)
      await onGroupsChanged()
      alert(`分组「${group.name}」订阅更新成功，共 ${count} 个节点`)
    } catch (e) {
      alert('订阅更新失败: ' + (e?.message || e))
    }
  })

  const handleGroupMove = (delta) => withGroupMenu(async (group) => {
    try {
      await api.MoveGroup(group.id, delta)
      await onGroupsChanged()
    } catch (e) {
      alert('移动分组失败: ' + (e?.message || e))
    }
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

  // 分组编辑弹窗确认（edit 模式：名称 + 订阅设置；create 模式：仅名称）
  const handleGroupModalConfirm = async (data) => {
    if (!groupModal) return
    try {
      if (groupModal.mode === 'create') {
        await api.AddGroup(data.name, groupModal.afterID)
      } else {
        await api.UpdateGroup(
          groupModal.group.id,
          data.name,
          data.subUrl,
          data.autoUpdate,
          data.intervalHours,
        )
      }
      setGroupModal(null)
      await onGroupsChanged()
    } catch (e) {
      alert((groupModal.mode === 'create' ? '新建分组失败: ' : '编辑分组失败: ') + (e?.message || e))
    }
  }

  // 上下移边界: 基于当前分组列表内位置
  const groupIndex = nodeMenu ? activeNodes.findIndex(n => n.id === nodeMenu.node.id) : -1
  const atTop = groupIndex <= 0
  const atBottom = groupIndex < 0 || groupIndex >= activeNodes.length - 1

  const activeGroup = groups.find(g => g.id === activeGroupId)
  // 分组在页签栏中的位置（决定左移/右移按钮的显示）
  const menuGroupIdx = groupMenu ? groups.findIndex(g => g.id === groupMenu.group.id) : -1
  const canMoveLeft = groupMenu && menuGroupIdx >= 2                       // 默认分组与其右侧第一个分组不可左移
  const canMoveRight = groupMenu && !groupMenu.group.is_default && menuGroupIdx < groups.length - 1

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
            title={g.is_default ? '默认分组（不可删除，名称固定）' : '右键管理分组'}
          >
            <span className="group-tab-name">{g.name}</span>
            {g.sub_url && <span className="group-tab-sub" title="已设置订阅链接">◉</span>}
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
              testResult={testResults[node.id]}
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
          {nodeMenu.node.id === appliedId ? (
            <button className="ctx-item" onClick={handleUnapply}>
              <span>✕</span> 取消应用
            </button>
          ) : (
            <button className="ctx-item primary" onClick={handleApply}>
              <span>▶</span> 应用
            </button>
          )}
          <button className="ctx-item" onClick={handleEdit}>
            <span>✎</span> 编辑
          </button>
          <button className="ctx-item" onClick={handleTestLatency}>
            <span>⏱</span> 测试真连接延迟
          </button>
          <button className="ctx-item" onClick={handleTestSpeed}>
            <span>⇣</span> 测试速度
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
          {/* 编辑：默认分组也可点击（名称输入框置灰，仅可改订阅设置） */}
          <button className="ctx-item" onClick={handleGroupEdit}>
            <span>✎</span> 编辑
          </button>
          {groupMenu.group.sub_url && (
            <button className="ctx-item" onClick={handleGroupRefresh}>
              <span>⟳</span> 更新
            </button>
          )}
          {canMoveLeft && (
            <button className="ctx-item" onClick={handleGroupMove(-1)}>
              <span>←</span> 左移
            </button>
          )}
          {canMoveRight && (
            <button className="ctx-item" onClick={handleGroupMove(1)}>
              <span>→</span> 右移
            </button>
          )}
          <button
            className={`ctx-item danger${groupMenu.group.is_default ? ' disabled' : ''}`}
            onClick={handleGroupDelete}
            disabled={groupMenu.group.is_default}
          >
            <span>⊗</span> 删除分组
          </button>
        </div>
      )}

      {/* 分组弹窗（新建 / 编辑） */}
      {groupModal && (
        <GroupEditModal
          mode={groupModal.mode}
          group={groupModal.group}
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

function NodeRow({ node, applied, selected, testResult, onClick, onContextMenu }) {
  const color = PROTOCOL_COLORS[node.protocol] || '#9ea3c0'
  const label = PROTOCOL_LABELS[node.protocol] || node.protocol?.toUpperCase()
  const meta = getNodeMeta(node)

  // 测试结果显示（延迟 / 速度），样式参考 v2rayN 列内数值
  let resultChip = null
  if (testResult?.status === 'testing') {
    resultChip = <span className="test-chip testing">测试中…</span>
  } else if (testResult?.status === 'error') {
    resultChip = <span className="test-chip fail" title={testResult.error}>失败</span>
  } else if (testResult?.status === 'done') {
    if (typeof testResult.latency === 'number') {
      const cls = testResult.latency < 300 ? 'good' : testResult.latency < 800 ? 'mid' : 'bad'
      resultChip = <span className={`test-chip ${cls}`}>{testResult.latency} ms</span>
    } else if (typeof testResult.speed === 'number') {
      const v = testResult.speed
      const cls = v > 20 ? 'good' : v > 5 ? 'mid' : 'bad'
      resultChip = <span className={`test-chip ${cls}`}>{v >= 1 ? v.toFixed(1) : v.toFixed(2)} Mbps</span>
    }
  }

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
      {resultChip}
    </div>
  )
}

// 分组弹窗（新建 / 编辑）
// 编辑模式：名称输入框（默认分组置灰）+ 订阅链接 + 自动更新开关 + 更新间隔。
// 订阅链接为空时自动更新设置无效（输入框禁用）。
function GroupEditModal({ mode, group, onConfirm, onClose }) {
  const isEdit = mode === 'edit'
  const isDefault = !!group?.is_default
  const [name, setName] = useState(isEdit ? (group?.name || '') : '')
  const [subUrl, setSubUrl] = useState(isEdit ? (group?.sub_url || '') : '')
  const [autoUpdate, setAutoUpdate] = useState(isEdit ? !!group?.auto_update : false)
  const [intervalHours, setIntervalHours] = useState(isEdit ? (group?.update_interval_hours || 24) : 24)
  const [saving, setSaving] = useState(false)

  const hasSub = subUrl.trim() !== ''
  const autoUpdateEnabled = hasSub          // 订阅链接为空 → 自动更新设置无效
  const intervalEnabled = hasSub && autoUpdate

  const handleConfirm = async () => {
    if (isEdit) {
      // 编辑模式：默认分组名称固定，不校验名称
      if (!isDefault && !name.trim()) return
    } else if (!name.trim()) {
      return
    }
    setSaving(true)
    try {
      await onConfirm({
        name: name.trim(),
        subUrl: subUrl.trim(),
        autoUpdate: autoUpdateEnabled && autoUpdate,
        intervalHours: intervalEnabled ? Math.max(1, parseInt(intervalHours, 10) || 0) : 0,
      })
    } finally {
      setSaving(false)
    }
  }

  const canConfirm = isEdit
    ? (isDefault || !!name.trim())
    : !!name.trim()

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal group-name-modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">{isEdit ? (isDefault ? '编辑默认分组' : '编辑分组') : '新建分组'}</span>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="modal-body">
          <label className="modal-field-label">分组名称</label>
          <input
            className="modal-input"
            autoFocus={!isEdit}
            placeholder="输入分组名称…"
            value={isDefault ? '默认' : name}
            disabled={isDefault}
            onChange={e => setName(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && canConfirm && !saving && handleConfirm()}
          />
          {isEdit && (
            <>
              <label className="modal-field-label">订阅链接（可选）</label>
              <input
                className="modal-input"
                placeholder="输入订阅链接 https://…"
                value={subUrl}
                onChange={e => setSubUrl(e.target.value)}
              />
              <label className={`modal-toggle${autoUpdateEnabled ? '' : ' disabled'}`}>
                <input
                  type="checkbox"
                  checked={autoUpdateEnabled && autoUpdate}
                  disabled={!autoUpdateEnabled}
                  onChange={e => setAutoUpdate(e.target.checked)}
                />
                <span>自动更新订阅</span>
              </label>
              <div className="modal-interval-row">
                <label className={`modal-field-label${intervalEnabled ? '' : ' disabled'}`}>自动更新间隔（小时）</label>
                <input
                  type="number"
                  min="1"
                  className="modal-input"
                  value={intervalHours}
                  disabled={!intervalEnabled}
                  onChange={e => setIntervalHours(e.target.value)}
                />
                {!hasSub && <div className="modal-hint">填写订阅链接后自动更新设置才会生效</div>}
              </div>
            </>
          )}
        </div>
        <div className="modal-footer">
          <button className="btn-cancel" onClick={onClose} disabled={saving}>取消</button>
          <button className="btn-primary" onClick={handleConfirm} disabled={!canConfirm || saving}>
            {saving ? '保存中…' : '确认'}
          </button>
        </div>
      </div>
    </div>
  )
}
