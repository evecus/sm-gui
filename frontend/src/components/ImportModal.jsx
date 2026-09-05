import React, { useState } from 'react'
import './Modal.css'

export default function ImportModal({ onConfirm, onClose, loading }) {
  const [content, setContent] = useState('')

  const handlePaste = async () => {
    try {
      const text = await navigator.clipboard.readText()
      setContent(text)
    } catch (e) {
      // ignore
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">导入节点</span>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="modal-body">
          <div className="modal-desc">
            粘贴节点链接（每行一个）或 base64 编码内容<br />
            支持：vmess:// vless:// trojan:// ss:// hysteria2:// hysteria:// tuic://<br />
            socks:// anytls:// ssr:// wireguard:// 及 sing-box / Clash 完整配置
          </div>
          <div className="textarea-toolbar">
            <span className="textarea-label">节点内容</span>
            <button className="link-btn" onClick={handlePaste}>从剪贴板粘贴</button>
          </div>
          <textarea
            className="modal-textarea"
            value={content}
            onChange={e => setContent(e.target.value)}
            placeholder="vmess://...&#10;vless://...&#10;trojan://..."
            rows={10}
            autoFocus
          />
        </div>
        <div className="modal-footer">
          <button className="btn-cancel" onClick={onClose} disabled={loading}>取消</button>
          <button
            className="btn-primary"
            onClick={() => onConfirm(content)}
            disabled={!content.trim() || loading}
          >
            {loading ? '导入中…' : '导入'}
          </button>
        </div>
      </div>
    </div>
  )
}
