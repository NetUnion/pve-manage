# SQLite 表结构说明

本文档描述 Cloud Manage 当前使用的 SQLite 表、主要字段和用途。所有时间字段都以 RFC3339Nano 文本格式存储。

## 迁移与版本

程序启动时会读取 `schema_migrations`，按版本顺序执行迁移。当前迁移版本已经覆盖以下表：

- `users`
- `security_groups`
- `templates`
- `vms`
- `ssh_keys`
- `node_metrics`
- `vm_tasks`
- `maintenance_tasks`

## schema_migrations

记录已经执行过的迁移版本。

- `version`：主键，迁移版本号。
- `name`：迁移名称。
- `applied_at`：应用时间。

用途：

- 确保迁移只执行一次。
- 让新实例和老实例都能按相同顺序升级。

## users

OIDC 登录后的用户表。

字段：

- `id`
- `username`
- `email`
- `name`
- `groups_json`
- `is_active`
- `is_admin`
- `created_at`
- `updated_at`
- `last_login_at`

说明：

- `username` 是系统主键语义上的身份标识。
- `groups_json` 保存 OIDC groups 原始列表。
- `is_admin` 由配置文件里的 `user.admin_group` 决定。

主要索引：

- `UNIQUE(username)`

## security_groups

用户自定义安全组。

字段：

- `id`
- `owner_username`
- `name`
- `rules_json`
- `policy_in`
- `policy_out`
- `created_at`
- `updated_at`

说明：

- `policy_in / policy_out` 现在使用 `accept / drop` 语义。
- `rules_json` 保存规则数组，规则包含：
  - `direction`
  - `action`
  - `ethertype`
  - `protocol`
  - `cidr`
  - `port_start`
  - `port_end`

主要索引：

- `UNIQUE(owner_username, name)`

使用规则：

- VM 创建和普通用户编辑时只能引用当前登录用户自己的 security group。
- 管理员从 `Admin / All VMs` 编辑或接管 VM 时，前端按目标 owner 过滤 security group；后端也按目标 owner 校验，防止把其他用户的 security group 写入 VM。

## templates

worker 从 PVE `template-pool` 扫描出来的模板记录。

字段：

- `id`
- `cluster_key`
- `template_vmid`
- `name`
- `description`
- `os_type`
- `real_status_json`
- `last_seen_at`
- `created_at`
- `updated_at`

说明：

- `real_status_json` 保存模板的 PVE 实际状态快照。
- `last_seen_at` 表示最近一次扫描到这个模板的时间。

主要索引：

- `UNIQUE(cluster_key, template_vmid)`

## vms

最重要的控制面表。它记录了每台 VM 的身份、配置快照、任务状态、实际状态和删除语义。

字段：

- `id`
- `owner_username`
- `cluster_key`
- `vmid`
- `vmname`
- `ip`
- `node`
- `target_node`
- `password`
- `sshkeys_json`
- `shared_usernames_json`
- `security_group_name`
- `uestc_restricted`
- `quota_exempt`
- `config_json`
- `prefer_status_json`
- `real_status_json`
- `sync_state`
- `sync_error`
- `version`
- `created_at`
- `updated_at`
- `deleted_at`
- `delete_requested_at`
- `delete_execute_after`
- `managed`
- `task_queue_paused`
- `metrics_json`

关键说明：

- `owner_username`：VM 所有者。
- `cluster_key`：所属 cluster 的配置 key。
- `vmid`：PVE VMID，cluster 内唯一。
- `vmname`：用户输入的原名，不在库里自动拼 owner 前缀。
- `ip`：后端分配的 IPv4/CIDR。
- `node`：当前实际所在 node。
- `target_node`：期望落点 node，创建和接管时用于调度。
- `password`：root 登录密码，普通用户仅自己可见。
- `sshkeys_json`：SSH key 列表。
- `shared_usernames_json`：共享用户列表。
- `security_group_name`：绑定的安全组名称。
- `uestc_restricted`：是否强制网络限制。
- `quota_exempt`：是否标记为超限 VM。只有管理员可在 All VMs 的编辑/接管流程中设置；为 true 时，该 VM 可以超过所选 CPU、内存、磁盘配置上限，且不计入 owner 的 VM 数量、CPU、内存、存储总配额。
- `config_json`：业务配置快照。
- `prefer_status_json`：期望状态/参数快照。
- `real_status_json`：PVE 实际状态快照。
- `sync_state`：同步状态，例如 `pending / syncing / synced / failed / deleting / unmanaged`。
- `version`：每次业务变更递增，用于前后端展示和重入队。
- `delete_requested_at / delete_execute_after`：删除延迟窗口。
- `managed`：是否被 Cloud Manage 接管。
- `task_queue_paused`：任务队列是否暂停。
- `metrics_json`：最近采集到的指标历史。

