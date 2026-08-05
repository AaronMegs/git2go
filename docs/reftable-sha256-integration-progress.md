# Reftable + SHA256 分阶段实施跟踪（v36-pre）

> 启动日期：2026-08-04
> reftable 工作分支：`feat-reftable`
> SHA256 参考工作树：`/Users/aaronmegs/workspace/libgit2/git2go-worktrees/feat-sha256`
> libgit2 vendor：main `939362a3c`

## 1. 版本与集成策略

- reftable 与 SHA256 先在各自分支独立实现和验证，随后整合。
- SHA256 适配不在本分支重复设计；涉及 `Oid`、typed oid API、`RepositoryInitOptions.OidType`、`GIT_STATIC`、CI 和发布流程时，以 `feat-sha256` 已验证实现为主要参考，集成时做冲突审计。
- 在 libgit2 尚未发布包含 reftable/SHA256 转正能力的稳定版本前，git2go 使用 **`v36.0.0-pre.N`** 预发布版本；不发布稳定 `v36.0.0`。
- 当前 `feat-reftable` 仍保持 `/v35` module path，直到 reftable 阶段 A 完成并与 `feat-sha256` 集成；集成线切换到 `/v36`，发布 tag 使用 `v36.0.0-pre.N`。
- 当前 vendor 已升级到含 SHA256 typed OID ABI 的 main，因此本分支的干净构建会被 `git_oid` ABI 守卫按预期阻止。阶段 A 可在旧的 SHA1-compatible 构建产物上做局部验证；最终验证必须在与 `feat-sha256` 集成后执行。

## 2. 推荐顺序与状态

### 阶段 A：先完善当前 reftable/refdb 适配

| # | 项目 | 状态 | 交付/验证 |
| --- | --- | --- | --- |
| A1 | 自定义 backend bridge v2 设计 | ✅ 完成 | 可选 capability interfaces；完整 callback 映射 |
| A2 | iterator 生命周期/并发/泄漏修复 | ✅ 完成 | 每 iterator 独立 handle；Free/Untrack；name buffer 回收 |
| A3 | 补 `init/compress/lock/unlock` | ✅ 完成（latest-main 干净构建终验待整合） | 四项均端到端验证；`init` 通过包内 C bridge 验证 head/mode/flags |
| A4 | 修复 `Repository.SetRefdb` | ✅ 完成 | 返回 `error`；OS thread；双 KeepAlive；nil 校验 |
| A5 | CI 真正 v1.9.4 + main 双轨 | ✅ 配置完成；本地 v1.9.4 重建未获授权 | stable job 真正 checkout/build v1.9.4；main/reftable；race；配置竞争已修复 |
| A6 | 扩展 reftable 测试矩阵 | ✅ 完成（latest-main 最终回归待集成） | reopen、reflog、symbolic refs、iterator/bridge v2/transaction |

### 阶段 B：v36-pre 解锁最新 main

| # | 项目 | 状态 | 来源 |
| --- | --- | --- | --- |
| B1 | `Oid` 方案 A / typed OID ABI | SHA256 分支已实现，待整合 | `feat-sha256`: `oid_typed.go`, `oid.go` |
| B2 | 更新 Go/C 布局断言 | SHA256 分支已实现，待整合 | `oid_typed.go` runtime layout checks |
| B3 | 新式 typed oid API | SHA256 分支已实现，待整合审计 | `oid_type_api.go` 与相关 wrapper |
| B4 | `RepositoryInitOptions.OidType` | 待整合时补齐 | reftable 与 SHA256 init options 合并点 |
| B5 | `GIT_STATIC` | SHA256 分支已实现，待整合 | `Build_*.go` |
| B6 | vendor 最新 main | 已完成 | `939362a3c`, commit `8b4a398` |
| B7 | v36-pre 版本/发布线 | SHA256 分支已实现，待整合 | `/v36`, `v36.0.0-pre.N` workflow/changelog |

### 阶段 C：SHA256 + reftable 组合验证

| 组合 | 状态 |
| --- | --- |
| SHA1 + files | 待最终集成回归 |
| SHA1 + reftable | reftable 分支已有基础覆盖，待集成回归 |
| SHA256 + files | SHA256 分支已有覆盖，待集成回归 |
| SHA256 + reftable | 待新增完整组合测试 |

## 3. 阶段 A1-A4 实施记录

### 3.1 bridge v2 API

保留原有 13 个 mandatory callback 的 `RefdbBackendInterface`，将上游 optional callback 建模为能力接口，避免破坏已有实现：

