import React, { useState, useEffect, useCallback, useRef } from 'react'
import { api } from './lib/wails'
import NodeList from './components/NodeList'
import SubscriptionModal from './components/SubscriptionModal'
import ImportModal from './components/ImportModal'
import LogPanel from './components/LogPanel'
import ConfigBar from './components/ConfigBar'
import BottomBar from './components/BottomBar'
import SettingsPanel from './components/SettingsPanel'
import Toast from './components/Toast'
import './App.css'

export default function App() {
  const [nodes, setNodes] = useState([])
  const [groups, setGroups] = useState([{ id: 'default', name: '默认', is_default: true }])
  const [activeGroupId, setActiveGroupId] = useState('default') // 启动时显示 默认 分组
  const [settings, setSettings] = useState({ config_path: '', subscriptions: [] })
  const [configFiles, setConfigFiles] = useState([])
  const [singboxStatus, setSingboxStatus] = useState({ running: false, pid: 0 })
  const [tunEnabled, setTunEnabled] = useState(false)
  const [proxyEnabled, setProxyEnabled] = useState(false)
  const [activeTab, setActiveTab] = useState('nodes') // nodes | log | settings
  const [showSubModal, setShowSubModal] = useState(false)
  const [showImportModal, setShowImportModal] = useState(false)
  const [appliedId, setAppliedId] = useState('')
  const [pollMs, setPollMs] = useState(2000)
  const [toast, setToast] = useState(null)
  const [loading, setLoading] = useState(false)
  const pollRef = useRef(null)

  // 当前内核名称（用于 UI 文案）
  const coreName = settings.core === 'mihomo' ? 'mihomo' : 'sing-box'

  const showToast = useCallback((msg, type = 'info') => {
    setToast({ msg, type, id: Date.now() })
  }, [])

  const loadNodes = useCallback(async () => {
    try {
      const n = await api.GetNodes()
      setNodes(n || [])
    } catch (e) {
      showToast('加载节点失败: ' + e, 'error')
    }
  }, [showToast])

  const loadGroups = useCallback(async () => {
    try {
      const g = await api.GetGroups()
      if (g && g.length > 0) setGroups(g)
      // 当前选中分组若已被删除, 回到默认分组
      setActiveGroupId(prev => (g && g.some(x => x.id === prev)) ? prev : 'default')
    } catch (e) { /* ignore */ }
  }, [])

  const loadSettings = useCallback(async () => {
    try {
      const s = await api.GetSettings()
      setSettings(s || { config_path: '', subscriptions: [] })
      if (s?.poll_interval_ms) setPollMs(s.poll_interval_ms)
      // 回显持久化的 TUN 开关状态（custom 模式下与配置文件保持同步）
      setTunEnabled(!!s?.tun_enabled)
    } catch (e) { /* ignore */ }
  }, [])

  const loadApplied = useCallback(async () => {
    try {
      const id = await api.GetAppliedNodeID()
      setAppliedId(id || '')
    } catch (e) { /* ignore */ }
  }, [])

  const loadConfigFiles = useCallback(async () => {
    try {
      const f = await api.GetConfigFiles()
      setConfigFiles(f || [])
    } catch (e) { /* ignore */ }
  }, [])

  // 下拉选择 configs 目录中的配置文件
const handleSelectConfig = useCallback(async (name) => {
if (!name) return
try {
const full = await api.SelectConfigFile(name)
showToast('已切换配置: ' + name, 'success')
await loadSettings()
await loadApplied()
return full
} catch (e) {
showToast('切换配置失败: ' + (e?.message || e), 'error')
    }
  }, [loadSettings, loadApplied, showToast])

  const handleOpenConfigsDir = useCallback(async () => {
    try { await api.OpenConfigsDir() } catch (e) { /* ignore */ }
  }, [])

  useEffect(() => {
    loadNodes()
    loadGroups()
    loadSettings()
    loadApplied()
    loadConfigFiles()
  }, [loadNodes, loadGroups, loadSettings, loadApplied, loadConfigFiles])

  // 状态轮询（间隔来自设置，可在设置页修改后即时生效）
  useEffect(() => {
    pollRef.current = setInterval(async () => {
      try {
        const s = await api.GetSingBoxStatus()
        setSingboxStatus(s || { running: false })
      } catch (e) { /* ignore */ }
    }, pollMs)
    return () => clearInterval(pollRef.current)
  }, [pollMs])

// ─── Actions ──────────────────────────────────────────────────────────────

  const handleClearNodes = async () => {
    try {
      if (!window.confirm('确认清空所有节点？')) return
      await api.ClearNodes()
      await loadNodes()
      showToast('已清空所有节点', 'success')
    } catch (e) {
      showToast('清空失败: ' + e, 'error')
    }
  }

  const handleImport = async (content) => {
    setLoading(true)
    try {
      const count = await api.ImportNodes(content, activeGroupId)
      await loadNodes()
      showToast(`成功导入 ${count} 个节点`, 'success')
      setShowImportModal(false)
    } catch (e) {
      showToast('导入失败: ' + e, 'error')
    } finally {
      setLoading(false)
    }
  }

  const handleFetchSub = async (url) => {
    setLoading(true)
    try {
      const count = await api.FetchSubscription(url, activeGroupId)
      await loadNodes()
      await loadSettings()
      showToast(`订阅更新成功，获取 ${count} 个节点`, 'success')
      setShowSubModal(false)
    } catch (e) {
      showToast('订阅拉取失败: ' + e, 'error')
    } finally {
      setLoading(false)
    }
  }

  const handleApplyNode = async (id) => {
    try {
      await api.ApplyNode(id)
      setAppliedId(id)
      showToast('节点已应用到配置文件', 'success')
    } catch (e) {
      showToast('应用节点失败: ' + e, 'error')
    }
  }

  const handleSaveSettings = async (s) => {
    try {
      const coreChanged = s.core !== settings.core
      await api.SaveSettings(s)
      // 内核切换后后端会把 config_path 切到新内核记忆的配置文件（可能为空），
      // 所以以后端最新设置为准
      const fresh = await api.GetSettings()
      // 重启受影响的开关，让设置即时生效：
      // 核心在跑则先停 → 重写 TUN/系统代理配置 → 再拉起核心
      const wasRunning = singboxStatus.running
      if (wasRunning) { await api.StopSingBox().catch(() => {}) }
      // 切换内核后可能尚未选择新内核的配置文件，跳过重建（内置路由模式无配置文件，不需跳过）
      const isBuiltin = fresh.routing_mode && fresh.routing_mode !== 'custom'
      const hasConfig = !!fresh.config_path || isBuiltin
      if (hasConfig) {
        if (tunEnabled) {
          await api.DisableTun()
          await api.EnableTun()
        }
        if (proxyEnabled) {
          await api.DisableSystemProxy()
          await api.EnableSystemProxy()
        }
      }
      if (wasRunning && hasConfig) { await api.StartSingBox() }
      await loadSettings()
      await loadApplied()
      await loadConfigFiles()
      showToast(wasRunning && hasConfig ? '设置已保存，核心已重启生效' : '设置已保存', 'success')
      if (coreChanged) {
        showToast(`已切换内核: ${s.core === 'mihomo' ? 'mihomo' : 'sing-box'}${fresh.config_path ? '' : '（请先选择该内核的配置文件）'}`, 'info')
      }
    } catch (e) {
      showToast('保存设置失败: ' + (e?.message || e), 'error')
    }
  }

  const handleDeleteNode = async (id) => {
    try {
      await api.DeleteNode(id)
      setNodes(ns => ns.filter(n => n.id !== id))
    } catch (e) {
      showToast('删除失败: ' + e, 'error')
    }
  }

  // ─── Bottom bar toggles ───────────────────────────────────────────────────

  const handleToggleTun = async (on) => {
    try {
      if (on) {
        await api.EnableTun()
        setTunEnabled(true)
        showToast('已启用 TUN 模式', 'success')
      } else {
        await api.DisableTun()
        setTunEnabled(false)
        showToast('已关闭 TUN 模式', 'info')
      }
    } catch (e) {
      showToast('TUN 操作失败: ' + e, 'error')
    }
  }

  const handleToggleProxy = async (on) => {
    try {
      if (on) {
        await api.EnableSystemProxy()
        setProxyEnabled(true)
        showToast(`已启用系统代理 (${settings.proxy_listen || '127.0.0.1'}:${settings.proxy_port || 2080})`, 'success')
      } else {
        await api.DisableSystemProxy()
        setProxyEnabled(false)
        showToast('已关闭系统代理', 'info')
      }
    } catch (e) {
      showToast('系统代理操作失败: ' + e, 'error')
    }
  }

  const handleToggleSingbox = async (on) => {
    try {
      if (on) {
        await api.StartSingBox()
        showToast(`${coreName} 已启动`, 'success')
      } else {
        await api.StopSingBox()
        showToast(`${coreName} 已停止`, 'info')
      }
    } catch (e) {
      showToast(`${coreName} 操作失败: ` + e, 'error')
    }
  }

  // ─── Render ───────────────────────────────────────────────────────────────

  return (
    <div className="app">
      {/* Title bar */}
      <div className="titlebar" style={{ '--wails-draggable': 'drag' }}>
        <div className="titlebar-tabs">
          <button
            className={`tab-btn${activeTab === 'nodes' ? ' active' : ''}`}
            onClick={() => setActiveTab('nodes')}
          >节点列表</button>
          <button
            className={`tab-btn${activeTab === 'log' ? ' active' : ''}`}
            onClick={() => setActiveTab('log')}
          >
            运行日志
            {singboxStatus.running && <span className="tab-badge" />}
          </button>
          <button
            className={`tab-btn${activeTab === 'settings' ? ' active' : ''}`}
            onClick={() => setActiveTab('settings')}
            title="设置"
          >
            ⚙ 设置
          </button>
        </div>
        <div className="titlebar-status">
          {singboxStatus.running
            ? <span className="status-dot running" title={`PID: ${singboxStatus.pid}`} />
            : <span className="status-dot stopped" />
          }
          <span className="status-text">
            {singboxStatus.running ? `运行中 #${singboxStatus.pid}` : '未运行'}
          </span>
        </div>
      </div>

{/* Config bar — 仅节点列表页面专属，其他页面不显示 */}
{activeTab === 'nodes' && (
<ConfigBar
core={settings.core}
configPath={settings.config_path}
routingMode={settings.routing_mode || 'custom'}
configFiles={configFiles}
onSelectConfig={handleSelectConfig}
onRefreshConfigs={loadConfigFiles}
onOpenConfigsDir={handleOpenConfigsDir}
onImport={() => setShowImportModal(true)}
onSubscription={() => setShowSubModal(true)}
onClear={handleClearNodes}
nodeCount={nodes.length}
        loading={loading}
      />
      )}

      {/* Main content */}
      <div className="main-content">
        {activeTab === 'nodes' && (
          <NodeList
            nodes={nodes}
            groups={groups}
            activeGroupId={activeGroupId}
            appliedId={appliedId}
            onSelectGroup={setActiveGroupId}
            onGroupsChanged={async () => { await loadGroups(); await loadNodes() }}
            onApply={handleApplyNode}
            onDelete={handleDeleteNode}
            onRefresh={async () => { await loadNodes(); await loadApplied() }}
          />
        )}
        {activeTab === 'log' && (
          <LogPanel />
        )}
        {activeTab === 'settings' && (
          <SettingsPanel
            settings={settings}
            onSave={handleSaveSettings}
          />
        )}
      </div>

      {/* Bottom bar */}
      <BottomBar
        tunEnabled={tunEnabled}
        proxyEnabled={proxyEnabled}
        singboxRunning={singboxStatus.running}
        proxyAddr={`127.0.0.1:${settings.proxy_port || 2080}`}
        coreName={coreName}
        onToggleTun={handleToggleTun}
        onToggleProxy={handleToggleProxy}
        onToggleSingbox={handleToggleSingbox}
      />

      {/* Modals */}
      {showImportModal && (
        <ImportModal
          onConfirm={handleImport}
          onClose={() => setShowImportModal(false)}
          loading={loading}
        />
      )}
      {showSubModal && (
        <SubscriptionModal
          subscriptions={settings.subscriptions || []}
          groupId={activeGroupId}
          onFetch={handleFetchSub}
          onClose={() => setShowSubModal(false)}
          loading={loading}
        />
      )}
      {/* Toast */}
      {toast && <Toast key={toast.id} message={toast.msg} type={toast.type} />}
    </div>
  )
}
