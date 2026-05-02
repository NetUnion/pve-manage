# Cloud Manage 后端设计计划书

## 目标

实现一个接近公有云体验的 PVE 虚拟机管理系统。用户通过 SSO 登录后，可以在 Web 前端创建、查看、修改、删除自己的 VM，也可以把 VM 共享给其他用户共同管理。

后端负责认证、授权、配置校验、状态记录和 API 暴露；真正对 PVE 执行创建、变更、删除等操作的逻辑由 worker 异步完成。

核心原则：

- API 层记录“用户希望系统达到的状态”。
- Worker 层把期望状态同步到 PVE。
- SQLite 是控制面的事实来源。
- PVE 返回的真实状态写回 SQLite，供前端展示。
- 所有用户可选配置来自后端配置文件，前端只展示后端允许的选项。

## 总体架构

组件：

- Frontend：面向用户的 VM 管理界面。
- Backend API：提供登录、用户信息、配置列表、VM CRUD 和管理接口。
- SQLite：保存用户、VM、配置选择、状态和审计信息。
- Worker：周期性拉取 VM 表中的期望状态，调用 PVE API 执行同步，然后回写真实状态。
- PVE：实际运行 VM 的虚拟化平台。
- OIDC Provider：SSO 登录来源，提供 username、email、name、groups。

数据流：

1. 用户通过 OIDC 登录。
2. Backend 校验 token，提取 username、email、name、groups。
3. Backend upsert user table。
4. 用户调用 API 创建或修改 VM。
5. Backend 根据配置 YAML 校验参数，并写入 vm table 的 `prefer_status`。
6. Worker 拉取待同步 VM。
7. Worker 对 PVE 执行 create/update/delete/start/stop 等操作。
8. Worker 将结果写回 `real_status`。
9. Frontend 通过 API 读取 VM 列表和真实状态。

## 后端技术选择

建议初版使用 Go：

- API 框架：Go 标准库 `net/http` + `chi`，或直接使用 `gin`。偏向 `chi`，更轻、更接近标准库。
- 数据库：SQLite。
- SQL 访问：`database/sql` + `modernc.org/sqlite` 或 `github.com/mattn/go-sqlite3`。如果希望纯 Go 编译，优先 `modernc.org/sqlite`。
- 迁移：`golang-migrate/migrate`。
- OIDC：`coreos/go-oidc/v3` + `golang.org/x/oauth2`。
- 配置文件：`gopkg.in/yaml.v3`。
- 校验：普通 Go struct validation，必要时使用 `go-playground/validator`。
- Worker：Go 独立进程或同一个二进制的子命令，例如 `cloud-manage serve` 和 `cloud-manage worker`。

SQLite 初期足够，因为系统控制面数据量小，主要瓶颈在 PVE 操作而不是数据库。需要注意：

- 开启 WAL。
- 写操作尽量短事务。
- Worker 初版按单实例部署设计。
- PVE 接入通过 PVE API token 完成，不通过 SSH 或直接编辑 PVE 配置文件。

建议 Go 项目结构：

```text
cmd/cloud-manage/main.go
internal/api/
internal/auth/
internal/config/
internal/db/
internal/model/
internal/service/
internal/worker/
internal/pve/
migrations/
config/
```

命令：

```text
cloud-manage serve
cloud-manage worker
cloud-manage migrate
```

## 用户模型

`users` table：

```text
id                integer primary key
username          text unique not null
email             text
name              text
groups_json       text not null
is_active         integer not null default 1
is_admin          integer not null default 0
created_at        datetime not null
updated_at        datetime not null
last_login_at     datetime
```

说明：

- `username` 来自 OIDC claim，作为系统内主身份。
- `email`、`name` 用于显示和联系。
- `groups_json` 保存 OIDC groups 原始列表。
- `is_admin` 可由配置映射，例如某些 OIDC group 自动成为管理员。

登录行为：

