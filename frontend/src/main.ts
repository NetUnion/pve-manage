import './style.css'

type OptionItem = {
  key: string
  name: string
  limit: number
  memory_limit?: number
  node?: string[]
}

type NetworkOption = {
  key: string
  uestc: 'choose' | 'force'
  ipv4: {
    type: string
    start_ip: string
    cidr: number
    gateway: string
  }
  ipv6: {
    type: string
  }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ipfilter: string[]
}

type ClusterOption = {
  key: string
  name: string
  limit: number
  start_vmid: number
  cpu: OptionItem[]
  storage: OptionItem[]
  network: NetworkOption[]
}

type ConfigOptions = {
  clusters: ClusterOption[]
}

type Me = {
  username: string
  email: string
  name: string
  groups: string[]
  is_admin: boolean
}

type VM = {
  id: number
  owner_username: string
  cluster_key: string
  vmid: number
  vmname: string
  ip: string
  sshkeys: string[]
  shared_usernames: string[]
  security_group_name: string
  uestc_restricted: boolean
  managed: boolean
  config: Record<string, unknown>
  prefer_status: Record<string, unknown>
  real_status: Record<string, unknown>
  sync_state: string
  sync_error?: string | null
  version: number
  created_at: string
  updated_at: string
  deleted_at?: string | null
  delete_requested_at?: string | null
  delete_execute_after?: string | null
}

type SecurityGroup = {
  id: number
  owner_username: string
  name: string
  rules: Rule[]
  created_at: string
  updated_at: string
}

type Rule = {
  direction: 'in' | 'out'
  action: 'allow' | 'deny'
  ethertype: 'ipv4' | 'ipv6'
  protocol: 'tcp' | 'udp' | 'icmp'
  cidr: string
  port_start?: number | null
  port_end?: number | null
}

type Template = {
  id: number
  cluster_key: string
  template_vmid: number
  name: string
  description?: string | null
  os_type?: string | null
  real_status: Record<string, unknown>
  last_seen_at: string
  created_at: string
  updated_at: string
}

type UserRow = {
  username: string
  email: string
  name: string
  groups: string[]
  is_admin: boolean
}

const state = {
  me: null as Me | null,
  options: null as ConfigOptions | null,
  vms: [] as VM[],
  allVMs: [] as VM[],
  securityGroups: [] as SecurityGroup[],
  templates: [] as Template[],
  users: [] as UserRow[],
  activeTab: 'vms',
  adminTab: 'users',
  vmFilter: '',
  vmDialogMode: 'create' as 'create' | 'edit',
  vmDialogVm: null as VM | null,
  sgDialogMode: 'create' as 'create' | 'edit',
  sgDialogGroup: null as SecurityGroup | null,
  sgRules: [] as Rule[],
}

const app = document.querySelector<HTMLDivElement>('#app')
if (!app) {
  throw new Error('app root missing')
}

