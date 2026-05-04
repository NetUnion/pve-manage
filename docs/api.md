# API 参考

本文档描述 Cloud Manage 当前对外提供的 HTTP 接口。除登录跳转外，所有接口均返回 JSON。

## 通用约定

- 所有业务接口都要求先登录。
- 普通接口返回 `200/201` 和 JSON body。
- 出错时返回 JSON：

```json
{ "error": "reason" }
```

- 前端使用 `/api/config/options` 作为选项源，不在前端硬编码 cluster / cpu / storage。

## 认证与会话

### `GET /auth/login`

发起 OIDC 登录，通常会重定向到 OIDC Provider。

### `GET /auth/oidc/callback`

OIDC 回调入口。登录完成后写入会话并返回前端。

### `GET /auth/callback`

`/auth/oidc/callback` 的兼容别名。

### `POST /auth/logout`

清理当前会话。

## 健康检查

### `GET /healthz`

检查数据库连通性。

返回：

```json
{ "ok": true }
```

失败时：

```json
{ "ok": false, "error": "..." }
```

## 配置与用户信息

### `GET /api/config/options`

返回前端可选配置列表。

返回结构：

```json
{
  "clusters": [
    {
      "key": "cnss_sg2",
      "name": "CNSS Server Group 2",
      "limit": 1000,
      "start_vmid": 1000,
      "cpu": [...],
      "storage": [...],
      "network": [...]
    }
  ]
}
```

字段含义：

- `clusters[].cpu[]`：CPU 类型、核数上限、内存上限、可用节点。
- `clusters[].storage[]`：存储类型与磁盘上限。
- `clusters[].network[]`：bridge、UESTC、IPv4/IPv6、ipfilter 等信息。

### `GET /api/me`

返回当前登录用户的信息、配额和已用资源。

返回体大致为：

```json
{
  "username": "xeam",
  "email": "...",
  "name": "XeAm",
  "groups": ["..."],
  "is_admin": false,
  "quota": {...},
  "usage": {...}
}
```

其中 `quota` 来自配置文件里的用户组配额，`usage` 来自数据库里当前用户拥有的受管 VM。

## VM 接口

### `GET /api/vms`

返回当前用户可见的 VM 列表。

可见性规则：

- owner 可见
- shared user 可见
- admin 可见

返回：

```json
{ "items": [VM, ...] }
```

### `POST /api/vms`

创建新 VM。

核心请求字段：

- `cluster_key`
- `vmname`
- `cpu_key`
- `cpu_cores`
- `memory_gb`
- `storage_key`
- `disk_gb`
- `bridge_key`
- `boot_order`
- `template_vmid`
- `sshkeys` 或 `ssh_key_ids`
- `shared_usernames`
- `security_group_name`
- `uestc_restricted`
- `power`

规则：

- `vmname` 存库时保持原名，PVE 侧实际创建时再拼 owner 前缀。
- `disk_gb` 至少 20。
- 所有配置都必须在 `config/config.yaml` 中合法。
- 创建后会入 `provision` 任务。

返回：

- `201 Created`
- VM 记录
- 以及创建时生成的 `password`

### `GET /api/vms/{id}`

返回单台 VM 的完整详情和任务列表。

返回结构：

```json
{
  ...VM,
  "password": "...",
  "tasks": [ ... ]
}
```

### `PATCH /api/vms/{id}`

编辑 VM。

允许修改的内容取决于身份：

- owner / admin：可修改大部分业务字段。
- shared user：只能改 power。

后端会做这些约束：

- `cluster_key / storage_key / bridge_key / template_vmid` 创建后不可改。
- `disk_gb` 只能增大，不能缩小。
- `cpu_cores / memory_gb` 改动会进入任务队列。
- `power` 通过专用 power 任务处理。

### `DELETE /api/vms/{id}`

请求删除 VM。

行为：

- 不立即物理删除。
- 先进入等待删除窗口。
- worker 到期后再执行实际删除。