- `RefdbBackendInitializer.Init(initialHead *string, mode, flags)`；
- `RefdbBackendCompressor.Compress()`；
- `RefdbBackendLocker.Lock/Unlock()`；
- `RefdbBackendUnlockStatus` 精确保留 cancel(0) / update(1) / delete(2)；
- `*string` 保留 C `NULL` 与显式空字符串的差异。

`init` 字段只存在于 libgit2 main；`reftable_on.go` 通过 package-wide `GIT2GO_HAS_REFDB_BACKEND_INIT` CFLAG 打开 C 结构字段接线。无 tag/v1.9.x 路径不引用该字段，并在 Go 侧拒绝安装 initializer capability。`compress/lock/unlock` 在 v1.9.4 已存在，两个轨道都可用。

### 3.2 iterator 与 lock 生命周期

- backend state 不再保存共享 iterator；
- 每次 `Iterator()` 都创建独立 `pointerHandles` handle；
- C iterator free 调用 Go `Free()` + `Untrack()`；
- C managed iterator 保存并回收 `next_name` 的上一次字符串；
- C wrapper 分配失败时回收已经创建的 Go iterator handle；
- transaction lock payload 使用独立 handle，`Unlock` 后无论成功/失败都 `Untrack()`。

### 3.3 Transaction 与 SetRefdb

- 新增完整 `Transaction` 公开绑定：new / lock / set direct target / set symbolic target / set reflog / remove / commit / free；
- transaction 测试真实触发 custom backend 的 lock/unlock，验证 payload、update/cancel status、target 和 NULL message；
- `Repository.SetRefdb` 改为返回错误，固定 OS thread，并 KeepAlive repository/refdb。

### 3.4 已执行验证

```text
go build -tags "static libgit2_reftable" ./...                         PASS
go build -tags static ./...                                            PASS
go test -tags "static libgit2_reftable" -run "TestRefdbBackend|TestSetRefdb"  PASS
go test -race -tags "static libgit2_reftable" \
  -run "TestRefdbBackendConcurrentIterators|TestRefdbBackendTransaction"      PASS
```

上述验证使用现有 SHA1-compatible `static-build` 产物，证明 bridge v2 的 Go/C 行为；不是 latest-main 干净构建证明。latest-main 的最终验证等待与 `feat-sha256` typed OID 实现整合。

额外验证：

```text
optional callback 指针（init/compress/lock+unlock）按 capability 安装     PASS
init callback head/mode/flags 端到端调用                              PASS
无 tag 构建拒绝 main-only initializer capability                        PASS
并行 config 测试重复 20 次（每测试独立 t.TempDir）                      PASS
reftable reopen / reflog / symbolic ref 定向测试                         PASS
```

真实 v1.9.4 临时重建命令未获执行授权，因此本轮不宣称本地 stable 真库验证；CI 已改为真实 checkout/build v1.9.4，并在后续 CI 运行中闭环。

## 4. SHA256 工作树复用原则

- 只读参考，不直接修改 `feat-sha256` 工作树。
- 优先复用已提交、已验证的符号与测试，不在 reftable 分支创建第二套 OID 模型。
- 预计高冲突文件：`repository.go`、`wrapper.c`、`Build_*.go`、`.github/workflows/ci.yml`、`Makefile`、`README.md`、`go.mod`。
- 可复用与冲突审计详见 [sha256-worktree-integration-audit.md](./sha256-worktree-integration-audit.md)。
- 预计可直接复用文件：`oid_typed.go`、`oid_type_api.go`、SHA256 专项测试与 v36-pre 发布文档/工作流；整合前仍需代码审计。
- v36-pre 不再与 v1.9.x 的 20-byte `git_oid` ABI 同二进制兼容；真实 v1.9.4 job 属于 v35 维护线。整合后的 v36 CI 应改为验证旧 ABI 被 capability guard 拒绝，而不是宣称可运行兼容。

## 5. 验证分层

1. 阶段 A 局部：Go/C bridge 单测、race/多 iterator 生命周期、错误传播。
2. 阶段 A stable：真实 libgit2 v1.9.4 no-reftable 构建。
3. 阶段 A main：reftable tag 构建；在 OID 集成前只能使用已存在的 SHA1-compatible 构建产物作有限验证。
4. 阶段 B 集成：最新 main 全量编译、布局断言、typed OID 全量回归。
5. 阶段 C：四组合矩阵 + reopen/persistence/reflog/branch/symbolic ref。

## 6. 记录规范

- 每完成一个阶段项，更新本文件状态、验证命令与结果。
- 代码与文档分开提交；每个提交保持单一主题。
- 不在 reftable 分支重复提交 SHA256 分支已有的大规模实现；整合时保留来源 commit 说明。
- 任何无法在当前 latest-main vendor 下验证的改动必须明确标注“provisional”，不得宣称全量通过。