- 每次登录都 upsert 用户信息。
- groups 以 OIDC 最新传入值为准。
- OIDC 配置来自 `config/oidc.yaml`。
- username 使用 NetUnion SSO 传入的 username claim。
- username 必须匹配 `^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`，也就是 3-32 位、小写字母/数字/连字符、首尾为字母或数字。
- 如果 username 不符合规则，应拒绝登录。

## VM 模型

`vms` table：

```text
id                    integer primary key
owner_username        text not null
cluster_key           text not null
vmid                  integer not null
vmname                text not null
ip                    text not null
password              text not null
sshkeys_json          text not null
shared_usernames_json text not null
security_group_name   text not null
uestc_restricted      integer not null default 0
config_key            text not null
prefer_status_json    text not null
real_status_json      text not null
sync_state            text not null
sync_error            text
version               integer not null default 1
created_at            datetime not null
updated_at            datetime not null
deleted_at            datetime
delete_requested_at   datetime
delete_execute_after  datetime
unique(cluster_key, vmid)
```

字段解释：

- `owner_username`：VM 所有者。
- `cluster_key`：VM 所属 cluster，例如 `cnss_sg2`。
- `vmid`：PVE 中的 VMID，在同一 cluster 内唯一，后端创建 VM 记录时分配。
- `vmname`：用户可见名称，同时用于生成 PVE VM name。
- `ip`：后端分配给 VM 的 IPv4/CIDR。普通用户不可指定或修改，管理员可修改。
- `password`：后端随机生成的 root 密码，传递给 worker 设置 cloud-init。
- `sshkeys_json`：SSH 公钥列表。
- `shared_usernames_json`：允许共同管理此 VM 的 username 列表。
- `security_group_name`：绑定到该 VM 的用户自定义 security group 名称。
- `uestc_restricted`：是否为该 VM 应用 cluster 配置中的 `network.uestc` firewall group；有效用户策略为 `force` 时必须为 `1`。
- `config_key`：用户选择的配置套餐 key。
- `prefer_status_json`：期望状态，例如规格、镜像、开关机、删除意图。
- `real_status_json`：PVE 实际状态，例如 running/stopped、实际 IP、node、磁盘、错误。
- `sync_state`：同步状态。
- `version`：用于并发控制，用户修改 VM 时递增。
- `delete_requested_at`：用户请求删除的时间。
- `delete_execute_after`：实际允许 worker 删除 PVE VM 的时间，初版为请求删除后 24 小时。

建议 `sync_state`：

```text
pending
syncing
synced
failed
deleting
deleted
```

删除语义：

- 用户删除 VM 后，后端不立即删除 PVE VM。
- 后端将 VM 标记为等待删除状态，并设置 `delete_execute_after = delete_requested_at + 24h`。
- 等待删除期间，worker 应停止 VM，但不清除 VM。
- 24 小时窗口过后，worker 才实际删除 PVE VM。
- 删除窗口内是否允许 owner 取消删除，后续可作为 API 能力补充。

## 配置 YAML

后端需要读取一个 YAML 文件，作为：

- 前端可展示配置列表。
- 后端创建/修改 VM 时的校验依据。
- Worker 同步到 PVE 时的参数来源。

当前配置文件为 `config/config.yaml`，核心结构：

```yaml
cluster:
  cnss_sg2:
    name: CNSS Server Group 2
    limit: 1000
    start_vmid: 1000
    cpu:
      intel_xeon_e5_v4:
        name: Intel Xeon CPU E5-2680 v4
        limit: 16
        memory_limit: 32
        node:
          - node1
          - node2
    storage:
      zfs-iscsi:
        name: 高性能 HDD
        limit: 500
    network:
      uestc: deny-uestc
      bridge:
        custom:
          ipv4:
            type: static
            start_ip: 10.12.65.0
            cidr: 18
            gateway: 10.10.64.1
          ipv6:
            type: slaac
          ipfilter:
            - dc/cnss-sg2-ipv6-cernet
            - dc/cnss-sg2-ipv6-custom

user:
  admin_group:
    - root
  limit:
    cnss-user:
      number: 10
      cpu: 16
      memory: 32
      storage: 500
      security_group: 5
      uestc: choose
```

