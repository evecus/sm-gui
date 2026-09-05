import React, { useEffect, useState } from 'react'
import './Toast.css'

const ICONS = { success: '✓', error: '✕', info: 'ℹ', warning: '⚠' }

export default function Toast({ message, type = 'info' }) {
  const [visible, setVisible] = useState(true)

  useEffect(() => {
    const t1 = setTimeout(() => setVisible(false), 2800)
    return () => clearTimeout(t1)
  }, [])

  if (!visible) return null

  return (
    <div className={`toast toast-${type}`}>
      <span className="toast-icon">{ICONS[type] || ICONS.info}</span>
      <span className="toast-msg">{message}</span>
    </div>
  )
}
