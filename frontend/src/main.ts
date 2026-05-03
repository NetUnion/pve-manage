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
  quota?: {
    number: number
    cpu: number
    memory: number
    storage: number
    security_group: number
    uestc: 'choose' | 'force'
  }
  usage?: {
    count: number
    cpu: number
    memory: number
    storage: number
    security_group: number
  }
}

type VM = {
  id: number
  owner_username: string
  cluster_key: string
  vmid: number
  vmname: string
  ip: string
  node?: string
  sshkeys: string[]
  shared_usernames: string[]
  security_group_name: string
  uestc_restricted: boolean
  managed: boolean
  task_queue_paused: boolean
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
  password?: string
  tasks?: VMTask[]
}

type VMTask = {
  id: number
  vm_id: number
  seq: number
  kind: string
  payload: Record<string, unknown>
  status: string
  error?: string | null
  created_at: string
  updated_at: string
  started_at?: string | null
  finished_at?: string | null
}

type MaintenanceTask = {
  id: number
  kind: string
  payload: Record<string, unknown>
  status: string
  error?: string | null
  created_at: string
  updated_at: string
  started_at?: string | null
  finished_at?: string | null
}

type SecurityGroup = {
  id: number
  owner_username: string
  name: string
  policy_in: string
  policy_out: string
  rules: Rule[]
  created_at: string
  updated_at: string
}

type SSHKey = {
  id: number
  owner_username: string
  name: string
  public_key: string
  created_at: string
  updated_at: string
}

