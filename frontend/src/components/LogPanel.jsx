import React, { useState, useEffect, useRef } from 'react'
import { api } from '../lib/wails'
import './LogPanel.css'

export default function LogPanel() {
  const [logs, setLogs] = useState([])
  const [autoScroll, setAutoScroll] = useState(true)
  const bottomRef = useRef(null)

  useEffect(() => {
    const fetch = async () => {
      try {
        const l = await api.GetSingBoxLog()
        setLogs(l || [])
      } catch (e) { /* ignore */ }
    }
    fetch()
    const timer = setInterval(fetch, 1000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, autoScroll])

  const handleClear = () => setLogs([])

  return (
    <div className="log-panel">
      <div className="log-toolbar">
        <span className="log-title">运行日志</span>
        <span className="log-count">{logs.length} 条</span>
        <div className="log-toolbar-right">
          <label className="log-autoscroll">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={e => setAutoScroll(e.target.checked)}
            />
            自动滚动
          </label>
          <button className="log-clear-btn" onClick={handleClear}>清空</button>
        </div>
      </div>
      <div className="log-body">
        {logs.length === 0 ? (
          <div className="log-empty">暂无日志，启动 sing-box 后日志将在此显示</div>
        ) : (
          logs.map((line, i) => (
            <div key={i} className={`log-line${getLogLevel(line)}`}>
              {line}
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}

function getLogLevel(line) {
  const lower = line.toLowerCase()
  if (lower.includes('error') || lower.includes('fatal')) return ' error'
  if (lower.includes('warn')) return ' warn'
  if (lower.includes('started') || lower.includes('已启动') || lower.includes('success')) return ' success'
  return ''
}