字段语义：

- `cluster.*.limit`：该 cluster VMID 分配范围内最多 VM 数量。
- `cluster.*.start_vmid`：该 cluster VMID 起点。
- `cluster.*.cpu.*.limit`：CPU 核心数，单位 core。
- `cluster.*.cpu.*.memory_limit`：内存上限，单位 GB。
- `cluster.*.cpu.*.node`：可承载该 CPU 池 VM 的 PVE 节点列表。
- `cluster.*.storage.*.limit`：存储上限，单位 GB。
- `cluster.*.network.bridge.*.ipv4.start_ip`：该网络的 IPv4 起点。
- `cluster.*.network.bridge.*.ipv4.cidr`：IPv4 CIDR 前缀长度。
- `cluster.*.network.bridge.*.ipv4.gateway`：cloud-init IPv4 gateway。
- `cluster.*.network.bridge.*.ipv6.type`：IPv6 模式；当前为 SLAAC，后端不做 IPv6 IPAM。
- `user.limit.*.number`：每个用户最多 VM 数量。
- `user.limit.*.cpu`：每个用户所有 VM 合计最多 CPU 核心数。
- `user.limit.*.memory`：每个用户所有 VM 合计最多内存 GB。
- `user.limit.*.storage`：每个用户所有 VM 合计最多存储 GB。
- `user.limit.*.security_group`：每个用户最多可创建的自定义 security group 数量。
- `user.limit.*.uestc`：内网访问限制策略，`choose` 表示用户可选择是否应用，`force` 表示强制应用。

配额计算：

- 用户配额按 OIDC group 匹配。
- 用户同时属于多个 group 时，对每个字段取最大值。
- 例如 group-a 是 2 台 VM / 4 核，group-b 是 4 台 VM / 2 核，则用户有效配额是 4 台 VM / 4 核。
- `security_group` 数量也按最大值合并。
- `uestc` 按宽松规则合并：只要任一匹配组是 `choose`，用户就可以选择是否应用限制；只有全部有效匹配组都是 `force` 时才强制应用。
- 实际可用资源还要受所选 cluster 配置限制。
- 校验时取“用户剩余额度”和“cluster option 限制”中的较小值。

网络说明：

- `network.uestc` 是实际 PVE firewall group 名称，例如 `deny-uestc`。
- 如果用户组的 `uestc` 为 `force`，VM 必须记录并应用该限制。
- 如果用户组的 `uestc` 为 `choose`，前端允许用户选择是否应用该限制，后端把选择记录到 `vms.uestc_restricted`。
- `network.bridge.*.ipfilter` 是安全限制。Worker 创建或更新 VM 时，必须把这里列出的所有条目加入 VM firewall ipset `ipfilter-net0`，防止用户在 VM 内伪造源 IP。

IPv4 分配：

- 后端必须控制并分配 IPv4，IPv6 暂不控制。
- 创建 VM 时先确定 cluster 和 VMID。
- VMID 只要求在 cluster 内唯一，不要求跨 cluster 全局唯一。
- VMID 分配使用该 cluster 范围内最小可用值。
- 删除 VM 后释放 VMID，后续创建可以复用，因此 IPv4 也随 VMID 复用。
- 如果计算出的 VMID 或 IPv4 与已有记录冲突，后端必须直接报错，不做隐式修复或自动跳号。
- IPv4 计算规则为 `start_ip + (vmid - start_vmid)`。
- 存入 `vms.ip` 时带 CIDR，例如 VMID `1011`、`start_vmid=1000`、`start_ip=10.10.80.0`、`cidr=18` 时，结果是 `10.10.80.11/18`。
- 普通用户不能指定或修改 VM IPv4。
- `admin_group` 用户可以通过管理员 API 修改 VM IPv4；修改后仍必须校验 CIDR 格式、地址族和冲突。
- Worker 根据 `vms.ip` 和配置中的 `gateway` 同步 cloud-init。

Security group：

