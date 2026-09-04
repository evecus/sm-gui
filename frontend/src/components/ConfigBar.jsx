import React from 'react'
import './ConfigBar.css'

// 从完整路径提取文件名(用于回显当前选中的下拉项)
function basename(p) {
  if (!p) return ''
  return p.split(/[\\/]/).pop() || ''
}

// 内置路由配置项前缀（与后端 config.BuiltinPrefix 一致）
const BUILTIN_PREFIX = '内置配置：'

// 路由模式 → 内置下拉项显示名（与后端 builtinModeNames 一致）
const BUILTIN_NAMES = {
  bypass: '内置配置：绕过大陆',
  blacklist: '内置配置：GFW列表',
  global: '内置配置：全局代理',
}

export default function ConfigBar({ core = 'sing-box', configPath, routingMode = 'custom', configFiles = [], onSelectConfig, onRefreshConfigs, onOpenConfigsDir, onImport, onSubscription, onClear, nodeCount, loading }) {
  const selectedName = basename(configPath)
  // 内置模式激活时回显对应的内置配置项；否则回显选中的真实配置文件
  const activeBuiltin = BUILTIN_NAMES[routingMode] || ''
  const value = configFiles.includes(selectedName)
    ? selectedName
    : (activeBuiltin && configFiles.includes(activeBuiltin) ? activeBuiltin : '')
  const isMihomo = core === 'mihomo'
  const extHint = isMihomo ? 'yaml' : 'json'
  const realFiles = configFiles.filter(f => !f.startsWith(BUILTIN_PREFIX))
  const builtinFiles = configFiles.filter(f => f.startsWith(BUILTIN_PREFIX))

  return (
    <div className="config-bar">
      <div className="config-path-area" title={`配置文件来自程序同目录的 configs 文件夹（当前内核: ${core}）${configPath ? '\n当前: ' + configPath : activeBuiltin ? `\n当前: ${activeBuiltin}（模板合成，不修改 configs 目录）` : ''}`}>
        <span className="config-path-icon">⚙</span>
        <select
          className={`config-select${value ? ' has-value' : ''}`}
          value={value}
          onChange={e => onSelectConfig(e.target.value)}
        >
          <option value="">
            {configFiles.length > 0
              ? (value ? value : `— 选择 ${core} 配置文件 —`)
              : `configs 目录为空，请放入 ${extHint} 配置…`}
          </option>
          {realFiles.map(f => (
            <option key={f} value={f}>{f}</option>
          ))}
          {builtinFiles.length > 0 && (
            <optgroup label="内置路由配置（无需配置文件）">
              {builtinFiles.map(f => (
                <option key={f} value={f}>{f}</option>
              ))}
            </optgroup>
          )}
        </select>
        <button className="config-mini-btn" onClick={onRefreshConfigs} title="重新扫描 configs 目录">↻</button>
        <button className="config-mini-btn" onClick={onOpenConfigsDir} title="打开 configs 目录">⌂</button>
      </div>
      <div className="config-actions">
        <button className="action-btn" onClick={onImport} disabled={loading} title="从剪贴板或文本导入节点链接">
          <span className="btn-icon">⊕</span>
          导入节点
        </button>
        <button className="action-btn" onClick={onSubscription} disabled={loading} title="拉取/管理订阅">
          <span className="btn-icon">↻</span>
          订阅
        </button>
        <button className="action-btn danger" onClick={onClear} disabled={loading} title="清空所有节点">
          <span className="btn-icon">⊗</span>
          清空
        </button>
        <span className="node-count">{nodeCount} 个节点</span>
      </div>
    </div>
  )
}
