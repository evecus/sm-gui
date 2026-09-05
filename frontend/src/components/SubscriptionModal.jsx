import React, { useState } from 'react'
import { api } from '../lib/wails'
import './Modal.css'
import './SubscriptionModal.css'

// 订阅管理弹窗：输入订阅链接 → 拉取成功后自动新建「订阅N」分组
//（名称/链接自动填充，不自动更新），拉取的节点放入该分组。
// 手动更新已移除——订阅的「更新」统一走分组右键菜单。
export default function SubscriptionModal({ subscriptions, onFetch, onClose, loading }) {
  const [url, setUrl] = useState('')

  const handleRemove = async (subUrl) => {
    await api.RemoveSubscription(subUrl)
    onClose()
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()} style={{ width: 540 }}>
        <div className="modal-header">
          <span className="modal-title">订阅管理</span>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="modal-body">
          {/* Existing subscriptions */}
          {subscriptions.length > 0 && (
            <div className="sub-list">
              <div className="sub-list-label">已添加的订阅</div>
              {subscriptions.map(sub => (
                <div key={sub} className="sub-item">
                  <span className="sub-url">{sub}</span>
                  <button
                    className="sub-btn remove"
                    onClick={() => handleRemove(sub)}
                    disabled={loading}
                  >
                    ⊗
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* Add new */}
          <div className="textarea-label" style={{ marginBottom: 6 }}>添加新订阅地址</div>
          <div className="sub-input-row">
            <input
              className="modal-input"
              type="url"
              placeholder="https://your-subscription-url..."
              value={url}
              onChange={e => setUrl(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && url.trim() && onFetch(url.trim())}
              autoFocus
            />
          </div>
          <div className="modal-hint" style={{ marginTop: 8 }}>
            拉取成功后将自动新建「订阅N」分组并放入节点；更新请使用分组右键菜单中的「更新」
          </div>
        </div>
        <div className="modal-footer">
          <button className="btn-cancel" onClick={onClose} disabled={loading}>关闭</button>
          <button
            className="btn-primary"
            onClick={() => onFetch(url.trim())}
            disabled={!url.trim() || loading}
          >
            {loading ? '拉取中…' : '拉取订阅'}
          </button>
        </div>
      </div>
    </div>
  )
}