- 用户可以创建自己的 security group。
- 不自动创建 `default` security group。
- 用户必须先创建 security group，才能在 VM 创建或修改时绑定它。
- `security_group_name` 规则：小写字母、数字、连字符，建议长度 1-32，且以小写字母或数字开头和结尾。
- VM 创建或修改时必须绑定一个用户自己的 security group。
- VM table 需要保存绑定的 `security_group_name`。
- Worker 渲染 PVE firewall 时使用 `GROUP user_<username>_<security_group_name>`。
- 后端提供公有云风格的 security group 规则模型，worker 再把它翻译成 PVE firewall 配置。

`security_groups` table：

```text
id                  integer primary key
owner_username      text not null
name                text not null
rules_json          text not null
created_at          datetime not null
updated_at          datetime not null
unique(owner_username, name)
```

`rules_json` 建议结构：

```json
[
  {
    "direction": "in",
    "action": "allow",
    "ethertype": "ipv4",
    "protocol": "tcp",
    "cidr": "203.0.113.0/24",
    "port_start": 22,
    "port_end": 22
  },
  {
    "direction": "out",
    "action": "allow",
    "ethertype": "ipv6",
    "protocol": "icmp",
    "cidr": "::/0",
    "port_start": null,
    "port_end": null
  }
]
```

规则约束：

- `direction`：`in` 或 `out`。
- `action`：`allow` 或 `deny`。
- `ethertype`：`ipv4` 或 `ipv6`。
- `protocol`：`tcp`、`udp`、`icmp`，后续可扩展。
- `cidr`：单个 IP 或 CIDR，必须匹配 `ethertype`。
- `port_start` 和 `port_end`：TCP/UDP 可填端口或端口范围，ICMP 为空。

Template：

- Worker 扫描每个 cluster 的 `template-pool`。
- Worker 将可用 template 上报给后端。
- 后端保存到 `templates` table，供前端展示和创建 VM 时校验。

`templates` table：

```text
id              integer primary key
cluster_key     text not null
template_vmid   integer not null
name            text not null
description     text
os_type         text
real_status_json text not null
last_seen_at    datetime not null
created_at      datetime not null
updated_at      datetime not null
unique(cluster_key, template_vmid)
```

VM 创建和硬件调整：

- Worker 使用 PVE template 做 full clone。
- Clone 目标 storage 使用用户选择的 storage。
- PVE 在 full clone 时负责生成唯一 MAC 等硬件唯一标识。
- Clone 完成后，worker 再调整 VM hardware：
  - CPU cores。
  - memory。
  - disk size。
- PVE cloud-init 设置：
  - 默认用户为 `root`。
  - IPv4 使用后端分配的 `vms.ip`。
  - gateway 使用配置中的 `cluster.*.network.bridge.*.ipv4.gateway`。
  - IPv6 使用 SLAAC，例如 `ip6=auto`。
  - password 使用 `vms.password`。
  - SSH keys 使用 `vms.sshkeys_json`。
- PVE cloud-init 本身不负责磁盘配置；磁盘由 worker 通过 VM hardware resize 处理。
- VM 最小磁盘大小为 20GB，后端必须拒绝低于 20GB 的请求。
- 磁盘 resize 只能扩容，不能缩容。修改 VM 配置时，如果目标 disk 小于当前 real disk，后端应拒绝，或保持 pending failed 并提示不可缩容。

仍待补充：

- template 展示字段和 OS 分类规则。

## API 设计

用户相关：

```text
GET  /api/me
```

返回当前登录用户信息，包括 username、email、name、groups、权限标记。

配置相关：

```text
GET  /api/config/options
```

返回镜像、套餐、网络等可选项。前端只使用这个接口渲染可选配置。

VM 用户接口：

```text
GET    /api/vms
POST   /api/vms
GET    /api/vms/{id}
PATCH  /api/vms/{id}
DELETE /api/vms/{id}
```

行为：

