import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'

// 系统深浅色切换时实时更新 <html data-theme>（index.html 已在启动时设置初始值）
try {
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  mq.addEventListener('change', (e) => {
    document.documentElement.dataset.theme = e.matches ? 'dark' : 'light'
  })
} catch (e) { /* ignore */ }

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