app.innerHTML = `
  <header class="topbar">
    <div class="brand">
      <div class="brand-title">Cloud Manage</div>
      <div class="brand-subtitle">PVE 虚拟机控制台</div>
    </div>
    <div class="topbar-actions">
      <div id="me-chip" class="me-chip">未登录</div>
      <button id="login-btn" class="btn btn-primary">登录</button>
      <button id="logout-btn" class="btn btn-ghost">退出</button>
    </div>
  </header>

  <div id="toast" class="toast" role="status" aria-live="polite" aria-atomic="true" hidden>
    <div id="toast-text" class="toast-text"></div>
    <button id="toast-close" type="button" class="toast-close" aria-label="关闭提示">×</button>
  </div>

  <main class="shell">
    <div class="workspace">
      <aside class="sidebar">
        <div class="sidebar-section">
          <div class="sidebar-label">导航</div>
          <nav class="sidebar-menu">
            <button class="tab active" data-tab="vms">
              <span class="menu-title">VMs</span>
              <span class="menu-subtitle">虚拟机</span>
            </button>
            <button class="tab" data-tab="security">
              <span class="menu-title">Security Groups</span>
              <span class="menu-subtitle">安全组</span>
            </button>
            <button class="tab" data-tab="templates">
              <span class="menu-title">Templates</span>
              <span class="menu-subtitle">模板</span>
            </button>
            <button class="tab admin-only" data-tab="admin">
              <span class="menu-title">Admin</span>
              <span class="menu-subtitle">管理</span>
            </button>
          </nav>
        </div>
        <div class="sidebar-section sidebar-foot">
          <div class="sidebar-label">状态</div>
          <div id="summary" class="summary summary-sidebar"></div>
        </div>
      </aside>

      <section class="workspace-main">
        <section id="tab-vms" class="tab-panel active">
          <div class="panel-head">
            <div>
              <h2>虚拟机</h2>
              <p>列出你拥有或共享管理的 VM。</p>
            </div>
            <div class="panel-actions">
              <input id="vm-search" class="input" placeholder="搜索名称 / IP / owner" />
              <button id="create-vm-btn" class="btn btn-primary">新建 VM</button>
            </div>
          </div>
          <div id="vm-table" class="table-wrap"></div>
        </section>

        <section id="tab-security" class="tab-panel">
          <div class="panel-head">
            <div>
              <h2>Security Groups</h2>
              <p>管理你自己的安全组规则。</p>
            </div>
            <div class="panel-actions">
              <button id="create-sg-btn" class="btn btn-primary">新建安全组</button>
            </div>
          </div>
          <div id="sg-table" class="table-wrap"></div>
        </section>

        <section id="tab-templates" class="tab-panel">
          <div class="panel-head">
            <div>
              <h2>Templates</h2>
              <p>Worker 上报的可用模板。</p>
            </div>
          </div>
          <div id="template-table" class="table-wrap"></div>
        </section>

        <section id="tab-admin" class="tab-panel">
          <div class="panel-head">
            <div>
              <h2>Admin</h2>
              <p>查看全局 VM、用户并修改 IP。</p>
            </div>
          </div>
          <div class="subtabs">
            <button class="subtab active" data-admin-tab="users">Users</button>
            <button class="subtab" data-admin-tab="vms">All VMs</button>
          </div>
          <div id="admin-users-panel" class="subpanel active">
            <div id="user-table" class="table-wrap"></div>
          </div>
          <div id="admin-vms-panel" class="subpanel">
            <div id="admin-vm-table" class="table-wrap"></div>
          </div>
        </section>
      </div>
    </section>
  </main>

  <dialog id="vm-dialog" class="dialog">
    <form id="vm-form" class="dialog-body">
      <div class="dialog-head">
        <h3 id="vm-dialog-title">新建 VM</h3>
        <button type="button" class="icon-btn" data-close-dialog="vm-dialog">×</button>
      </div>
      <input type="hidden" id="vm-mode" value="create" />
      <input type="hidden" id="vm-id" />
      <div class="form-grid">
        <label>Cluster<select id="vm-cluster" class="input"></select></label>
        <label>Template<select id="vm-template" class="input"></select></label>
        <label>名称<input id="vm-name" class="input" /></label>
        <label>CPU Key<select id="vm-cpu-key" class="input"></select></label>
        <label>CPU 核数<input id="vm-cpu-cores" class="input" type="number" min="1" /></label>
        <label>Memory GB<input id="vm-memory-gb" class="input" type="number" min="1" /></label>
        <label>Storage Key<select id="vm-storage-key" class="input"></select></label>
        <label>Disk GB<input id="vm-disk-gb" class="input" type="number" min="20" /></label>
        <label>Bridge<select id="vm-bridge-key" class="input"></select></label>
        <label>Security Group<select id="vm-sg" class="input"></select></label>
        <label>Power<select id="vm-power" class="input">
          <option value="running">running</option>
          <option value="stopped">stopped</option>
        </select></label>
        <label class="checkbox-row"><input id="vm-uestc" type="checkbox" /> uestc restriction</label>
      </div>
      <label>SSH Keys<textarea id="vm-sshkeys" class="input textarea" placeholder="one key per line"></textarea></label>
      <label>Shared Usernames<textarea id="vm-shared" class="input textarea" placeholder="one username per line"></textarea></label>
      <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" data-close-dialog="vm-dialog">取消</button>
        <button type="submit" class="btn btn-primary" id="vm-submit">保存</button>
      </div>
    </form>
  </dialog>

  <dialog id="sg-dialog" class="dialog">
    <form id="sg-form" class="dialog-body">
      <div class="dialog-head">
        <h3 id="sg-dialog-title">新建 Security Group</h3>
        <button type="button" class="icon-btn" data-close-dialog="sg-dialog">×</button>
      </div>
      <input type="hidden" id="sg-mode" value="create" />
      <label>Name<input id="sg-name" class="input" /></label>
      <div class="rule-head">
        <h4>Rules</h4>
        <button type="button" id="add-rule-btn" class="btn btn-ghost">Add Rule</button>
      </div>
      <div class="rule-table">
        <div class="rule-row rule-row-head">
          <span>Dir</span><span>Action</span><span>Type</span><span>Proto</span><span>CIDR</span><span>Start</span><span>End</span><span></span>
        </div>
        <div id="rule-rows"></div>
      </div>
      <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" data-close-dialog="sg-dialog">取消</button>
        <button type="submit" class="btn btn-primary">保存</button>
      </div>
    </form>
  </dialog>

  <dialog id="detail-dialog" class="dialog">
    <div class="dialog-body">
      <div class="dialog-head">
        <h3>VM Details</h3>
        <button type="button" class="icon-btn" data-close-dialog="detail-dialog">×</button>
      </div>
      <div id="detail-content" class="detail-content"></div>
      <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" data-close-dialog="detail-dialog">关闭</button>
      </div>
    </div>
  </dialog>

  <dialog id="ip-dialog" class="dialog">
    <form id="ip-form" class="dialog-body">
      <div class="dialog-head">
        <h3>修改 IPv4</h3>
        <button type="button" class="icon-btn" data-close-dialog="ip-dialog">×</button>
      </div>
      <input type="hidden" id="ip-vm-id" />
      <label>IP/CIDR<input id="ip-value" class="input" placeholder="10.10.80.11/18" /></label>
      <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" data-close-dialog="ip-dialog">取消</button>
        <button type="submit" class="btn btn-primary">保存</button>
      </div>
    </form>
  </dialog>
`

const $ = <T extends HTMLElement>(selector: string): T => {
  const el = document.querySelector(selector)
  if (!el) throw new Error(`missing ${selector}`)
  return el as T
}

const els = {
  meChip: $('#me-chip'),
  summary: $('#summary'),
  toast: $('#toast') as HTMLDivElement,
  toastText: $('#toast-text'),
  toastClose: $('#toast-close') as HTMLButtonElement,
  loginBtn: $('#login-btn'),
  logoutBtn: $('#logout-btn'),
  vmTable: $('#vm-table'),
  sgTable: $('#sg-table'),
  templateTable: $('#template-table'),
  userTable: $('#user-table'),
  adminVmTable: $('#admin-vm-table'),
  vmSearch: $('#vm-search'),
  createVmBtn: $('#create-vm-btn'),
  createSgBtn: $('#create-sg-btn'),
  tabs: Array.from(document.querySelectorAll<HTMLButtonElement>('.tab')),
  panels: {
    vms: $('#tab-vms'),
    security: $('#tab-security'),
    templates: $('#tab-templates'),
    admin: $('#tab-admin'),
  },
  vmDialog: $('#vm-dialog') as HTMLDialogElement,
  sgDialog: $('#sg-dialog') as HTMLDialogElement,
  detailDialog: $('#detail-dialog') as HTMLDialogElement,
  ipDialog: $('#ip-dialog') as HTMLDialogElement,
  vmForm: $('#vm-form') as HTMLFormElement,
  sgForm: $('#sg-form') as HTMLFormElement,
  ipForm: $('#ip-form') as HTMLFormElement,
  vmDialogTitle: $('#vm-dialog-title'),
  sgDialogTitle: $('#sg-dialog-title'),
  vmMode: $('#vm-mode') as HTMLInputElement,
  vmId: $('#vm-id') as HTMLInputElement,
  vmCluster: $('#vm-cluster') as HTMLSelectElement,
  vmTemplate: $('#vm-template') as HTMLSelectElement,
  vmName: $('#vm-name') as HTMLInputElement,
  vmCpuKey: $('#vm-cpu-key') as HTMLSelectElement,
  vmCpuCores: $('#vm-cpu-cores') as HTMLInputElement,
  vmMemoryGB: $('#vm-memory-gb') as HTMLInputElement,
  vmStorageKey: $('#vm-storage-key') as HTMLSelectElement,
  vmDiskGB: $('#vm-disk-gb') as HTMLInputElement,
  vmBridgeKey: $('#vm-bridge-key') as HTMLSelectElement,
  vmSg: $('#vm-sg') as HTMLSelectElement,
  vmPower: $('#vm-power') as HTMLSelectElement,
  vmUESTC: $('#vm-uestc') as HTMLInputElement,
  vmSshkeys: $('#vm-sshkeys') as HTMLTextAreaElement,
  vmShared: $('#vm-shared') as HTMLTextAreaElement,
  vmSubmit: $('#vm-submit') as HTMLButtonElement,
  sgMode: $('#sg-mode') as HTMLInputElement,
  sgName: $('#sg-name') as HTMLInputElement,
  ruleRows: $('#rule-rows'),
  addRuleBtn: $('#add-rule-btn'),
  detailContent: $('#detail-content'),
  ipVmId: $('#ip-vm-id') as HTMLInputElement,
  ipValue: $('#ip-value') as HTMLInputElement,
}