### `POST /api/vms/{id}/restore`

把等待删除中的 VM 恢复回正常状态。

### `POST /api/vms/{id}/delete-now`

跳过等待窗口，立即进入删除执行流程。

### `POST /api/vms/{id}/power`

提交电源任务。

请求体：

```json
{ "power": "running" | "stopped" | "reboot" }
```

说明：

- `running`：开机。
- `stopped`：关机。
- `reboot`：重启。

### `POST /api/vms/{id}/tasks/pause`

暂停该 VM 的任务队列。

### `POST /api/vms/{id}/tasks/resume`

恢复该 VM 的任务队列。

### `POST /api/vms/{id}/tasks/{task_id}/retry`

重试失败任务。

仅失败任务可重试。

## SSH Key 接口

### `GET /api/ssh-keys`

返回当前用户自己的 SSH Keys。

### `POST /api/ssh-keys`

新增 SSH Key。

请求体：

```json
{
  "name": "laptop",
  "public_key": "ssh-ed25519 AAAA..."
}
```

### `DELETE /api/ssh-keys/{id}`

删除当前用户自己的 SSH Key。

## Security Group 接口

### `GET /api/security-groups`

返回当前用户自己的 security groups。

### `POST /api/security-groups`

新增 security group。

请求体：

```json
{
  "name": "main",
  "policy_in": "accept",
  "policy_out": "drop",
  "rules": [
    {
      "direction": "in",
      "action": "accept",
      "ethertype": "ipv4",
      "protocol": "tcp",
      "cidr": "0.0.0.0/0",
      "port_start": 22,
      "port_end": 22
    }
  ]
}
```

### `GET /api/security-groups/{name}`

查看单个 security group。

### `PATCH /api/security-groups/{name}`

更新 security group。

行为：

- 更新组本身。
- 重新标记引用该组的受管 VM 为待同步。

### `DELETE /api/security-groups/{name}`

删除 security group。

前置条件：

- 该组不能仍被任何受管 VM 使用。

## Templates 接口

### `GET /api/templates`

返回 worker 从 PVE 扫描到的模板列表。

可按 `cluster_key` 过滤：

```text
/api/templates?cluster_key=cnss_sg2
```

## 管理员接口

以下接口仅管理员可用。

### `GET /api/admin/vms`

返回全量 VM，包括 unmanaged inventory。

### `PATCH /api/admin/vms/{id}`

管理员修改 VM IP。

请求体：

```json
{ "ip": "10.12.65.3/18" }
```

行为：

- 只允许受管 VM。
- 修改后入队，必要时触发重启。

### `POST /api/admin/vms/{id}/adopt`

管理员把 unmanaged VM 接管成受管 VM。

请求体包含：

- `owner_username`
- `vmname`
- `ip`
- `cpu_key`
- `cpu_cores`
- `memory_gb`
- `storage_key`
- `disk_gb`
- `bridge_key`
- `boot_order`
- `sshkeys` / `ssh_key_ids`
- `shared_usernames`
- `security_group_owner`
- `security_group_name`
- `uestc_restricted`
- `power`
- `password`（可选）

行为：

- 先把 VM 变成受管。
- 写入配置快照。
- 入队 `apply`。
- 如果需要，再入队 `reboot`。

### `GET /api/admin/users`

返回全量用户。

### `GET /api/admin/security-groups`

返回全量 security groups。

### `GET /api/admin/ssh-keys`

返回全量 SSH Keys。

### `GET /api/admin/maintenance-tasks`

返回全量维护任务记录。

## 任务与状态说明

### VM 任务状态

- `pending`
- `running`
- `done`
- `failed`
- `canceled`

### VM 同步状态

`sync_state` 是 VM 总览状态，前端会展示为：

- `pending`
- `syncing`
- `synced`
- `failed`
- `deleting`
- `unmanaged`

### 维护任务状态

维护任务复用类似的状态语义，前端通过 Admin 页查看。

