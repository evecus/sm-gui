import React from 'react'
import './BottomBar.css'

export default function BottomBar({
  tunEnabled, proxyEnabled, singboxRunning, proxyAddr, coreName = 'sing-box',
  onToggleTun, onToggleProxy, onToggleSingbox
}) {
  return (
    <div className="bottom-bar">
      <ToggleButton
        label="TUN 模式"
        icon="⬡"
        enabled={tunEnabled}
        onToggle={onToggleTun}
        activeColor="var(--green)"
        desc={tunEnabled ? '虚拟网卡已启用' : '点击启用 TUN'}
      />
      <div className="bottom-divider" />
      <ToggleButton
        label="系统代理"
        icon="⇌"
        enabled={proxyEnabled}
        onToggle={onToggleProxy}
        activeColor="var(--accent)"
        desc={proxyEnabled ? (proxyAddr || '系统代理已启用') : '点击设置系统代理'}
      />
      <div className="bottom-divider" />
      <ToggleButton
        label="启动核心"
        icon="▶"
        enabled={singboxRunning}
        onToggle={onToggleSingbox}
        activeColor="var(--yellow)"
        desc={singboxRunning ? `${coreName} 运行中` : `点击启动 ${coreName}`}
        isPrimary
      />
    </div>
  )
}

function ToggleButton({ label, icon, enabled, onToggle, activeColor, desc, isPrimary }) {
  const handleClick = () => onToggle(!enabled)

  return (
    <button
      className={`toggle-btn${enabled ? ' enabled' : ''}${isPrimary ? ' primary' : ''}`}
      style={enabled ? { '--active-color': activeColor } : {}}
      onClick={handleClick}
    >
      <div className="toggle-top">
        <span className="toggle-icon">{icon}</span>
        <span className="toggle-label">{label}</span>
        <div className={`toggle-switch${enabled ? ' on' : ''}`} style={enabled ? { background: activeColor } : {}}>
          <div className="toggle-knob" />
        </div>
      </div>
      <div className="toggle-desc">{desc}</div>
    </button>
  )
}