function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function formatTime(value?: string | null): string {
  if (!value) return '-'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
}

function flash(message: string, kind: 'info' | 'ok' | 'error' = 'info') {
  showToast(message, kind)
}

let toastTimer: number | undefined

function moveToastToTopLayer() {
  const openDialogs = Array.from(document.querySelectorAll<HTMLDialogElement>('dialog.dialog[open]'))
  const activeDialog = openDialogs.at(-1)
  const target = activeDialog ?? document.body
  if (els.toast.parentElement !== target) {
    target.appendChild(els.toast)
  }
}

function showToast(message: string, kind: 'info' | 'ok' | 'error' = 'info') {
  els.toastText.textContent = message
  els.toast.className = `toast ${kind}`
  moveToastToTopLayer()
  els.toast.hidden = false
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => {
    hideToast()
  }, 5000)
}

function hideToast() {
  window.clearTimeout(toastTimer)
  els.toast.hidden = true
  if (els.toast.parentElement !== document.body) {
    document.body.appendChild(els.toast)
  }
}

function apiPath(path: string): string {
  return path
}

async function api<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers = new Headers(opts.headers)
  if (opts.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(apiPath(path), {
    credentials: 'same-origin',
    ...opts,
    headers,
  })
  if (!res.ok) {
    const text = await res.text()
    let message = text || res.statusText
    try {
      const parsed = JSON.parse(text)
      if (parsed && typeof parsed.error === 'string') {
        message = parsed.error
      }
    } catch {
      /* ignore */
    }
    throw new Error(message)
  }
  return (await res.json()) as T
}

function getCluster(key?: string): ClusterOption | undefined {
  return state.options?.clusters.find((item) => item.key === key)
}

function getSelectedCluster(): ClusterOption | undefined {
  return getCluster(els.vmCluster.value)
}

function userOwns(vm: VM): boolean {
  return Boolean(vm.managed && state.me && (state.me.is_admin || state.me.username === vm.owner_username))
}

function visibleVm(vm: VM): boolean {
  if (!state.me) return false
  return vm.managed && (state.me.username === vm.owner_username || vm.shared_usernames.includes(state.me.username))
}

function powerFrom(vm: VM): string {
  const prefer = vm.prefer_status as Record<string, unknown>
  const power = prefer.power
  return typeof power === 'string' ? power : 'unknown'
}

function realPowerFrom(vm: VM): string {
  const real = vm.real_status as Record<string, unknown>
  const power = real.power
  return typeof power === 'string' ? power : 'unknown'
}

function statusBadge(label: string): string {
  const display =
    label === 'synced' ? '已同步' :
    label === 'failed' ? '失败' :
    label === 'deleting' ? '待删除' :
    label === 'pending' ? '待同步' :
    label === 'syncing' ? '同步中' :
    label
  const cls = label === 'synced' ? 'ok' : label === 'failed' ? 'bad' : label === 'deleting' ? 'warn' : 'soft'
  return `<span class="badge ${cls}">${escapeHtml(display)}</span>`
}

function isDeletePending(vm: VM): boolean {
  return vm.sync_state === 'deleting' || Boolean(vm.delete_requested_at)
}

function rowActions(vm: VM, allowAdminIp = false): string {
  if (isDeletePending(vm)) {
    const canRestore = userOwns(vm) || state.me?.is_admin
    return `
      <div class="row-actions">
        <button class="btn btn-mini" data-action="detail" data-id="${vm.id}">详情</button>
        ${canRestore ? `<button class="btn btn-mini btn-warn" data-action="delete-now" data-id="${vm.id}">立即删除</button>` : ''}
        ${canRestore ? `<button class="btn btn-mini btn-warn" data-action="restore-delete" data-id="${vm.id}">恢复删除</button>` : ''}
      </div>
    `
  }
  return `
    <div class="row-actions">
      <button class="btn btn-mini" data-action="detail" data-id="${vm.id}">详情</button>
      ${vm.managed ? `<button class="btn btn-mini" data-action="power" data-id="${vm.id}" data-power="running">开机</button>` : ''}
      ${vm.managed ? `<button class="btn btn-mini" data-action="power" data-id="${vm.id}" data-power="stopped">关机</button>` : ''}
      ${vm.managed ? `<button class="btn btn-mini" data-action="power" data-id="${vm.id}" data-power="reboot">重启</button>` : ''}
      ${userOwns(vm) ? `<button class="btn btn-mini" data-action="edit" data-id="${vm.id}">编辑</button>` : ''}
      ${userOwns(vm) ? `<button class="btn btn-mini btn-danger" data-action="delete" data-id="${vm.id}">删除</button>` : ''}
      ${allowAdminIp && vm.managed ? `<button class="btn btn-mini" data-action="admin-ip" data-id="${vm.id}">改IP</button>` : ''}
    </div>
  `
}