type Rule = {
  direction: 'in' | 'out'
  action: 'accept' | 'drop'
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
  adminSecurityGroups: [] as SecurityGroup[],
  sshKeys: [] as SSHKey[],
  adminSSHKeys: [] as SSHKey[],
  maintenanceTasks: [] as MaintenanceTask[],
  templates: [] as Template[],
  users: [] as UserRow[],
  activeTab: 'vms',
  adminTab: 'users' as 'users' | 'vms' | 'security-groups' | 'ssh-keys' | 'maintenance',
  vmDialogMode: 'create' as 'create' | 'edit' | 'adopt',
  vmDialogVm: null as VM | null,
  detailVmId: null as number | null,
  sgDialogMode: 'create' as 'create' | 'edit',
  sgDialogGroup: null as SecurityGroup | null,
  sgRules: [] as Rule[],
  sgPolicyIn: 'accept' as 'accept' | 'drop',
  sgPolicyOut: 'accept' as 'accept' | 'drop',
  sshDialogMode: 'create' as 'create' | 'edit',
  sshDialogKey: null as SSHKey | null,
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
            <button class="tab" data-tab="ssh">
              <span class="menu-title">SSH Keys</span>
              <span class="menu-subtitle">公钥</span>
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

        <section id="tab-ssh" class="tab-panel">
          <div class="panel-head">
            <div>
              <h2>SSH Keys</h2>
              <p>保存常用 SSH 公钥，创建 VM 时直接选择。</p>
            </div>
            <div class="panel-actions">
              <button id="create-ssh-btn" class="btn btn-primary">新增 SSH Key</button>
            </div>
          </div>
          <div id="ssh-table" class="table-wrap"></div>
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
            <button class="subtab" data-admin-tab="security-groups">All Security Groups</button>
            <button class="subtab" data-admin-tab="ssh-keys">All SSH Keys</button>
            <button class="subtab" data-admin-tab="maintenance">Maintenance</button>
          </div>
          <div id="admin-users-panel" class="subpanel active">
            <div id="user-table" class="table-wrap"></div>
          </div>
          <div id="admin-vms-panel" class="subpanel">
            <div id="admin-vm-table" class="table-wrap"></div>
          </div>
          <div id="admin-security-groups-panel" class="subpanel">
            <div id="admin-security-group-table" class="table-wrap"></div>
          </div>
          <div id="admin-ssh-keys-panel" class="subpanel">
            <div id="admin-ssh-key-table" class="table-wrap"></div>
          </div>
          <div id="admin-maintenance-panel" class="subpanel">
            <div id="admin-maintenance-table" class="table-wrap"></div>
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
      <div id="vm-flow-hint" class="flow-hint"></div>
      <div class="form-grid">
        <label>Cluster<select id="vm-cluster" class="input"></select></label>
        <label id="vm-owner-row" class="hidden">Owner<input id="vm-owner" class="input" placeholder="owner username" /></label>
        <label id="vm-template-row">Template<select id="vm-template" class="input"></select></label>
        <label>名称<input id="vm-name" class="input" /></label>
        <label>CPU Key<select id="vm-cpu-key" class="input"></select></label>
        <label>CPU 核数<input id="vm-cpu-cores" class="input" type="number" min="1" /></label>
        <label>Memory GB<input id="vm-memory-gb" class="input" type="number" min="1" /></label>
        <label>Storage Key<select id="vm-storage-key" class="input"></select></label>
        <label>Disk GB<input id="vm-disk-gb" class="input" type="number" min="20" /></label>
        <label>Bridge<select id="vm-bridge-key" class="input"></select></label>
        <label>Boot Order<select id="vm-boot-order" class="input">
          <option value="order=scsi0;ide0">disk first</option>
          <option value="order=ide0;scsi0">cdrom first</option>
        </select></label>
        <label>Security Group<select id="vm-sg" class="input"></select></label>
        <label>Password<input id="vm-password" class="input" type="password" autocomplete="new-password" placeholder="留空则随机生成或保持不变" /></label>
        <label class="checkbox-row"><input id="vm-uestc" type="checkbox" /> uestc restriction</label>
      </div>
      <div id="vm-limit-hint" class="limit-hint"></div>
      <div class="ssh-picker">
        <div class="section-title-row">
          <h4>SSH Keys</h4>
          <button type="button" id="ssh-shortcut-btn" class="btn btn-ghost">管理 SSH Keys</button>
        </div>
        <div id="vm-sshkey-list" class="ssh-checklist"></div>
      </div>
      <label>Shared Usernames<textarea id="vm-shared" class="input textarea" placeholder="one username per line"></textarea></label>
      <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" data-close-dialog="vm-dialog">取消</button>
        <button type="submit" class="btn btn-primary" id="vm-submit">保存</button>
      </div>
    </form>
  </dialog>

  <dialog id="ssh-dialog" class="dialog">
    <form id="ssh-form" class="dialog-body">
      <div class="dialog-head">
        <h3 id="ssh-dialog-title">新增 SSH Key</h3>
        <button type="button" class="icon-btn" data-close-dialog="ssh-dialog">×</button>
      </div>
      <input type="hidden" id="ssh-id" />
      <label>Name<input id="ssh-name" class="input" /></label>
      <label>Public Key<textarea id="ssh-key" class="input textarea" placeholder="ssh-ed25519 AAAA..."></textarea></label>
      <div class="dialog-actions">
        <button type="button" class="btn btn-ghost" data-close-dialog="ssh-dialog">取消</button>
        <button type="submit" class="btn btn-primary" id="ssh-submit">保存</button>
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
      <div class="form-grid compact">
        <label>Input Policy<select id="sg-policy-in" class="input">
          <option value="accept">accept</option>
          <option value="drop">drop</option>
        </select></label>
        <label>Output Policy<select id="sg-policy-out" class="input">
          <option value="accept">accept</option>
          <option value="drop">drop</option>
        </select></label>
      </div>
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
  adminSecurityGroupTable: $('#admin-security-group-table'),
  adminSshKeyTable: $('#admin-ssh-key-table'),
  adminMaintenanceTable: $('#admin-maintenance-table'),
  createVmBtn: $('#create-vm-btn'),
  createSgBtn: $('#create-sg-btn'),
  tabs: Array.from(document.querySelectorAll<HTMLButtonElement>('.tab')),
  panels: {
    vms: $('#tab-vms'),
    security: $('#tab-security'),
    ssh: $('#tab-ssh'),
    templates: $('#tab-templates'),
    admin: $('#tab-admin'),
  },
  vmDialog: $('#vm-dialog') as HTMLDialogElement,
  sshDialog: $('#ssh-dialog') as HTMLDialogElement,
  sgDialog: $('#sg-dialog') as HTMLDialogElement,
  detailDialog: $('#detail-dialog') as HTMLDialogElement,
  ipDialog: $('#ip-dialog') as HTMLDialogElement,
  vmForm: $('#vm-form') as HTMLFormElement,
  sshForm: $('#ssh-form') as HTMLFormElement,
  sgForm: $('#sg-form') as HTMLFormElement,
  ipForm: $('#ip-form') as HTMLFormElement,
  vmDialogTitle: $('#vm-dialog-title'),
  sshDialogTitle: $('#ssh-dialog-title'),
  sgDialogTitle: $('#sg-dialog-title'),
  vmMode: $('#vm-mode') as HTMLInputElement,
  sshId: $('#ssh-id') as HTMLInputElement,
  sshName: $('#ssh-name') as HTMLInputElement,
  sshKey: $('#ssh-key') as HTMLTextAreaElement,
  vmId: $('#vm-id') as HTMLInputElement,
  vmFlowHint: $('#vm-flow-hint'),
  vmCluster: $('#vm-cluster') as HTMLSelectElement,
  vmOwner: $('#vm-owner') as HTMLInputElement,
  vmOwnerRow: $('#vm-owner-row'),
  vmTemplateRow: $('#vm-template-row'),
  vmTemplate: $('#vm-template') as HTMLSelectElement,
  vmName: $('#vm-name') as HTMLInputElement,
  vmCpuKey: $('#vm-cpu-key') as HTMLSelectElement,
  vmCpuCores: $('#vm-cpu-cores') as HTMLInputElement,
  vmMemoryGB: $('#vm-memory-gb') as HTMLInputElement,
  vmStorageKey: $('#vm-storage-key') as HTMLSelectElement,
  vmDiskGB: $('#vm-disk-gb') as HTMLInputElement,
  vmBridgeKey: $('#vm-bridge-key') as HTMLSelectElement,
  vmBootOrder: $('#vm-boot-order') as HTMLSelectElement,
  vmSg: $('#vm-sg') as HTMLSelectElement,
  vmPassword: $('#vm-password') as HTMLInputElement,
  vmUESTC: $('#vm-uestc') as HTMLInputElement,
  vmLimitHint: $('#vm-limit-hint'),
  vmSSHKeyList: $('#vm-sshkey-list'),
  vmShared: $('#vm-shared') as HTMLTextAreaElement,
  vmSubmit: $('#vm-submit') as HTMLButtonElement,
  sshSubmit: $('#ssh-submit') as HTMLButtonElement,
  createSshBtn: $('#create-ssh-btn'),
  sshShortcutBtn: $('#ssh-shortcut-btn'),
  sshTable: $('#ssh-table'),
  sgMode: $('#sg-mode') as HTMLInputElement,
  sgName: $('#sg-name') as HTMLInputElement,
  sgPolicyIn: $('#sg-policy-in') as HTMLSelectElement,
  sgPolicyOut: $('#sg-policy-out') as HTMLSelectElement,
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

function displayClusterName(key?: string): string {
  return getCluster(key)?.name || key || '-'
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
  return realPowerFrom(vm)
}

function realPowerFrom(vm: VM): string {
  const real = vm.real_status as Record<string, unknown>
  const power = real.power
  return typeof power === 'string' ? power : 'unknown'
}

function configString(config: Record<string, unknown>, key: string, fallback = ''): string {
  const value = config[key]
  return typeof value === 'string' && value ? value : fallback
}

function configNumber(config: Record<string, unknown>, key: string, fallback = 0): number {
  const value = config[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return fallback
}

function remainingQuotaValue(kind: keyof NonNullable<Me['quota']>, usageKind: keyof NonNullable<Me['usage']>): number {
  const quota = state.me?.quota?.[kind]
  const usage = state.me?.usage?.[usageKind]
  if (typeof quota !== 'number' || typeof usage !== 'number') return 0
  return Math.max(0, quota - usage)
}

function taskKindLabel(kind: string): string {
  switch (kind) {
    case 'provision':
      return '创建'
    case 'apply':
      return '配置'
    case 'reboot':
      return '重启'
    case 'delete':
      return '删除'
    default:
      return kind
  }
}

function taskStatusBadge(status: string): string {
  const label =
    status === 'done' ? '完成' :
    status === 'failed' ? '失败' :
    status === 'running' ? '执行中' :
    status === 'canceled' ? '已取消' :
    status === 'pending' ? '等待中' :
    status
  const cls = status === 'done' ? 'ok' : status === 'failed' ? 'bad' : status === 'running' ? 'warn' : 'soft'
  return `<span class="badge ${cls}">${escapeHtml(label)}</span>`
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
        ${!vm.managed ? `<button class="btn btn-mini" data-action="adopt" data-id="${vm.id}">接管</button>` : ''}
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
  const adminSecurityGroupsPanel = document.getElementById('admin-security-groups-panel')
  const adminSshKeysPanel = document.getElementById('admin-ssh-keys-panel')
  const adminMaintenancePanel = document.getElementById('admin-maintenance-panel')
  adminUsersPanel?.classList.toggle('active', state.adminTab === 'users')
  adminVmsPanel?.classList.toggle('active', state.adminTab === 'vms')
  adminSecurityGroupsPanel?.classList.toggle('active', state.adminTab === 'security-groups')
  adminSshKeysPanel?.classList.toggle('active', state.adminTab === 'ssh-keys')
  adminMaintenancePanel?.classList.toggle('active', state.adminTab === 'maintenance')
}

function renderVmTable() {
  const filtered = state.vms.filter((vm) => vm.managed)
  if (!filtered.length) {
    els.vmTable.innerHTML = `<div class="empty">没有匹配的 VM。</div>`
    return
  }
  els.vmTable.innerHTML = `
    <div class="vm-list">
      <div class="vm-list-head">
        <span>名称</span>
        <span>Owner</span>
        <span>Cluster</span>
        <span>Node</span>
        <span>VMID</span>
        <span>IP</span>
        <span>SG</span>
        <span>Sync</span>
        <span>Power</span>
        <span>更新时间</span>
      </div>
      ${filtered
        .map(
          (vm) => `
            <section class="vm-card" data-vm-id="${vm.id}">
              <div class="vm-card-main">
                <div class="strong vm-card-name">${escapeHtml(vm.vmname)}</div>
                <div>${escapeHtml(vm.owner_username)}</div>
                <div>${escapeHtml(displayClusterName(vm.cluster_key))}</div>
                <div>${escapeHtml(vm.node ?? 'auto')}</div>
                <div>${vm.vmid}</div>
                <div class="mono">${escapeHtml(vm.ip)}</div>
                <div>${escapeHtml(vm.security_group_name)}</div>
                <div>${statusBadge(vm.sync_state)}</div>
                <div>${escapeHtml(powerFrom(vm))}</div>
                <div>${escapeHtml(formatTime(vm.updated_at))}</div>
              </div>
              <div class="vm-card-actions">
                ${rowActions(vm)}
              </div>
            </section>
          `,
        )
        .join('')}
    </div>
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
        <tr><th>Name</th><th>Input</th><th>Output</th><th>Rules</th><th>Updated</th><th></th></tr>
      </thead>
      <tbody>
        ${groups
          .map(
            (sg) => `
              <tr data-sg-name="${escapeHtml(sg.name)}">
                <td class="strong">${escapeHtml(sg.name)}</td>
                <td>${escapeHtml(normalizeSgPolicy(sg.policy_in))}</td>
                <td>${escapeHtml(normalizeSgPolicy(sg.policy_out))}</td>
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

function shortKey(key: string): string {
  const compact = key.replace(/\s+/g, ' ').trim()
  return compact.length > 96 ? `${compact.slice(0, 96)}…` : compact
}

function canonicalSSHKeyLine(value: string): string {
  return value
    .replace(/\r/g, '')
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .join(' ')
}

function renderSSHKeys() {
  const keys = state.sshKeys
  if (!keys.length) {
    els.sshTable.innerHTML = `<div class="empty">还没有 SSH Key。先新增一条，再回到 VM 创建里勾选。</div>`
    renderVmSSHKeyPicker()
    return
  }
  els.sshTable.innerHTML = `
    <table>
      <thead>
        <tr><th>Name</th><th>Public Key</th><th>Updated</th><th></th></tr>
      </thead>
      <tbody>
        ${keys
          .map(
            (key) => `
              <tr data-ssh-id="${key.id}">
                <td class="strong">${escapeHtml(key.name)}</td>
                <td class="mono">${escapeHtml(shortKey(key.public_key))}</td>
                <td>${escapeHtml(formatTime(key.updated_at))}</td>
                <td>
                  <div class="row-actions">
                    <button class="btn btn-mini btn-danger" data-ssh-action="delete" data-id="${key.id}">删除</button>
                  </div>
                </td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
  renderVmSSHKeyPicker()
}

function renderVmSSHKeyPicker() {
  const selected = new Set<string>()
  if (state.vmDialogVm) {
    for (const key of state.vmDialogVm.sshkeys) {
      selected.add(canonicalSSHKeyLine(key))
    }
  }
  const adminScope = state.me?.is_admin && (state.vmDialogMode === 'adopt' || state.vmDialogMode === 'edit')
  const keys = adminScope ? state.adminSSHKeys : state.sshKeys
  if (!keys.length) {
    els.vmSSHKeyList.innerHTML = `<div class="empty compact">没有可选 SSH Key。</div>`
    return
  }
  els.vmSSHKeyList.innerHTML = keys
    .map((key) => {
      const checked = selected.has(canonicalSSHKeyLine(key.public_key)) ? 'checked' : ''
      return `
        <label class="ssh-item">
          <input type="checkbox" data-ssh-key-id="${key.id}" ${checked} />
          <span>
            <span class="ssh-name">${escapeHtml(key.name)}</span>
            <span class="ssh-key mono">${escapeHtml(shortKey(key.public_key))}</span>
          </span>
        </label>
      `
    })
    .join('')
}

function renderVmLimitHint() {
  const cluster = currentCluster()
  if (!cluster) {
    els.vmLimitHint.innerHTML = ''
    return
  }
  const cpu = cluster.cpu.find((item) => item.key === els.vmCpuKey.value) ?? cluster.cpu[0]
  const storage = cluster.storage.find((item) => item.key === els.vmStorageKey.value) ?? cluster.storage[0]
  const quotaCPU = remainingQuotaValue('cpu', 'cpu')
  const quotaMemory = remainingQuotaValue('memory', 'memory')
  const quotaStorage = remainingQuotaValue('storage', 'storage')
  const quotaCount = remainingQuotaValue('number', 'count')
  const selectedVM = state.vmDialogVm
  const editBonusCPU = state.vmDialogMode === 'edit' && selectedVM ? configNumber(selectedVM.config, 'cpu_cores', 0) : 0
  const editBonusMemory = state.vmDialogMode === 'edit' && selectedVM ? configNumber(selectedVM.config, 'memory_gb', 0) : 0
  const editBonusDisk = state.vmDialogMode === 'edit' && selectedVM ? configNumber(selectedVM.config, 'disk_gb', 0) : 0
  const editBonusCount = state.vmDialogMode === 'edit' && selectedVM ? 1 : 0
  const cpuLimit = Math.max(0, Math.min(cpu?.limit ?? 0, quotaCPU + editBonusCPU))
  const memoryLimit = Math.max(0, Math.min(cpu?.memory_limit ?? cpu?.limit ?? 0, quotaMemory + editBonusMemory))
  const diskLimit = Math.max(0, Math.min(storage?.limit ?? 0, quotaStorage + editBonusDisk))
  const countLimit = Math.max(0, quotaCount + editBonusCount)
  const nodeText = cpu?.node?.length ? cpu.node.join(', ') : '全部节点'
  const modeText =
    state.vmDialogMode === 'create'
      ? '创建时'
      : state.vmDialogMode === 'adopt'
        ? '接管时'
        : '修改时'
  els.vmLimitHint.innerHTML = `
    <div class="limit-hint-title">${escapeHtml(modeText)}可选范围</div>
    <div class="limit-hint-grid">
      <span>CPU 核数 1 - ${cpuLimit || '-'}</span>
      <span>内存 GB 1 - ${memoryLimit || '-'}</span>
      <span>磁盘 GB 20 - ${diskLimit || '-'}</span>
      <span>VM 数量 1 - ${countLimit || '-'}</span>
      <span>可用节点 ${escapeHtml(nodeText)}</span>
    </div>
  `
  els.vmCpuCores.max = cpuLimit ? String(cpuLimit) : ''
  els.vmMemoryGB.max = memoryLimit ? String(memoryLimit) : ''
  els.vmDiskGB.max = diskLimit ? String(diskLimit) : ''
  els.vmCpuCores.min = '1'
  els.vmMemoryGB.min = '1'
  els.vmDiskGB.min = '20'
  if (state.vmDialogMode === 'create' && countLimit <= 0) {
    els.vmSubmit.disabled = true
  }
}

function renderVmFlowHint() {
  const cluster = currentCluster()
  const cpu = cluster?.cpu.find((item) => item.key === els.vmCpuKey.value) ?? cluster?.cpu[0]
  const storage = cluster?.storage.find((item) => item.key === els.vmStorageKey.value) ?? cluster?.storage[0]
  const title = cluster ? `当前 Cluster：${cluster.name}` : '先选择 Cluster'
  const quotaCPU = remainingQuotaValue('cpu', 'cpu')
  const quotaMemory = remainingQuotaValue('memory', 'memory')
  const quotaStorage = remainingQuotaValue('storage', 'storage')
  const body = [
    '1. 先选 Cluster，再选 CPU / 存储 / 模板。',
    cpu ? `CPU 可选：1 - ${Math.max(0, Math.min(cpu.limit || 0, quotaCPU || 0)) || '-'} 核，内存 1 - ${Math.max(0, Math.min(cpu.memory_limit ?? cpu.limit ?? 0, quotaMemory || 0)) || '-'} GB。` : 'CPU 暂无可选项。',
    storage ? `磁盘可选：20 - ${Math.max(0, Math.min(storage.limit || 0, quotaStorage || 0)) || '-'} GB。` : '磁盘暂无可选项。',
    '后面的选项只会在当前 Cluster 的范围里展开。',
  ]
  els.vmFlowHint.innerHTML = `
    <div class="flow-hint-title">${escapeHtml(title)}</div>
    <div class="flow-hint-body">
      ${body.map((item) => `<span>${escapeHtml(item)}</span>`).join('')}
    </div>
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
                <td>${escapeHtml(displayClusterName(tpl.cluster_key))}</td>
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
      <thead><tr><th>Name</th><th>Owner</th><th>Cluster</th><th>Node</th><th>VMID</th><th>IP</th><th>Managed</th><th>Sync</th><th></th></tr></thead>
      <tbody>
        ${state.allVMs
          .map(
            (vm) => `
              <tr>
                <td class="strong">${escapeHtml(vm.vmname)}</td>
                <td>${escapeHtml(vm.owner_username)}</td>
                <td>${escapeHtml(displayClusterName(vm.cluster_key))}</td>
                <td>${escapeHtml(vm.node ?? 'auto')}</td>
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

function renderAdminSecurityGroups() {
  if (!state.me?.is_admin) {
    els.adminSecurityGroupTable.innerHTML = `<div class="empty">管理员可见。</div>`
    return
  }
  if (!state.adminSecurityGroups.length) {
    els.adminSecurityGroupTable.innerHTML = `<div class="empty">没有安全组。</div>`
    return
  }
  els.adminSecurityGroupTable.innerHTML = `
    <table>
      <thead><tr><th>Name</th><th>Owner</th><th>Input</th><th>Output</th><th>Rules</th><th>Updated</th></tr></thead>
      <tbody>
        ${state.adminSecurityGroups
          .map(
            (sg) => `
              <tr>
                <td class="strong">${escapeHtml(sg.name)}</td>
                <td>${escapeHtml(sg.owner_username)}</td>
                <td>${escapeHtml(normalizeSgPolicy(sg.policy_in))}</td>
                <td>${escapeHtml(normalizeSgPolicy(sg.policy_out))}</td>
                <td>${sg.rules.length}</td>
                <td>${escapeHtml(formatTime(sg.updated_at))}</td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function renderAdminSSHKeys() {
  if (!state.me?.is_admin) {
    els.adminSshKeyTable.innerHTML = `<div class="empty">管理员可见。</div>`
    return
  }
  if (!state.adminSSHKeys.length) {
    els.adminSshKeyTable.innerHTML = `<div class="empty">没有 SSH Key。</div>`
    return
  }
  els.adminSshKeyTable.innerHTML = `
    <table>
      <thead><tr><th>Name</th><th>Owner</th><th>Public Key</th><th>Updated</th></tr></thead>
      <tbody>
        ${state.adminSSHKeys
          .map(
            (key) => `
              <tr>
                <td class="strong">${escapeHtml(key.name)}</td>
                <td>${escapeHtml(key.owner_username)}</td>
                <td class="mono">${escapeHtml(shortKey(key.public_key))}</td>
                <td>${escapeHtml(formatTime(key.updated_at))}</td>
              </tr>
            `,
          )
          .join('')}
      </tbody>
    </table>
  `
}

function renderAdminMaintenance() {
  if (!state.me?.is_admin) {
    els.adminMaintenanceTable.innerHTML = `<div class="empty">管理员可见。</div>`
    return
  }
  if (!state.maintenanceTasks.length) {
    els.adminMaintenanceTable.innerHTML = `<div class="empty">没有维护任务。</div>`
    return
  }
  els.adminMaintenanceTable.innerHTML = `
    <table>
      <thead><tr><th>Kind</th><th>Status</th><th>Payload</th><th>Error</th><th>Created</th><th>Updated</th></tr></thead>
      <tbody>
        ${state.maintenanceTasks
          .map(
            (task) => `
              <tr>
                <td class="strong">${escapeHtml(task.kind)}</td>
                <td>${taskStatusBadge(task.status)}</td>
                <td class="mono">${escapeHtml(JSON.stringify(task.payload, null, 2))}</td>
                <td class="mono">${escapeHtml(task.error || '-')}</td>
                <td>${escapeHtml(formatTime(task.created_at))}</td>
                <td>${escapeHtml(formatTime(task.updated_at))}</td>
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
  const isAdopt = state.vmDialogMode === 'adopt'
  const adminEdit = Boolean(state.me?.is_admin && state.vmDialogMode === 'edit')
  const adminScope = Boolean(state.me?.is_admin) && (isAdopt || adminEdit)
  const groups = adminScope ? state.adminSecurityGroups : state.securityGroups

  els.vmCluster.innerHTML = opts.map((cluster) => `<option value="${escapeHtml(cluster.key)}">${escapeHtml(cluster.name)}</option>`).join('')
  if (previousCluster) {
    els.vmCluster.value = previousCluster
  }
  const cluster = currentCluster()
  if (!cluster) return

  const cpuOptions = cluster.cpu
  const storageOptions = cluster.storage
  const bridgeOptions = cluster.network
  const cpuKeys = new Set(cpuOptions.map((cpu) => cpu.key))
  const storageKeys = new Set(storageOptions.map((storage) => storage.key))
  const bridgeKeys = new Set(bridgeOptions.map((network) => network.key))

  els.vmCpuKey.innerHTML = cpuOptions.map((cpu) => `<option value="${escapeHtml(cpu.key)}">${escapeHtml(cpu.name)}</option>`).join('')
  els.vmStorageKey.innerHTML = storageOptions.map((s) => `<option value="${escapeHtml(s.key)}">${escapeHtml(s.name)}</option>`).join('')
  els.vmBridgeKey.innerHTML = bridgeOptions.map((n) => `<option value="${escapeHtml(n.key)}">${escapeHtml(n.key)}</option>`).join('')
  const templates = isAdopt ? [] : state.templates.filter((tpl) => tpl.cluster_key === cluster.key)
  const currentTemplateId = state.vmDialogVm ? configNumber(state.vmDialogVm.config, 'template_vmid', 0) : 0
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

  els.vmSg.innerHTML = groups.length
    ? groups
        .map((sg) => {
          const value = isAdopt ? `${sg.owner_username}::${sg.name}` : sg.name
          return `<option value="${escapeHtml(value)}">${escapeHtml(sg.owner_username)} / ${escapeHtml(sg.name)}</option>`
        })
        .join('')
    : `<option value="">请先创建安全组</option>`

  if (previousCpu && cpuKeys.has(previousCpu)) els.vmCpuKey.value = previousCpu
  if (!els.vmCpuKey.value && cpuOptions[0]) els.vmCpuKey.value = cpuOptions[0].key
  if (previousStorage && storageKeys.has(previousStorage)) els.vmStorageKey.value = previousStorage
  if (!els.vmStorageKey.value && storageOptions[0]) els.vmStorageKey.value = storageOptions[0].key
  if (previousBridge && bridgeKeys.has(previousBridge)) els.vmBridgeKey.value = previousBridge
  if (!els.vmBridgeKey.value && bridgeOptions[0]) els.vmBridgeKey.value = bridgeOptions[0].key
  if (previousSg) els.vmSg.value = previousSg
  if (previousTemplate) els.vmTemplate.value = previousTemplate
  if (!isAdopt && !els.vmTemplate.value && templateOptions[0]) {
    els.vmTemplate.value = String(templateOptions[0].template_vmid)
  }
  const groupValues = new Set(groups.map((sg) => (isAdopt ? `${sg.owner_username}::${sg.name}` : sg.name)))
  if (!els.vmSg.value || !groupValues.has(els.vmSg.value)) {
    if (groups[0]) {
      els.vmSg.value = isAdopt ? `${groups[0].owner_username}::${groups[0].name}` : groups[0].name
    }
  }
  if (!els.vmSg.value && groups[0]) {
    els.vmSg.value = isAdopt ? `${groups[0].owner_username}::${groups[0].name}` : groups[0].name
  }
  els.vmSubmit.disabled = state.vmDialogMode === 'create' && (!templateOptions.length || !groups.length)

  const network = cluster.network[0]
  if (network) {
    if (state.vmDialogMode === 'create' && !state.vmDialogVm) {
      els.vmUESTC.checked = network.uestc === 'force'
    }
    els.vmUESTC.disabled = network.uestc === 'force'
  }
  els.vmOwnerRow.classList.toggle('hidden', !(isAdopt || adminEdit))
  els.vmTemplateRow.classList.toggle('hidden', isAdopt)
  els.vmCluster.disabled = isAdopt
  const managedEdit = state.vmDialogMode === 'edit'
  els.vmTemplate.disabled = managedEdit || isAdopt
  els.vmCpuKey.disabled = managedEdit || isAdopt
  els.vmStorageKey.disabled = managedEdit || isAdopt
  els.vmBridgeKey.disabled = managedEdit || isAdopt
  els.vmBootOrder.disabled = managedEdit || isAdopt
  els.vmDiskGB.disabled = managedEdit || isAdopt
  if (isAdopt) {
    els.vmSubmit.disabled = !groups.length
  }
  renderVmLimitHint()
  renderVmSSHKeyPicker()
  renderVmFlowHint()
}

function parseLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
}

function selectedSSHKeyIDs(): number[] {
  const nodes = Array.from(els.vmSSHKeyList.querySelectorAll<HTMLInputElement>('input[type="checkbox"][data-ssh-key-id]'))
  return nodes
    .filter((node) => node.checked)
    .map((node) => Number(node.dataset.sshKeyId))
    .filter((value) => Number.isFinite(value) && value > 0)
}

function resetVmDialog() {
  els.vmMode.value = 'create'
  els.vmDialogTitle.textContent = '新建 VM'
  els.vmId.value = ''
  els.vmOwner.value = ''
  els.vmName.value = ''
  els.vmCpuCores.value = '1'
  els.vmMemoryGB.value = '1'
  els.vmDiskGB.value = '20'
  els.vmPassword.value = ''
  els.vmShared.value = ''
  els.vmBootOrder.value = 'order=scsi0;ide0'
  els.vmUESTC.checked = false
  els.vmUESTC.disabled = false
  els.vmCluster.disabled = false
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
  els.vmCluster.disabled = false
  renderVmFormOptions()
  els.vmOwner.value = vm.owner_username
  els.vmTemplate.value = String(configNumber(vm.config, 'template_vmid', state.templates.find((tpl) => tpl.cluster_key === vm.cluster_key)?.template_vmid ?? 0) || '')
  els.vmName.value = vm.vmname
  els.vmCpuKey.value = configString(vm.config, 'cpu_key', cluster.cpu[0]?.key ?? '')
  els.vmCpuCores.value = String(configNumber(vm.config, 'cpu_cores', 1))
  els.vmMemoryGB.value = String(configNumber(vm.config, 'memory_gb', 1))
  els.vmStorageKey.value = configString(vm.config, 'storage_key', cluster.storage[0]?.key ?? '')
  els.vmDiskGB.value = String(configNumber(vm.config, 'disk_gb', 20))
  els.vmBridgeKey.value = configString(vm.config, 'bridge_key', cluster.network[0]?.key ?? '')
  els.vmSg.value = vm.security_group_name
  els.vmPassword.value = ''
  els.vmBootOrder.value = configString(vm.config, 'boot_order', configString(vm.config, 'boot', 'order=scsi0;ide0'))
  els.vmUESTC.checked = Boolean(vm.uestc_restricted)
  els.vmUESTC.disabled = Boolean(cluster.network[0]?.uestc === 'force')
  els.vmShared.value = vm.shared_usernames.join('\n')
  renderVmSSHKeyPicker()
}

function fillAdoptVmDialog(vm: VM) {
  const cluster = getCluster(vm.cluster_key) ?? state.options?.clusters[0]
  if (!cluster) return
  state.vmDialogMode = 'adopt'
  state.vmDialogVm = vm
  els.vmMode.value = 'adopt'
  els.vmDialogTitle.textContent = `接管 VM #${vm.vmid}`
  els.vmId.value = String(vm.id)
  els.vmCluster.value = vm.cluster_key
  renderVmFormOptions()
  els.vmOwner.value = ''
  els.vmName.value = vm.vmname
  els.vmCpuKey.value = configString(vm.config, 'cpu_key', cluster.cpu[0]?.key ?? '')
  els.vmCpuCores.value = String(configNumber(vm.config, 'cpu_cores', 1))
  els.vmMemoryGB.value = String(configNumber(vm.config, 'memory_gb', 1))
  els.vmStorageKey.value = configString(vm.config, 'storage_key', cluster.storage[0]?.key ?? '')
  els.vmDiskGB.value = String(configNumber(vm.config, 'disk_gb', 20))
  els.vmBridgeKey.value = configString(vm.config, 'bridge_key', cluster.network[0]?.key ?? '')
  els.vmPassword.value = ''
  els.vmBootOrder.value = configString(vm.config, 'boot_order', configString(vm.config, 'boot', 'order=scsi0;ide0'))
  els.vmUESTC.checked = Boolean(vm.uestc_restricted)
  els.vmUESTC.disabled = Boolean(cluster.network[0]?.uestc === 'force')
  els.vmShared.value = vm.shared_usernames.join('\n')
  els.vmCluster.disabled = true
  renderVmSSHKeyPicker()
}

function openVmDialog(mode: 'create' | 'edit' | 'adopt', vm?: VM) {
  state.vmDialogMode = mode
  if (mode === 'create') {
    resetVmDialog()
  } else if (vm) {
    if (mode === 'adopt') {
      fillAdoptVmDialog(vm)
    } else {
      fillVmDialog(vm)
    }
  }
  showDialog(els.vmDialog)
}

function addRuleRow(rule: Rule = {
  direction: 'in',
  action: 'accept',
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
          <option value="accept" ${rule.action === 'accept' ? 'selected' : ''}>accept</option>
          <option value="drop" ${rule.action === 'drop' ? 'selected' : ''}>drop</option>
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
  state.sgPolicyIn = 'accept'
  state.sgPolicyOut = 'accept'
  els.sgDialogTitle.textContent = '新建 Security Group'
  els.sgName.value = ''
  els.sgPolicyIn.value = state.sgPolicyIn
  els.sgPolicyOut.value = state.sgPolicyOut
  renderRuleRows()
}

function fillSgDialog(group: SecurityGroup) {
  state.sgDialogMode = 'edit'
  state.sgDialogGroup = group
  state.sgRules = group.rules.length ? JSON.parse(JSON.stringify(group.rules)).map(normalizeRule) : []
  state.sgPolicyIn = normalizeSgPolicy(group.policy_in) as 'accept' | 'drop'
  state.sgPolicyOut = normalizeSgPolicy(group.policy_out) as 'accept' | 'drop'
  els.sgDialogTitle.textContent = `编辑 ${group.name}`
  els.sgName.value = group.name
  els.sgPolicyIn.value = state.sgPolicyIn
  els.sgPolicyOut.value = state.sgPolicyOut
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

function resetSSHDialog() {
  state.sshDialogMode = 'create'
  state.sshDialogKey = null
  els.sshDialogTitle.textContent = '新增 SSH Key'
  els.sshId.value = ''
  els.sshName.value = ''
  els.sshKey.value = ''
}

function fillSSHDialog(key: SSHKey) {
  state.sshDialogMode = 'edit'
  state.sshDialogKey = key
  els.sshDialogTitle.textContent = `编辑 SSH Key #${key.id}`
  els.sshId.value = String(key.id)
  els.sshName.value = key.name
  els.sshKey.value = key.public_key
}

function openSSHDialog(mode: 'create' | 'edit', key?: SSHKey) {
  if (mode === 'create') {
    resetSSHDialog()
  } else if (key) {
    fillSSHDialog(key)
  }
  showDialog(els.sshDialog)
}

function renderDetail(vm: VM) {
  const deleteInfo = isDeletePending(vm)
    ? `<div><span class="label">删除时间</span><div>${escapeHtml(formatTime(vm.delete_execute_after))}</div></div>`
    : ''
  const passwordBlock = vm.password
    ? `<div class="detail-block"><h4>Login Password</h4><div class="mono password-value">${escapeHtml(vm.password)}</div></div>`
    : ''
  els.detailContent.innerHTML = `
    <div class="detail-grid">
      <div><span class="label">Name</span><div>${escapeHtml(vm.vmname)}</div></div>
      <div><span class="label">Owner</span><div>${escapeHtml(vm.owner_username)}</div></div>
      <div><span class="label">Cluster</span><div>${escapeHtml(displayClusterName(vm.cluster_key))}</div></div>
      <div><span class="label">Node</span><div>${escapeHtml(vm.node ?? 'auto')}</div></div>
      <div><span class="label">VMID</span><div>${vm.vmid}</div></div>
      <div><span class="label">IP</span><div class="mono">${escapeHtml(vm.ip)}</div></div>
      <div><span class="label">Security Group</span><div>${escapeHtml(vm.security_group_name)}</div></div>
      <div><span class="label">Boot Order</span><div>${escapeHtml(configString(vm.config, 'boot_order', configString(vm.config, 'boot', 'order=scsi0;ide0')))}</div></div>
      <div><span class="label">Sync State</span><div>${statusBadge(vm.sync_state)}</div></div>
      <div><span class="label">Power</span><div>${escapeHtml(realPowerFrom(vm))}</div></div>
      <div><span class="label">任务队列</span><div>${vm.task_queue_paused ? '<span class="badge warn">已暂停</span>' : '<span class="badge ok">运行中</span>'}</div></div>
      ${deleteInfo}
    </div>
    ${passwordBlock}
    <div class="detail-block">
      <h4>状态概览</h4>
      <div class="detail-grid compact">
        <div><span class="label">同步</span><div>${statusBadge(vm.sync_state)}</div></div>
        <div><span class="label">实际电源</span><div>${escapeHtml(realPowerFrom(vm))}</div></div>
        <div><span class="label">版本</span><div>${vm.version}</div></div>
        <div><span class="label">管理</span><div>${vm.managed ? '受管' : '不受管'}</div></div>
      </div>
    </div>
    <div class="detail-block">
      <h4>任务队列</h4>
      ${
      vm.tasks?.length
          ? `<div class="task-list">
              ${vm.tasks
                .map((task) => {
                  const payload = JSON.stringify(task.payload, null, 2)
                  const error = task.error ? `<div class="task-error mono">${escapeHtml(task.error)}</div>` : ''
                  const taskActions =
                    task.status === 'failed'
                      ? `<button type="button" class="btn btn-mini" data-task-action="retry" data-task-id="${task.id}" data-vm-id="${vm.id}">重试</button>`
                      : ''
                  return `
                    <section class="task-item">
                      <div class="task-head">
                        <div>
                          <div class="strong">${escapeHtml(taskKindLabel(task.kind))} #${task.seq}</div>
                          <div class="task-meta mono">#${task.id} · ${escapeHtml(formatTime(task.created_at))}</div>
                        </div>
                        <div class="task-head-actions">
                          ${taskStatusBadge(task.status)}
                          ${taskActions}
                        </div>
                      </div>
                      <div class="task-payload mono">${escapeHtml(payload)}</div>
                      ${error}
                    </section>
                  `
                })
                .join('')}
            </div>`
          : `<div class="empty compact">没有任务记录。</div>`
      }
    </div>
  `
}

function currentVmById(id: number): VM | undefined {
  return state.vms.find((vm) => vm.id === id) ?? state.allVMs.find((vm) => vm.id === id)
}

function currentSSHKeyById(id: number): SSHKey | undefined {
  return state.sshKeys.find((key) => key.id === id)
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

async function loadSSHKeys() {
  const data = await api<{ items: SSHKey[] }>('/api/ssh-keys')
  state.sshKeys = data.items
  renderSSHKeys()
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

async function loadAdminSecurityGroups() {
  if (!state.me?.is_admin || state.adminSecurityGroups.length) return
  const data = await api<{ items: SecurityGroup[] }>('/api/admin/security-groups')
  state.adminSecurityGroups = data.items
  renderAdminSecurityGroups()
}

async function loadAdminSSHKeys() {
  if (!state.me?.is_admin || state.adminSSHKeys.length) return
  const data = await api<{ items: SSHKey[] }>('/api/admin/ssh-keys')
  state.adminSSHKeys = data.items
  renderAdminSSHKeys()
}

async function loadAdminMaintenance() {
  if (!state.me?.is_admin || state.maintenanceTasks.length) return
  const data = await api<{ items: MaintenanceTask[] }>('/api/admin/maintenance-tasks')
  state.maintenanceTasks = data.items
  renderAdminMaintenance()
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
    case 'ssh':
      await loadSSHKeys()
      break
    case 'templates':
      await loadTemplates()
      break
  case 'admin':
      if (state.adminTab === 'users') {
        await loadAdminUsers()
      } else if (state.adminTab === 'vms') {
        await loadAdminVMs()
      } else if (state.adminTab === 'security-groups') {
        await loadAdminSecurityGroups()
      } else if (state.adminTab === 'maintenance') {
        await loadAdminMaintenance()
      } else {
        await loadAdminSSHKeys()
      }
      break
  }
}

function startVmAutoRefresh() {
  window.setInterval(async () => {
    if (!state.me || state.activeTab !== 'vms' || document.visibilityState !== 'visible') {
      return
    }
    try {
      await loadVMs()
      renderSummary()
      if (state.detailVmId && els.detailDialog.open) {
        const detail = await api<VM>(`/api/vms/${state.detailVmId}`)
        renderDetail(detail)
      }
    } catch (err) {
      flash((err as Error).message, 'error')
    }
  }, 10000)
}

async function prepareVmDialogData(mode: 'create' | 'edit' | 'adopt') {
  await loadOptions()
  if (mode === 'adopt' && state.me?.is_admin) {
    await Promise.all([loadAdminSecurityGroups(), loadAdminSSHKeys()])
  } else if (mode === 'edit' && state.me?.is_admin) {
    await Promise.all([loadTemplates(), loadAdminSecurityGroups(), loadAdminSSHKeys()])
  } else {
    await Promise.all([loadSecurityGroups(), loadSSHKeys(), loadTemplates()])
  }
  renderVmFormOptions()
}

function buildCreateVmPayload(): Record<string, unknown> {
  return {
    cluster_key: els.vmCluster.value,
    vmname: els.vmName.value.trim(),
    password: els.vmPassword.value.trim() || undefined,
    cpu_key: els.vmCpuKey.value,
    cpu_cores: Number(els.vmCpuCores.value),
    memory_gb: Number(els.vmMemoryGB.value),
    storage_key: els.vmStorageKey.value,
    disk_gb: Number(els.vmDiskGB.value),
    bridge_key: els.vmBridgeKey.value,
    boot_order: els.vmBootOrder.value,
    template_vmid: Number(els.vmTemplate.value),
    ssh_key_ids: selectedSSHKeyIDs(),
    shared_usernames: parseLines(els.vmShared.value),
    security_group_name: els.vmSg.value,
    uestc_restricted: els.vmUESTC.checked,
  }
}

function buildEditVmPayload(vm: VM): Record<string, unknown> {
  const ownerOnly = userOwns(vm) || Boolean(state.me?.is_admin)
  if (!ownerOnly) {
    return {}
  }
  const payload: Record<string, unknown> = {
    vmname: els.vmName.value.trim(),
    password: els.vmPassword.value.trim() || undefined,
    cpu_key: els.vmCpuKey.value,
    cpu_cores: Number(els.vmCpuCores.value),
    memory_gb: Number(els.vmMemoryGB.value),
    storage_key: els.vmStorageKey.value,
    disk_gb: Number(els.vmDiskGB.value),
    bridge_key: els.vmBridgeKey.value,
    boot_order: els.vmBootOrder.value,
    template_vmid: Number(els.vmTemplate.value),
    ssh_key_ids: selectedSSHKeyIDs(),
    shared_usernames: parseLines(els.vmShared.value),
    security_group_name: els.vmSg.value,
    uestc_restricted: els.vmUESTC.checked,
  }
  if (state.me?.is_admin) {
    const owner = els.vmOwner.value.trim()
    if (owner && owner !== vm.owner_username) {
      payload.owner_username = owner
    }
  }
  return payload
}

function buildAdoptVmPayload(): Record<string, unknown> {
  const [securityGroupOwner, securityGroupName] = els.vmSg.value.includes('::')
    ? els.vmSg.value.split('::', 2)
    : ['', els.vmSg.value]
  return {
    owner_username: els.vmOwner.value.trim(),
    vmname: els.vmName.value.trim(),
    password: els.vmPassword.value.trim() || undefined,
    cpu_key: els.vmCpuKey.value,
    cpu_cores: Number(els.vmCpuCores.value),
    memory_gb: Number(els.vmMemoryGB.value),
    storage_key: els.vmStorageKey.value,
    disk_gb: Number(els.vmDiskGB.value),
    bridge_key: els.vmBridgeKey.value,
    boot_order: els.vmBootOrder.value,
    ssh_key_ids: selectedSSHKeyIDs(),
    shared_usernames: parseLines(els.vmShared.value),
    security_group_owner: securityGroupOwner,
    security_group_name: securityGroupName,
    uestc_restricted: els.vmUESTC.checked,
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
    } else if (state.vmDialogMode === 'adopt' && state.vmDialogVm) {
      await api(`/api/admin/vms/${state.vmDialogVm.id}/adopt`, {
        method: 'POST',
        body: JSON.stringify(buildAdoptVmPayload()),
      })
      flash('VM 已接管，等待 worker 同步。', 'ok')
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
      action: field('action') as 'accept' | 'drop',
      ethertype: field('ethertype') as 'ipv4' | 'ipv6',
      protocol: field('protocol') as 'tcp' | 'udp' | 'icmp',
      cidr: field('cidr'),
      port_start: toInt('port_start'),
      port_end: toInt('port_end'),
    }
  })
}

function normalizeRule(rule: Rule): Rule {
  return {
    ...rule,
    action: normalizeRuleAction(rule.action),
    direction: rule.direction === 'out' ? 'out' : 'in',
    ethertype: rule.ethertype === 'ipv6' ? 'ipv6' : 'ipv4',
    protocol: rule.protocol === 'udp' ? 'udp' : rule.protocol === 'icmp' ? 'icmp' : 'tcp',
  }
}

function normalizeRuleAction(value: string): 'accept' | 'drop' {
  return value.trim().toLowerCase() === 'drop' ? 'drop' : 'accept'
}

async function submitSgForm(event: SubmitEvent) {
  event.preventDefault()
  try {
    const payload = {
      name: els.sgName.value.trim(),
      policy_in: normalizeSgPolicy(els.sgPolicyIn.value),
      policy_out: normalizeSgPolicy(els.sgPolicyOut.value),
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

async function submitSSHForm(event: SubmitEvent) {
  event.preventDefault()
  try {
    const payload = {
      name: els.sshName.value.trim(),
      public_key: els.sshKey.value.trim(),
    }
    if (state.sshDialogMode === 'create') {
      await api('/api/ssh-keys', { method: 'POST', body: JSON.stringify(payload) })
      flash('SSH Key 已创建。', 'ok')
    } else if (state.sshDialogKey) {
      flash('SSH Key 暂不支持编辑，请删除后重新新增。', 'info')
      closeDialog('ssh-dialog')
      return
    }
    closeDialog('ssh-dialog')
    state.sshKeys = []
    await loadSSHKeys()
    if (state.vmDialogVm) {
      renderVmSSHKeyPicker()
    }
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

async function openVmDetails(vm: VM) {
  try {
    state.detailVmId = vm.id
    const detail = await api<VM>(`/api/vms/${vm.id}`)
    renderDetail(detail)
    els.detailDialog.showModal()
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function retryVmTask(vmId: number, taskId: number) {
  try {
    await api(`/api/vms/${vmId}/tasks/${taskId}/retry`, { method: 'POST' })
    flash('任务已重新入队。', 'ok')
    await openDetailById(vmId)
  } catch (err) {
    flash((err as Error).message, 'error')
  }
}

async function openDetailById(id: number) {
  const vm = currentVmById(id)
  if (!vm) return
  const detail = await api<VM>(`/api/vms/${vm.id}`)
  renderDetail(detail)
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
      const tab = button.dataset.adminTab
      state.adminTab = tab === 'vms' || tab === 'security-groups' || tab === 'ssh-keys' || tab === 'maintenance' ? tab : 'users'
      renderTabs()
      try {
        await loadActiveTab()
      } catch (err) {
        flash((err as Error).message, 'error')
      }
    })
  })
  els.createVmBtn.addEventListener('click', async () => {
    try {
      state.vmDialogVm = null
      await prepareVmDialogData('create')
      openVmDialog('create')
    } catch (err) {
      flash((err as Error).message, 'error')
    }
  })
  els.createSgBtn.addEventListener('click', () => {
    state.sgDialogGroup = null
    openSgDialog('create')
  })
  els.createSshBtn.addEventListener('click', () => {
    state.sshDialogKey = null
    openSSHDialog('create')
  })
  els.sshShortcutBtn.addEventListener('click', () => {
    els.vmDialog.close()
    state.activeTab = 'ssh'
    renderTabs()
    void loadActiveTab()
  })
  els.vmCluster.addEventListener('change', () => {
    renderVmFormOptions()
  })
  els.vmForm.addEventListener('submit', submitVmForm)
  els.sshForm.addEventListener('submit', submitSSHForm)
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
          await openVmDetails(vm)
          break
        case 'edit':
          try {
            state.vmDialogVm = vm
            await prepareVmDialogData('edit')
            openVmDialog('edit', vm)
          } catch (err) {
            flash((err as Error).message, 'error')
          }
          break
        case 'adopt':
          try {
            state.vmDialogVm = vm
            await prepareVmDialogData('adopt')
            openVmDialog('adopt', vm)
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

    const taskAction = target.closest<HTMLElement>('[data-task-action]')
    if (taskAction) {
      const taskId = Number(taskAction.dataset.taskId)
      const vmId = Number(taskAction.dataset.vmId || state.detailVmId || 0)
      if (!vmId || !Number.isFinite(taskId) || taskId <= 0) return
      switch (taskAction.dataset.taskAction) {
        case 'retry':
          await retryVmTask(vmId, taskId)
          break
      }
      return
    }

    const sshAction = target.closest<HTMLElement>('[data-ssh-action]')
    if (sshAction) {
      const id = Number(sshAction.dataset.id)
      const key = currentSSHKeyById(id)
      if (!key) return
      switch (sshAction.dataset.sshAction) {
        case 'delete':
          if (!confirm(`删除 SSH Key "${key.name}" 吗？`)) return
          try {
            await api(`/api/ssh-keys/${id}`, { method: 'DELETE' })
            flash('SSH Key 已删除。', 'ok')
            state.sshKeys = state.sshKeys.filter((item) => item.id !== id)
            renderSSHKeys()
          } catch (err) {
            flash((err as Error).message, 'error')
          }
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
      renderAdminSecurityGroups()
      renderAdminSSHKeys()
      renderAdminMaintenance()
    }
  } catch (err) {
    flash((err as Error).message, 'error')
  }
  renderSummary()
  renderTabs()
  startVmAutoRefresh()
}

init().catch((err) => {
  flash((err as Error).message, 'error')
})

function normalizeSgPolicy(value: string): 'accept' | 'drop' {
  switch (value.trim().toLowerCase()) {
    case 'drop':
      return 'drop'
    case 'accept':
    default:
      return 'accept'
  }
}
