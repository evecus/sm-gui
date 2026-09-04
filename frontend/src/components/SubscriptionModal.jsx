import React, { useState } from 'react'
import { api } from '../lib/wails'
import './Modal.css'
import './SubscriptionModal.css'

export default function SubscriptionModal({ subscriptions, groupId, onFetch, onRefreshed, onClose, loading }) {
  const [url, setUrl] = useState('')
  const [refreshing, setRefreshing] = useState(null)

  const handleRefresh = async (subUrl) => {
    setRefreshing(subUrl)
    try {
      const count = await api.RefreshSubscription(subUrl, groupId)
      if (onRefreshed) await onRefreshed()
      alert(`订阅更新成功，获取 ${count} 个节点`)
    } catch (e) {
      alert('更新失败: ' + e)
    } finally {
      setRefreshing(null)
    }
  }

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
                    className="sub-btn refresh"
                    onClick={() => handleRefresh(sub)}
                    disabled={refreshing === sub || loading}
                  >
                    {refreshing === sub ? '更新中…' : '↻ 更新'}
                  </button>
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