function renderSummary() {
  const total = state.vms.length
  const pending = state.vms.filter((vm) => vm.sync_state === 'pending' || vm.sync_state === 'syncing').length
  const failed = state.vms.filter((vm) => vm.sync_state === 'failed').length
  const deleting = state.vms.filter(isDeletePending).length
  const admin = state.me?.is_admin ? '管理员' : '普通用户'
  els.summary.innerHTML = `
    <span class="stat">VM ${total}</span>
    <span class="stat">待同步 ${pending}</span>
    <span class="stat">失败 ${failed}</span>
    <span class="stat">待删除 ${deleting}</span>
    <span class="stat">${admin}</span>
  `
}

function renderMe() {
  if (!state.me) {
    els.meChip.textContent = '未登录'
    els.loginBtn.hidden = false
    els.logoutBtn.hidden = true
    document.querySelectorAll<HTMLElement>('.admin-only').forEach((el) => (el.hidden = true))
    return
  }
  els.meChip.textContent = state.me.name || state.me.username
  els.loginBtn.hidden = true
  els.logoutBtn.hidden = false
  document.querySelectorAll<HTMLElement>('.admin-only').forEach((el) => (el.hidden = !state.me?.is_admin))
}

function renderTabs() {
  els.tabs.forEach((tab) => {
    const active = tab.dataset.tab === state.activeTab
    tab.classList.toggle('active', active)
  })
  Object.entries(els.panels).forEach(([name, panel]) => {
    panel.classList.toggle('active', name === state.activeTab)
  })
  const adminButtons = document.querySelectorAll<HTMLButtonElement>('[data-admin-tab]')
  adminButtons.forEach((button) => {
    button.classList.toggle('active', button.dataset.adminTab === state.adminTab)
  })
  const adminUsersPanel = document.getElementById('admin-users-panel')
  const adminVmsPanel = document.getElementById('admin-vms-panel')
  adminUsersPanel?.classList.toggle('active', state.adminTab === 'users')
  adminVmsPanel?.classList.toggle('active', state.adminTab === 'vms')
}