主要索引：

- `UNIQUE(cluster_key, vmid)`，但当前版本对已删除记录使用 partial unique index，避免删除后的 VMID 永久占号。
- `idx_vms_active_cluster_vmid`

业务语义：

- `managed = 0`：unmanaged inventory，只用于库存和冲突检查。
- `managed = 1`：受管 VM，由任务队列驱动。
- 删除不是立即物理删除，而是先进入等待删除窗口。

## ssh_keys

每个用户保存的 SSH 公钥。

字段：

- `id`
- `owner_username`
- `name`
- `public_key`
- `created_at`
- `updated_at`

主要索引：

- `UNIQUE(owner_username, name)`
- `UNIQUE(owner_username, public_key)`

说明：

- 前端创建 VM 时建议从这里选择 key，而不是直接手填。
- VM 创建时只能引用当前登录用户自己的 key。
- 普通用户编辑 VM 时只能引用自己的 key。
- 管理员可以通过 Admin 页面查看全量 key；从 `Admin / All VMs` 编辑或接管 VM 时，表单按目标 owner 过滤 key，并显示 `owner / name` 以区分重名。
- 后端保存 VM 的 `ssh_key_ids` 时按目标 owner 查询和校验，避免把其他用户的 key 写入 VM。

## node_metrics

节点静态负载采样表。

字段：

- `id`
- `cluster_key`
- `node`
- `cpu_ratio`
- `mem_ratio`
- `recorded_at`
- `created_at`

说明：

- worker 会周期性采样节点负载。
- 这些数据用于创建 VM 时的节点均衡。

主要索引：

- `UNIQUE(cluster_key, node, recorded_at)`
- `idx_node_metrics_cluster_node_recorded`

## vm_tasks

单台 VM 的任务队列表。

字段：

- `id`
- `vm_id`
- `seq`
- `kind`
- `payload_json`
- `status`
- `error`
- `created_at`
- `updated_at`
- `started_at`
- `finished_at`
- `attempt_count`

任务类型示例：

- `provision`
- `apply`
- `power`
- `reboot`
- `delete`

说明：

- `seq` 表示同一台 VM 内的执行顺序。
- worker 按 `seq` 串行消费。
- 前一个任务未完成时，后续任务不会被执行。
- `attempt_count` 统计任务总执行次数；初次执行失败后最多自动重试 5 次。
- 5 次自动重试分别在失败后等待 1、2、4、8、16 分钟。
- 人工重试会把失败任务重新置为 `pending`，不受自动重试次数上限阻止。

主要索引：

- `UNIQUE(vm_id, seq)`
- `idx_vm_tasks_vm_status_seq`

生命周期：

- `pending`：待执行
- `running`：执行中
- `done`：已完成
- `failed`：失败
- `canceled`：被系统取消或回滚时标记

任务历史会在完成后保留一段时间，之后自动清理。

PVE 配置下发说明：

- `apply` / `provision` 等任务由 worker 调用 `internal/pve` 完成。
- VM 配置变更通过 `go-proxmox` 的 `VirtualMachine.Config` 执行。
- PVE 的 `sshkeys` 参数保存的是 SSH 公钥列表，但提交时需要先对字段值做 rawurlencode，再由外层 HTTP form 编码提交。

## maintenance_tasks

全局维护任务队列表。

字段：

- `id`
- `kind`
- `payload_json`
- `status`
- `error`
- `created_at`
- `updated_at`
- `started_at`
- `finished_at`

当前已实现的维护任务：

- `inventory_scan`

说明：

- worker 通过维护任务定期扫描模板、库存 VM、节点负载和指标历史。
- 当本轮 PVE 库存扫描成功时，未被扫描到的 unmanaged VM 会被软删除；managed VM 不会因为库存扫描缺失而自动删除。
- 这类任务不属于某一台 VM，但仍然有自己的生命周期。
- worker 启动时会把上一个进程遗留的 `running` 任务恢复成 `pending`，避免部署或重启过程中留下永久“执行中”的任务。

主要索引：

- `idx_maintenance_tasks_kind_status_created`

## 迁移备注

当前迁移中还包含两个“回填型”迁移：

- `normalize_managed_vm_names`
- `backfill_target_node`

它们用于把历史数据修正到当前语义，保证老库也能平稳升级。
