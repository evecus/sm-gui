// Bridge to Wails Go backend
// In production, window.go is injected by Wails runtime
// In dev, we mock it

const isWails = () => typeof window !== 'undefined' && window.go

export const api = {
  // Nodes
  GetNodes: () => call('GetNodes'),
  GetGroups: () => call('GetGroups'),
  AddGroup: (name, afterID) => call('AddGroup', name, afterID),
  RenameGroup: (id, name) => call('RenameGroup', id, name),
  DeleteGroup: (id) => call('DeleteGroup', id),
  ImportNodes: (content, groupID) => call('ImportNodes', content, groupID),
  FetchSubscription: (url, groupID) => call('FetchSubscription', url, groupID),
  ClearNodes: () => call('ClearNodes'),
  DeleteNode: (id) => call('DeleteNode', id),
  UpdateNode: (node) => call('UpdateNode', node),
  ApplyNode: (id) => call('ApplyNode', id),
  ExportNodeURI: (id) => call('ExportNodeURI', id),
  MoveNodeUp: (id) => call('MoveNodeUp', id),
  MoveNodeDown: (id) => call('MoveNodeDown', id),

// Settings
GetSettings: () => call('GetSettings'),
SaveSettings: (s) => call('SaveSettings', s),
GetConfigFiles: () => call('GetConfigFiles'),
SelectConfigFile: (name) => call('SelectConfigFile', name),
OpenConfigsDir: () => call('OpenConfigsDir'),
  GetSubscriptions: () => call('GetSubscriptions'),
  RemoveSubscription: (url) => call('RemoveSubscription', url),
  RefreshSubscription: (url, groupID) => call('RefreshSubscription', url, groupID),
  GetConfigPreview: () => call('GetConfigPreview'),
  GetAppliedNodeID: () => call('GetAppliedNodeID'),

  // TUN
  EnableTun: () => call('EnableTun'),
  DisableTun: () => call('DisableTun'),

  // System proxy
  EnableSystemProxy: () => call('EnableSystemProxy'),
  DisableSystemProxy: () => call('DisableSystemProxy'),

  // SingBox
  StartSingBox: () => call('StartSingBox'),
  StopSingBox: () => call('StopSingBox'),
  GetSingBoxStatus: () => call('GetSingBoxStatus'),
  GetSingBoxLog: () => call('GetSingBoxLog'),
}

async function call(method, ...args) {
  if (!isWails()) {
    return mockCall(method, ...args)
  }
  return window.go.main.App[method](...args)
}

// ─── Dev mock ─────────────────────────────────────────────────────────────────
let mockNodes = [
  { id: '1', name: '香港节点 01', protocol: 'vmess', address: 'hk1.example.com', port: 443, sub_url: 'https://sub.example.com/token' },
  { id: '2', name: '日本节点 01', protocol: 'vless', address: 'jp1.example.com', port: 8443, sub_url: '' },
  { id: '3', name: '新加坡 hysteria2', protocol: 'hysteria2', address: 'sg1.example.com', port: 1234, sub_url: '' },
  { id: '4', name: '美国 trojan', protocol: 'trojan', address: 'us1.example.com', port: 443, sub_url: '' },
  { id: '5', name: 'TUIC v5 SG', protocol: 'tuic', address: 'sg2.example.com', port: 8443, sub_url: 'https://sub.example.com/token' },
]
let mockSettings = {
  core: 'sing-box',
  config_path: 'C:\\Users\\user\\singbox\\config.json',
  routing_mode: 'custom',
  tun_enabled: false,
  subscriptions: ['https://sub.example.com/token'],
  proxy_listen: '127.0.0.1',
  proxy_port: 2080,
  exit_disable_proxy: true,
  tun_stack: 'gvisor',
  tun_mtu: 9000,
  tun_strict_route: true,
  sub_user_agent: 'clash.meta',
  sub_timeout_sec: 30,
  log_max_lines: 500,
  poll_interval_ms: 2000,
}
let mockAppliedNodeId = '2'
let mockGroups = [{ id: 'default', name: '默认', is_default: true }]
let mockStatus = { running: false, pid: 0 }
let mockLog = ['[程序启动] SM GUI 已就绪']