function renderVmTable() {
  const filtered = state.vms.filter((vm) => vm.managed).filter((vm) => {
    if (!state.vmFilter) return true
    const target = `${vm.vmname} ${vm.ip} ${vm.owner_username} ${vm.cluster_key} ${vm.security_group_name}`.toLowerCase()
    return target.includes(state.vmFilter.toLowerCase())
  })
  if (!filtered.length) {
    els.vmTable.innerHTML = `<div class="empty">没有匹配的 VM。</div>`
    return
  }
  els.vmTable.innerHTML = `
    <table>
      <thead>
        <tr>
          <th>名称</th><th>Owner</th><th>Cluster</th><th>VMID</th><th>IP</th><th>SG</th><th>Sync</th><th>Power</th><th>更新时间</th><th></th>
        </tr>
      </thead>
      <tbody>
        ${filtered
          .map(
            (vm) => `
            <tr data-vm-id="${vm.id}">
              <td class="strong">${escapeHtml(vm.vmname)}</td>
              <td>${escapeHtml(vm.owner_username)}</td>
              <td>${escapeHtml(vm.cluster_key)}</td>
              <td>${vm.vmid}</td>
              <td class="mono">${escapeHtml(vm.ip)}</td>
              <td>${escapeHtml(vm.security_group_name)}</td>
              <td>${statusBadge(vm.sync_state)}</td>
              <td>${escapeHtml(powerFrom(vm))}</td>
              <td>${escapeHtml(formatTime(vm.updated_at))}</td>
              <td>${rowActions(vm)}</td>
            </tr>
          `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function renderSecurityGroups() {
  const groups = state.securityGroups
  if (!groups.length) {
    els.sgTable.innerHTML = `<div class="empty">还没有安全组。</div>`
    return
  }
  els.sgTable.innerHTML = `
    <table>
      <thead>
        <tr><th>Name</th><th>Rules</th><th>Updated</th><th></th></tr>
      </thead>
      <tbody>
        ${groups
          .map(
            (sg) => `
              <tr data-sg-name="${escapeHtml(sg.name)}">
                <td class="strong">${escapeHtml(sg.name)}</td>
                <td>${sg.rules.length}</td>
                <td>${escapeHtml(formatTime(sg.updated_at))}</td>
                <td>
                  <div class="row-actions">
                    <button class="btn btn-mini" data-sg-action="edit" data-name="${escapeHtml(sg.name)}">编辑</button>
                    <button class="btn btn-mini btn-danger" data-sg-action="delete" data-name="${escapeHtml(sg.name)}">删除</button>
                  </div>
                </td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function renderTemplates() {
  const list = state.templates
  if (!list.length) {
    els.templateTable.innerHTML = `<div class="empty">还没有模板数据，等待 worker 上报。</div>`
    return
  }
  els.templateTable.innerHTML = `
    <table>
      <thead>
        <tr><th>Cluster</th><th>VMID</th><th>Name</th><th>OS</th><th>Last Seen</th></tr>
      </thead>
      <tbody>
        ${list
          .map(
            (tpl) => `
              <tr>
                <td>${escapeHtml(tpl.cluster_key)}</td>
                <td>${tpl.template_vmid}</td>
                <td class="strong">${escapeHtml(tpl.name)}</td>
                <td>${escapeHtml(tpl.os_type || '-')}</td>
                <td>${escapeHtml(formatTime(tpl.last_seen_at))}</td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function renderUsers() {
  if (!state.me?.is_admin) {
    els.userTable.innerHTML = `<div class="empty">管理员可见。</div>`
    return
  }
  if (!state.users.length) {
    els.userTable.innerHTML = `<div class="empty">没有用户数据。</div>`
    return
  }
  els.userTable.innerHTML = `
    <table>
      <thead><tr><th>Username</th><th>Email</th><th>Name</th><th>Groups</th><th>Admin</th></tr></thead>
      <tbody>
        ${state.users
          .map(
            (u) => `
              <tr>
                <td class="strong">${escapeHtml(u.username)}</td>
                <td>${escapeHtml(u.email || '-')}</td>
                <td>${escapeHtml(u.name || '-')}</td>
                <td>${escapeHtml(u.groups.join(', '))}</td>
                <td>${u.is_admin ? 'yes' : 'no'}</td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function renderAdminVMs() {
  if (!state.me?.is_admin) {
    els.adminVmTable.innerHTML = `<div class="empty">管理员可见。</div>`
    return
  }
  if (!state.allVMs.length) {
    els.adminVmTable.innerHTML = `<div class="empty">没有 VM。</div>`
    return
  }
  els.adminVmTable.innerHTML = `
    <table>
      <thead><tr><th>Name</th><th>Owner</th><th>Cluster</th><th>VMID</th><th>IP</th><th>Managed</th><th>Sync</th><th></th></tr></thead>
      <tbody>
        ${state.allVMs
          .map(
            (vm) => `
              <tr>
                <td class="strong">${escapeHtml(vm.vmname)}</td>
                <td>${escapeHtml(vm.owner_username)}</td>
                <td>${escapeHtml(vm.cluster_key)}</td>
                <td>${vm.vmid}</td>
                <td class="mono">${escapeHtml(vm.ip)}</td>
                <td>${vm.managed ? 'yes' : 'no'}</td>
                <td>${statusBadge(vm.sync_state)}</td>
                <td>
                  ${rowActions(vm, true)}
                </td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function showDialog(dialog: HTMLDialogElement) {
  if (!dialog.open) dialog.showModal()
}

function closeDialog(id: string) {
  const dialog = document.getElementById(id) as HTMLDialogElement | null
  dialog?.close()
}

function currentCluster(): ClusterOption | undefined {
  return state.options?.clusters.find((cluster) => cluster.key === els.vmCluster.value) ?? state.options?.clusters[0]
}

function renderVmFormOptions() {
  const opts = state.options?.clusters ?? []
  const previousCluster = els.vmCluster.value || opts[0]?.key || ''
  const previousTemplate = els.vmTemplate.value
  const previousCpu = els.vmCpuKey.value
  const previousStorage = els.vmStorageKey.value
  const previousBridge = els.vmBridgeKey.value
  const previousSg = els.vmSg.value
  els.vmCluster.innerHTML = opts.map((cluster) => `<option value="${escapeHtml(cluster.key)}">${escapeHtml(cluster.name)} (${escapeHtml(cluster.key)})</option>`).join('')
  if (previousCluster) {
    els.vmCluster.value = previousCluster
  }
  const cluster = currentCluster()
  if (!cluster) return

  els.vmCpuKey.innerHTML = cluster.cpu.map((cpu) => `<option value="${escapeHtml(cpu.key)}">${escapeHtml(cpu.name)}</option>`).join('')
  els.vmStorageKey.innerHTML = cluster.storage.map((s) => `<option value="${escapeHtml(s.key)}">${escapeHtml(s.name)}</option>`).join('')
  els.vmBridgeKey.innerHTML = cluster.network.map((n) => `<option value="${escapeHtml(n.key)}">${escapeHtml(n.key)}</option>`).join('')
  const templates = state.templates.filter((tpl) => tpl.cluster_key === cluster.key)
  const currentTemplateId = state.vmDialogVm?.prefer_status.template_vmid
  const templateOptions = templates.slice()
  if (currentTemplateId && !templateOptions.some((tpl) => tpl.template_vmid === currentTemplateId)) {
    templateOptions.unshift({
      id: 0,
      cluster_key: cluster.key,
      template_vmid: Number(currentTemplateId),
      name: `Template #${currentTemplateId}`,
      os_type: null,
      description: null,
      real_status: {},
      last_seen_at: '',
      created_at: '',
      updated_at: '',
    })
  }
  els.vmTemplate.innerHTML = templateOptions.length
    ? templateOptions.map((tpl) => `<option value="${tpl.template_vmid}">${escapeHtml(tpl.name)} (#${tpl.template_vmid})</option>`).join('')
    : `<option value="">暂无模板</option>`

  const ownGroups = state.securityGroups.filter((group) => group.owner_username === state.me?.username)
  els.vmSg.innerHTML = ownGroups.length
    ? ownGroups.map((sg) => `<option value="${escapeHtml(sg.name)}">${escapeHtml(sg.name)}</option>`).join('')
    : `<option value="">请先创建安全组</option>`

  if (previousCpu) els.vmCpuKey.value = previousCpu
  if (previousStorage) els.vmStorageKey.value = previousStorage
  if (previousBridge) els.vmBridgeKey.value = previousBridge
  if (previousSg) els.vmSg.value = previousSg
  if (previousTemplate) els.vmTemplate.value = previousTemplate
  if (!els.vmTemplate.value && templateOptions[0]) {
    els.vmTemplate.value = String(templateOptions[0].template_vmid)
  }
  if (!els.vmSg.value && ownGroups[0]) {
    els.vmSg.value = ownGroups[0].name
  }
  els.vmSubmit.disabled = state.vmDialogMode === 'create' && (!templateOptions.length || !ownGroups.length)

  const network = cluster.network[0]
  if (network) {
    if (state.vmDialogMode === 'create' && !state.vmDialogVm) {
      els.vmUESTC.checked = network.uestc === 'force'
    }
    els.vmUESTC.disabled = network.uestc === 'force'
  }
}

function parseLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
}

function resetVmDialog() {
  els.vmMode.value = 'create'
  els.vmDialogTitle.textContent = '新建 VM'
  els.vmId.value = ''
  els.vmName.value = ''
  els.vmCpuCores.value = '1'
  els.vmMemoryGB.value = '1'
  els.vmDiskGB.value = '20'
  els.vmPower.value = 'running'
  els.vmSshkeys.value = ''
  els.vmShared.value = ''
  els.vmUESTC.checked = false
  els.vmUESTC.disabled = false
  renderVmFormOptions()
}

function fillVmDialog(vm: VM) {
  const cluster = getCluster(vm.cluster_key) ?? state.options?.clusters[0]
  if (!cluster) return
  state.vmDialogMode = 'edit'
  state.vmDialogVm = vm
  els.vmMode.value = 'edit'
  els.vmDialogTitle.textContent = `编辑 VM #${vm.vmid}`
  els.vmId.value = String(vm.id)
  els.vmCluster.value = vm.cluster_key
  renderVmFormOptions()
  els.vmTemplate.value = String((vm.prefer_status.template_vmid as number | undefined) ?? (state.templates.find((tpl) => tpl.cluster_key === vm.cluster_key)?.template_vmid ?? ''))
  els.vmName.value = vm.vmname
  els.vmCpuKey.value = String(vm.prefer_status.cpu_key ?? cluster.cpu[0]?.key ?? '')
  els.vmCpuCores.value = String(vm.prefer_status.cpu_cores ?? 1)
  els.vmMemoryGB.value = String(vm.prefer_status.memory_gb ?? 1)
  els.vmStorageKey.value = String(vm.prefer_status.storage_key ?? cluster.storage[0]?.key ?? '')
  els.vmDiskGB.value = String(vm.prefer_status.disk_gb ?? 20)
  els.vmBridgeKey.value = String(vm.prefer_status.bridge_key ?? cluster.network[0]?.key ?? '')
  els.vmSg.value = vm.security_group_name
  els.vmPower.value = String(vm.prefer_status.power ?? 'running')
  els.vmUESTC.checked = Boolean(vm.uestc_restricted)
  els.vmUESTC.disabled = Boolean(cluster.network[0]?.uestc === 'force')
  els.vmSshkeys.value = vm.sshkeys.join('\n')
  els.vmShared.value = vm.shared_usernames.join('\n')
}

function openVmDialog(mode: 'create' | 'edit', vm?: VM) {
  state.vmDialogMode = mode
  if (mode === 'create') {
    resetVmDialog()
  } else if (vm) {
    fillVmDialog(vm)
  }
  showDialog(els.vmDialog)
}

function addRuleRow(rule: Rule = {
  direction: 'in',
  action: 'allow',
  ethertype: 'ipv4',
  protocol: 'tcp',
  cidr: '0.0.0.0/0',
  port_start: 22,
  port_end: 22,
}) {
  state.sgRules.push(rule)
  renderRuleRows()
}

function renderRuleRows() {
  els.ruleRows.innerHTML = state.sgRules
    .map((rule, index) => `
      <div class="rule-row" data-index="${index}">
        <select class="input" data-field="direction">
          <option value="in" ${rule.direction === 'in' ? 'selected' : ''}>in</option>
          <option value="out" ${rule.direction === 'out' ? 'selected' : ''}>out</option>
        </select>
        <select class="input" data-field="action">
          <option value="allow" ${rule.action === 'allow' ? 'selected' : ''}>allow</option>
          <option value="deny" ${rule.action === 'deny' ? 'selected' : ''}>deny</option>
        </select>
        <select class="input" data-field="ethertype">
          <option value="ipv4" ${rule.ethertype === 'ipv4' ? 'selected' : ''}>ipv4</option>
          <option value="ipv6" ${rule.ethertype === 'ipv6' ? 'selected' : ''}>ipv6</option>
        </select>
        <select class="input" data-field="protocol">
          <option value="tcp" ${rule.protocol === 'tcp' ? 'selected' : ''}>tcp</option>
          <option value="udp" ${rule.protocol === 'udp' ? 'selected' : ''}>udp</option>
          <option value="icmp" ${rule.protocol === 'icmp' ? 'selected' : ''}>icmp</option>
        </select>
        <input class="input" data-field="cidr" value="${escapeHtml(rule.cidr)}" />
        <input class="input" data-field="port_start" type="number" value="${rule.port_start ?? ''}" />
        <input class="input" data-field="port_end" type="number" value="${rule.port_end ?? ''}" />
        <button type="button" class="btn btn-mini btn-danger" data-remove-rule="${index}">删除</button>
      </div>
    `)
    .join('')
}

function resetSgDialog() {
  state.sgDialogMode = 'create'
  state.sgDialogGroup = null
  state.sgRules = []
  els.sgDialogTitle.textContent = '新建 Security Group'
  els.sgName.value = ''
  renderRuleRows()
}

function fillSgDialog(group: SecurityGroup) {
  state.sgDialogMode = 'edit'
  state.sgDialogGroup = group
  state.sgRules = group.rules.length ? JSON.parse(JSON.stringify(group.rules)) : []
  els.sgDialogTitle.textContent = `编辑 ${group.name}`
  els.sgName.value = group.name
  renderRuleRows()
}

function openSgDialog(mode: 'create' | 'edit', group?: SecurityGroup) {
  if (mode === 'create') {
    resetSgDialog()
  } else if (group) {
    fillSgDialog(group)
  }
  showDialog(els.sgDialog)
}

function renderDetail(vm: VM) {
  const pretty = (value: unknown) => escapeHtml(JSON.stringify(value, null, 2))
  const deleteInfo = isDeletePending(vm)
    ? `<div><span class="label">删除时间</span><div>${escapeHtml(formatTime(vm.delete_execute_after))}</div></div>`
    : ''
  els.detailContent.innerHTML = `
    <div class="detail-grid">
      <div><span class="label">Name</span><div>${escapeHtml(vm.vmname)}</div></div>
      <div><span class="label">Owner</span><div>${escapeHtml(vm.owner_username)}</div></div>
      <div><span class="label">Cluster</span><div>${escapeHtml(vm.cluster_key)}</div></div>
      <div><span class="label">VMID</span><div>${vm.vmid}</div></div>
      <div><span class="label">IP</span><div class="mono">${escapeHtml(vm.ip)}</div></div>
      <div><span class="label">Security Group</span><div>${escapeHtml(vm.security_group_name)}</div></div>
      <div><span class="label">Sync State</span><div>${statusBadge(vm.sync_state)}</div></div>
      <div><span class="label">Power</span><div>${escapeHtml(realPowerFrom(vm))}</div></div>
      ${deleteInfo}
    </div>
    <div class="detail-block">
      <h4>Prefer Status</h4>
      <pre>${pretty(vm.prefer_status)}</pre>
    </div>
    <div class="detail-block">
      <h4>Real Status</h4>
      <pre>${pretty(vm.real_status)}</pre>
    </div>
  `
}

function currentVmById(id: number): VM | undefined {
  return state.vms.find((vm) => vm.id === id) ?? state.allVMs.find((vm) => vm.id === id)
}

async function loadMe() {
  try {
    state.me = await api<Me>('/api/me')
  } catch {
    state.me = null
  }
  renderMe()
}

async function loadOptions() {
  if (state.options) return
  state.options = await api<ConfigOptions>('/api/config/options')
}

async function loadVMs() {
  const data = await api<{ items: VM[] }>('/api/vms')
  state.vms = data.items
  renderVmTable()
}

async function loadSecurityGroups() {
  const data = await api<{ items: SecurityGroup[] }>('/api/security-groups')
  state.securityGroups = data.items
  renderSecurityGroups()
}

async function loadTemplates() {
  const data = await api<{ items: Template[] }>('/api/templates')
  state.templates = data.items
  renderTemplates()
}

async function loadAdminUsers() {
  if (!state.me?.is_admin || state.users.length) return
  const data = await api<{ items: UserRow[] }>('/api/admin/users')
  state.users = data.items
  renderUsers()
}

async function loadAdminVMs() {
  if (!state.me?.is_admin || state.allVMs.length) return
  const data = await api<{ items: VM[] }>('/api/admin/vms')
  state.allVMs = data.items
  renderAdminVMs()
}

async function refreshVMViews() {
  await loadVMs()
  renderSummary()
  if (state.me?.is_admin && state.adminTab === 'vms') {
    state.allVMs = []
    await loadAdminVMs()
  }
}

async function loadActiveTab() {
  if (!state.me) return
  switch (state.activeTab) {
    case 'vms':
      await loadVMs()
      renderSummary()
      break
    case 'security':
      await loadSecurityGroups()
      break
    case 'templates':
      await loadTemplates()
      break
    case 'admin':
      if (state.adminTab === 'users') {
        await loadAdminUsers()
      } else {
        await loadAdminVMs()
      }
      break
  }
}

async function prepareVmDialogData() {
  await loadOptions()
  await Promise.all([loadSecurityGroups(), loadTemplates()])
  renderVmFormOptions()
}

function buildCreateVmPayload(): Record<string, unknown> {
  return {
    cluster_key: els.vmCluster.value,
    vmname: els.vmName.value.trim(),
    cpu_key: els.vmCpuKey.value,
    cpu_cores: Number(els.vmCpuCores.value),
    memory_gb: Number(els.vmMemoryGB.value),
    storage_key: els.vmStorageKey.value,
    disk_gb: Number(els.vmDiskGB.value),
    bridge_key: els.vmBridgeKey.value,
    template_vmid: Number(els.vmTemplate.value),
    sshkeys: parseLines(els.vmSshkeys.value),
    shared_usernames: parseLines(els.vmShared.value),
    security_group_name: els.vmSg.value,
    uestc_restricted: els.vmUESTC.checked,
    power: els.vmPower.value,
  }
}

function buildEditVmPayload(vm: VM): Record<string, unknown> {
  const ownerOnly = userOwns(vm)
  if (!ownerOnly) {
    return { power: els.vmPower.value }
  }
  return {
    vmname: els.vmName.value.trim(),
    cpu_key: els.vmCpuKey.value,
    cpu_cores: Number(els.vmCpuCores.value),
    memory_gb: Number(els.vmMemoryGB.value),
    storage_key: els.vmStorageKey.value,
    disk_gb: Number(els.vmDiskGB.value),
    bridge_key: els.vmBridgeKey.value,
    template_vmid: Number(els.vmTemplate.value),
    sshkeys: parseLines(els.vmSshkeys.value),
    shared_usernames: parseLines(els.vmShared.value),
    security_group_name: els.vmSg.value,
    uestc_restricted: els.vmUESTC.checked,
    power: els.vmPower.value,
  }
}

async function submitVmForm(event: SubmitEvent) {
  event.preventDefault()
  try {
    if (state.vmDialogMode === 'create') {
      await api('/api/vms', {
        method: 'POST',
        body: JSON.stringify(buildCreateVmPayload()),
      })
      flash('VM 已创建，等待 worker 同步。', 'ok')
    } else if (state.vmDialogVm) {
      await api(`/api/vms/${state.vmDialogVm.id}`, {
        method: 'PATCH',
        body: JSON.stringify(buildEditVmPayload(state.vmDialogVm)),
      })
      flash('VM 已更新。', 'ok')
    }
    els.vmDialog.close()
    await refreshVMViews()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

function serializeRules(): Rule[] {
  const rows = Array.from(els.ruleRows.querySelectorAll<HTMLElement>('.rule-row[data-index]'))
  return rows.map((row) => {
    const field = <K extends keyof Rule>(name: string): string => {
      const node = row.querySelector<HTMLInputElement | HTMLSelectElement>(`[data-field="${name}"]`)
      return node?.value ?? ''
    }
    const toInt = (name: string): number | null => {
      const raw = field(name)
      if (!raw) return null
      const parsed = Number(raw)
      return Number.isFinite(parsed) ? parsed : null
    }
    return {
      direction: field('direction') as 'in' | 'out',
      action: field('action') as 'allow' | 'deny',
      ethertype: field('ethertype') as 'ipv4' | 'ipv6',
      protocol: field('protocol') as 'tcp' | 'udp' | 'icmp',
      cidr: field('cidr'),
      port_start: toInt('port_start'),
      port_end: toInt('port_end'),
    }
  })
}

async function submitSgForm(event: SubmitEvent) {
  event.preventDefault()
  try {
    const payload = {
      name: els.sgName.value.trim(),
      rules: serializeRules(),
    }
    if (state.sgDialogMode === 'create') {
      await api('/api/security-groups', { method: 'POST', body: JSON.stringify(payload) })
      flash('安全组已创建。', 'ok')
    } else if (state.sgDialogGroup) {
      await api(`/api/security-groups/${encodeURIComponent(state.sgDialogGroup.name)}`, {
        method: 'PATCH',
        body: JSON.stringify(payload),
      })
      flash('安全组已更新。', 'ok')
    }
    els.sgDialog.close()
    await loadSecurityGroups()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function submitIpForm(event: SubmitEvent) {
  event.preventDefault()
  try {
    const vmId = Number(els.ipVmId.value)
    await api(`/api/admin/vms/${vmId}`, {
      method: 'PATCH',
      body: JSON.stringify({ ip: els.ipValue.value.trim() }),
    })
    flash('IP 已更新。', 'ok')
    els.ipDialog.close()
    state.allVMs = []
    await loadAdminVMs()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function deleteVm(id: number) {
  if (!confirm('确认删除这台 VM 吗？它会进入 24 小时等待删除窗口。')) return
  try {
    await api(`/api/vms/${id}`, { method: 'DELETE' })
    flash('已进入等待删除状态。', 'ok')
    await refreshVMViews()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function restoreVm(id: number) {
  try {
    await api(`/api/vms/${id}/restore`, { method: 'POST' })
    flash('已取消删除。', 'ok')
    await refreshVMViews()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function deleteNowVm(id: number) {
  try {
    await api(`/api/vms/${id}/delete-now`, { method: 'POST' })
    flash('已提交立即删除。', 'ok')
    await refreshVMViews()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function patchVmPower(id: number, power: string) {
  try {
    await api(`/api/vms/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ power }),
    })
    flash(`已提交 ${power} 请求。`, 'ok')
    await refreshVMViews()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

function openVmDetails(vm: VM) {
  renderDetail(vm)
  els.detailDialog.showModal()
}

function bindEvents() {
  els.loginBtn.addEventListener('click', () => {
    window.location.href = '/auth/login'
  })
  els.toastClose.addEventListener('click', hideToast)
  els.logoutBtn.addEventListener('click', async () => {
    await fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' })
    window.location.reload()
  })
  els.tabs.forEach((tab) => {
    tab.addEventListener('click', async () => {
      state.activeTab = tab.dataset.tab || 'vms'
      renderTabs()
      try {
        await loadActiveTab()
      } catch (err) {
        flash((err as Error).message, 'error')
      }
    })
  })
  document.querySelectorAll<HTMLButtonElement>('[data-admin-tab]').forEach((button) => {
    button.addEventListener('click', async () => {
      state.adminTab = button.dataset.adminTab === 'vms' ? 'vms' : 'users'
      renderTabs()
      try {
        await loadActiveTab()
      } catch (err) {
        flash((err as Error).message, 'error')
      }
    })
  })
  els.vmSearch.addEventListener('input', () => {
    state.vmFilter = els.vmSearch.value
    renderVmTable()
  })
  els.createVmBtn.addEventListener('click', async () => {
    try {
      state.vmDialogVm = null
      await prepareVmDialogData()
      openVmDialog('create')
    } catch (err) {
      flash((err as Error).message, 'error')
    }
  })
  els.createSgBtn.addEventListener('click', () => {
    state.sgDialogGroup = null
    openSgDialog('create')
  })
  els.vmCluster.addEventListener('change', () => {
    renderVmFormOptions()
  })
  els.vmForm.addEventListener('submit', submitVmForm)
  els.sgForm.addEventListener('submit', submitSgForm)
  els.ipForm.addEventListener('submit', submitIpForm)
  els.addRuleBtn.addEventListener('click', () => addRuleRow())
  document.body.addEventListener('click', async (event) => {
    const target = event.target as HTMLElement
    const closeId = target.getAttribute('data-close-dialog')
    if (closeId) {
      closeDialog(closeId)
      return
    }

    const vmAction = target.closest<HTMLElement>('[data-action]')
    if (vmAction) {
      const id = Number(vmAction.dataset.id)
      const vm = currentVmById(id)
      if (!vm) return
      switch (vmAction.dataset.action) {
        case 'detail':
          openVmDetails(vm)
          break
        case 'edit':
          try {
            state.vmDialogVm = vm
            await prepareVmDialogData()
            openVmDialog('edit', vm)
          } catch (err) {
            flash((err as Error).message, 'error')
          }
          break
        case 'delete':
          await deleteVm(id)
          break
        case 'restore-delete':
          await restoreVm(id)
          break
        case 'delete-now':
          await deleteNowVm(id)
          break
        case 'power':
          await patchVmPower(id, vmAction.dataset.power || 'running')
          break
        case 'admin-ip':
          els.ipVmId.value = String(vm.id)
          els.ipValue.value = vm.ip
          els.ipDialog.showModal()
          break
      }
      return
    }

    const sgAction = target.closest<HTMLElement>('[data-sg-action]')
    if (sgAction) {
      const name = sgAction.dataset.name || ''
      const sg = state.securityGroups.find((item) => item.name === name)
      if (!sg) return
      switch (sgAction.dataset.sgAction) {
        case 'edit':
          openSgDialog('edit', sg)
          break
        case 'delete':
          if (confirm(`删除安全组 ${name} ?`)) {
            try {
              await api(`/api/security-groups/${encodeURIComponent(name)}`, { method: 'DELETE' })
              flash('安全组已删除。', 'ok')
              await loadSecurityGroups()
            } catch (err) {
              flash((err as Error).message, 'error')
            }
          }
          break
      }
    }
  })

  els.ruleRows.addEventListener('input', (event) => {
    const row = (event.target as HTMLElement).closest<HTMLElement>('.rule-row[data-index]')
    if (!row) return
    const index = Number(row.dataset.index)
    const rule = state.sgRules[index]
    if (!rule) return
    const field = (event.target as HTMLInputElement | HTMLSelectElement).dataset.field as keyof Rule | undefined
    if (!field) return
    const value = (event.target as HTMLInputElement | HTMLSelectElement).value
    if (field === 'port_start' || field === 'port_end') {
      const parsed = value ? Number(value) : null
      ;(rule as any)[field] = parsed
    } else {
      ;(rule as any)[field] = value
    }
  })

  els.ruleRows.addEventListener('click', (event) => {
    const button = (event.target as HTMLElement).closest<HTMLButtonElement>('[data-remove-rule]')
    if (!button) return
    const index = Number(button.dataset.removeRule)
    state.sgRules.splice(index, 1)
    renderRuleRows()
  })
}

async function init() {
  bindEvents()
  try {
    await loadMe()
    if (state.me) {
      await loadActiveTab()
    } else {
      els.summary.innerHTML = ''
      renderVmTable()
      renderSecurityGroups()
      renderTemplates()
      renderUsers()
      renderAdminVMs()
    }
  } catch (err) {
    flash((err as Error).message, 'error')
  }
  renderSummary()
  renderTabs()
}

init().catch((err) => {
  flash((err as Error).message, 'error')
})