- `GET /api/vms`：列出 owner 是自己或 shared_usernames 包含自己的 VM。
- `POST /api/vms`：创建 VM 记录；后端分配 VMID、IPv4，随机生成 root password，写入 `prefer_status_json`，状态置为 `pending`。
- `PATCH /api/vms/{id}`：修改 VM 名称、SSH keys、共享用户、security group、uestc 选择、期望电源状态或配置。普通用户不能修改 VMID、IPv4、password。
- `DELETE /api/vms/{id}`：owner 发起删除后进入 24 小时等待删除窗口；worker 先停止 VM，窗口结束后才删除 PVE VM。

管理员接口后续可以加：

```text
GET /api/admin/vms
GET /api/admin/users
PATCH /api/admin/vms/{id}
```

管理员 VM 修改能力：

- `admin_group` 用户可以修改 VM IPv4。
- 管理员修改 IPv4 后，后端必须校验格式、地址族、CIDR 和冲突。
- 管理员一般也不应直接修改 password；如需要，建议提供单独的 reset password 操作，由后端重新随机生成。

## Worker

Worker 部署和 PVE 接入：

- Worker 初版为单实例。
- Worker 直接读取 SQLite 中的待同步 VM，并直接回写同步结果。
- Worker 通过 PVE API token 操作 PVE。
- PVE token 由部署方创建并配置给 worker。
- Worker 不通过 SSH 操作 PVE，也不直接编辑 `/etc/pve` 文件。

## 权限模型

普通用户可以管理：

- `owner_username == current_user.username`
- 或 `current_user.username` 在 `shared_usernames_json` 中

管理员可以查看和管理全部 VM。

共享管理权建议初版包含：

- 查看 VM。
- 执行开关机等操作。

共享用户限制：

- 共享用户只能执行开机、关机、重启等电源操作。
- 共享用户不能删除 VM。
- 共享用户不能修改 SSH keys。
- 共享用户不能修改 security group。
- 共享用户不能继续把 VM 共享给其他用户。
- 只有 owner 可以修改共享用户列表和发起删除。

## VM 状态设计

`prefer_status_json` 示例：

```json
{
  "intent": "present",
  "power": "running",
  "image": "debian-12",
  "plan": "small",
  "network": "default",
  "sshkeys": ["ssh-ed25519 ..."],
  "generation": 3
}
```

删除时：

```json
{
  "intent": "delete_pending",
  "power": "stopped",
  "delete_execute_after": "2026-05-03T15:30:00+08:00",
  "generation": 4
}
```

超过 24 小时删除窗口后，后端或 worker 可将 intent 推进为：

```json
{
  "intent": "absent",
  "generation": 5
}
```

`real_status_json` 示例：

```json
{
  "intent": "present",
  "power": "running",
  "vmid": 1002,
  "node": "node1",
  "ip": "10.0.0.12",
  "last_synced_at": "2026-05-02T15:30:00+08:00"
}
```

## 同步策略

Worker 循环：

1. 从 SQLite 查询 `sync_state in ('pending', 'failed', 'deleting')` 的 VM。
2. 对每个 VM 比较 `prefer_status` 和 `real_status`。
3. 需要创建则调用 PVE clone/template API。
4. 需要修改则调用 PVE config API。
5. 需要删除则调用 PVE delete API。
6. 需要开关机则调用 PVE status API。
7. 直接回写 SQLite 中的 `real_status_json`、`sync_state` 和 `sync_error`。

Worker 必须是幂等的：

- 重复执行同一个 `prefer_status` 不应产生重复 VM。
- PVE 已经存在目标 VM 时，应校验并收敛状态。
- 删除已不存在的 VM 应视为成功。

## 第一阶段里程碑

1. 建立 Go 项目骨架。
2. 建立 SQLite schema 和 migrate migration。
3. 实现 OIDC 登录和 `/api/me`。
4. 实现 YAML 配置加载和 `/api/config/options`。
5. 实现 VM CRUD，只写 SQLite，不碰 PVE。
6. 实现单实例 worker：创建 VM、查询状态、回写状态。
7. 增加权限校验和共享用户管理。
8. 增加基础审计日志。

## 待确认问题

- PVE API token 的配置文件格式和权限范围。