async function mockCall(method, ...args) {
  await new Promise(r => setTimeout(r, 120))
  switch (method) {
    case 'GetNodes': return [...mockNodes]
    case 'GetGroups': return [...mockGroups]
    case 'AddGroup': {
      const g = { id: Date.now().toString(), name: args[0], is_default: false }
      const afterIdx = mockGroups.findIndex(x => x.id === args[1])
      if (afterIdx >= 0) mockGroups.splice(afterIdx + 1, 0, g)
      else mockGroups.push(g)
      return g
    }
    case 'RenameGroup': {
      const g = mockGroups.find(x => x.id === args[0])
      if (g) g.name = args[1]
      return null
    }
    case 'DeleteGroup': {
      mockGroups = mockGroups.filter(x => x.id !== args[0])
      mockNodes.forEach(n => { if (n.group_id === args[0]) n.group_id = 'default' })
      return null
    }
    case 'ImportNodes': return 2
    case 'FetchSubscription': mockNodes.push({ id: Date.now().toString(), name: '新订阅节点', protocol: 'vmess', address: 'new.example.com', port: 443, sub_url: args[0] }); return 1
    case 'ClearNodes': mockNodes = []; return null
    case 'DeleteNode': mockNodes = mockNodes.filter(n => n.id !== args[0]); return null
    case 'ApplyNode': mockAppliedNodeId = args[0]; return null
    case 'ExportNodeURI': {
      const n = mockNodes.find(x => x.id === args[0])
      return n ? `${n.protocol}://${n.address}:${n.port}#${encodeURIComponent(n.name)}` : ''
    }
    case 'MoveNodeUp': {
      const i = mockNodes.findIndex(x => x.id === args[0])
      if (i > 0) { [mockNodes[i - 1], mockNodes[i]] = [mockNodes[i], mockNodes[i - 1]] }
      return null
    }
    case 'MoveNodeDown': {
      const i = mockNodes.findIndex(x => x.id === args[0])
      if (i >= 0 && i < mockNodes.length - 1) { [mockNodes[i + 1], mockNodes[i]] = [mockNodes[i], mockNodes[i + 1]] }
      return null
    }
case 'GetSettings': return { ...mockSettings }
case 'SaveSettings': mockSettings = { ...mockSettings, ...args[0] }; return null
case 'GetAppliedNodeID': return mockAppliedNodeId
case 'GetConfigFiles': {
  const base = mockSettings.core === 'mihomo' ? ['config.example.yaml'] : ['config.example.json']
  return [...base, '内置配置：绕过大陆', '内置配置：GFW列表', '内置配置：全局代理']
}
case 'SelectConfigFile': {
  const name = args[0] || ''
  if (name.startsWith('内置配置：')) {
    mockSettings.routing_mode = name.includes('绕过大陆') ? 'bypass' : name.includes('GFW') ? 'blacklist' : 'global'
    mockSettings.config_path = ''
  } else {
    mockSettings.routing_mode = 'custom'
    mockSettings.config_path = 'configs\\' + name
  }
  return mockSettings.config_path || name
}
case 'OpenConfigsDir': return null
    case 'GetSubscriptions': return [...mockSettings.subscriptions]
    case 'RemoveSubscription': mockSettings.subscriptions = mockSettings.subscriptions.filter(s => s !== args[0]); return null
    case 'RefreshSubscription': return 3
    case 'GetConfigPreview': return '{\n  "log": {},\n  "inbounds": [],\n  "outbounds": [{"tag":"proxy","type":"vless"}]\n}'
    case 'EnableTun': return null
    case 'DisableTun': return null
    case 'EnableSystemProxy': return null
    case 'DisableSystemProxy': return null
    case 'StartSingBox': mockStatus = { running: true, pid: 12345 }; mockLog.push('[15:30:00] sing-box 已启动 PID=12345'); return null
    case 'StopSingBox': mockStatus = { running: false, pid: 0 }; mockLog.push('[15:30:05] sing-box 已停止'); return null
    case 'GetSingBoxStatus': return { ...mockStatus }
    case 'GetSingBoxLog': return [...mockLog]
    default: return null
  }
}
