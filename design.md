# Cloud Manage 设计总览

Cloud Manage 是一个面向 Proxmox VE 的虚拟机控制面，目标是把“创建、接管、变更、删除、权限、任务追踪、库存扫描”这些动作统一到同一套后端里。

它不是直接在前端里拼 PVE 命令，也不是把数据库只当缓存，而是把 SQLite 当作控制面的事实来源，再通过 worker 把任务异步同步到 PVE。

## 文档分工

- 本文件：总体架构、运行方式、数据流、实现原则。
- [docs/sql-tables.md](docs/sql-tables.md)：所有 SQLite 表、索引、迁移与字段语义。
- [docs/api.md](docs/api.md)：HTTP API、请求体、返回体和权限边界。

## 当前实现概览

当前仓库已经落成以下模块：

- `cmd/cloud-manage`
  - `serve`：启动 Web/API。
  - `worker`：启动后台同步 worker。
  - `migrate`：执行 SQLite 迁移。
- `internal/auth`
  - OIDC 登录、会话、用户 upsert。
- `internal/api`
  - VM、任务、安全组、SSH Key、模板、管理员接口。
- `internal/worker`
  - 任务队列消费、维护任务、PVE 同步、库存扫描、指标采集。
- `internal/pve`
  - PVE API 封装，VM 生命周期和配置下发优先走 `github.com/luthermonson/go-proxmox`。
- `frontend`
  - Vite 控制台前端。

## 设计目标

1. 用户登录后可以像公有云控制台一样管理自己的 VM。
2. 用户的可选配置必须来自后端配置文件，前端只展示后端允许的组合。
3. 所有真正影响 PVE 的操作都进入任务队列，由 worker 顺序执行。
4. 任务和库存分离：
   - VM 任务：用户对单台 VM 的业务动作。
   - 维护任务：全局 inventory 扫描、模板扫描、节点负载和指标采集。
5. SQLite 保存控制面状态，PVE 保存虚拟化运行时状态。
6. 扫描到的 unmanaged VM 也要纳入库存，避免 VMID、IP、节点资源冲突。

## 运行模式

程序支持三个子命令：

```bash
cloud-manage serve
cloud-manage worker
cloud-manage migrate
```

推荐部署方式：

- `serve` 常驻，提供前端和 API。
- `worker` 常驻，10 秒轮询一次任务队列，约 10 分钟入队一次全局维护任务。
- `migrate` 在部署或升级时执行。

## 数据流

1. 用户通过 OIDC 登录。
2. 后端 upsert `users` 表，记录 `username / email / name / groups`。
3. 用户在前端提交 VM 创建、接管、编辑、删除、开关机等动作。
4. API 校验配置文件、配额和权限，把变化写入 `vms` 和 `vm_tasks`。
5. worker 取出待执行任务，顺序调用 PVE API。
6. worker 将结果写回 `vms.real_status_json`、`vms.sync_state`、`vm_tasks`。
7. 维护任务定期扫描 PVE：
   - 模板池
   - 节点负载
   - 所有 VM 库存
   - VM 指标历史

## 状态模型

当前系统同时保留三层信息：

- `config_json`
  - 我们记录的配置快照。
  - 用于展示、校验和 worker 同步。
- `prefer_status_json`
  - 当前希望系统达到的状态/参数。
  - 由 API 在入队时更新。
- `real_status_json`
  - PVE 当前实际状态。
  - 由 worker 和扫描任务回写。

任务队列是执行层的核心，但 `prefer_status_json` 仍然保留，用来稳定表达“这台 VM 期望做什么”。

## 任务模型

### VM 任务

`vm_tasks` 记录单台 VM 的操作步骤，例如：

- `provision`
- `apply`
- `power`
- `reboot`
- `delete`

任务按 `seq` 串行执行。前一个任务没有完成，后续任务不会越过它。

### 维护任务

`maintenance_tasks` 记录全局维护动作，例如：

- `inventory_scan`

worker 每轮先处理维护任务，再处理 VM 任务。

## 资源调度

创建 VM 时，节点选择不是随机的，而是根据配置文件允许的节点范围和当前静态占用做均衡：

- 只在该 CPU 组允许的 node 列表里选。
- 优先考虑当前静态 CPU / Memory 占用更低的 node。
- 模板所在 node 会参与 tie-break。

目标是让 VM 分布更均衡，同时可预测。

## 配置来源

后端读取三个配置文件：

- `config/config.yaml`
  - cluster、CPU、storage、network、quota。
- `config/oidc.yaml`
  - OIDC 发行方、claim 映射、cookie 配置。
- `config/token.yaml`
  - 每个 cluster 对应的 PVE API token。

前端展示的 cluster / cpu / storage / bridge 选项都来自 `config/config.yaml` 的 `options` 输出，而不是写死在前端。

## 权限模型

- 普通用户：
  - 只能操作自己拥有或共享给自己的 VM。
  - 只能看到自己的 security group / SSH key。
- 管理员：
  - 可以查看全量 VM、全量 security group、全量 SSH key、维护任务。
  - 可以接管 unmanaged VM。
  - 可以修改 IP 和 owner 等管理字段。

VM 创建表单里的 SSH Key 选择只使用当前登录用户自己的 SSH Key。普通用户编辑 VM 时也只能选择自己的 key。管理员从 `Admin / All VMs` 进入编辑或接管时，表单会加载全量 SSH Key，但按目标 owner 过滤，只显示该 owner 的 key，并在选项上显示 `owner / name`，避免不同用户的 key 重名。后端同样按目标 owner 校验 `ssh_key_ids`，防止把别人的 key 错挂到 VM 上。

## PVE 同步细节

worker 对 VM 的实际改动通过 `internal/pve` 封装完成。VM 配置下发使用 `go-proxmox` 的 `VirtualMachine.Config`，而不是前端或 worker 手写 HTTP 请求。

PVE 的 cloud-init `sshkeys` 参数比较特殊：该字段值本身需要先做 rawurlencode，随后再由 HTTP form 编码层提交。代码里保留了专门的编码 helper，避免空格、`/`、`+`、`@` 等字符被 PVE 误解析。

## 前端

前端是一个 Vite 单页管理台，当前页面包括：

- VMs
- Security Groups
- SSH Keys
- Templates
- Admin

前端只负责展示和提交，不做业务规则裁决。真正的限制都在后端。

## 现在的约束

这套实现已经收敛到“任务队列驱动 + 库存扫描 + 配置快照”的模型，但仍保留一些 PVE 细节字段，原因是：

- 前端需要展示历史和现状。
- worker 需要和 PVE 对齐配置。
- 接管场景需要尽量从扫描里补齐缺失信息。

如果后面要继续演进，最值得优先收紧的是：

- 任务控制面的可视化
- 接管流程的分步引导
- 维护任务的可观测性
