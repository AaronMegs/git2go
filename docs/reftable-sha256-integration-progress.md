# Reftable + SHA256 分阶段实施跟踪（v36-pre）

> 启动日期：2026-08-04
> reftable 工作分支：`feat-reftable`
> SHA256 参考工作树：`/Users/aaronmegs/workspace/libgit2/git2go-worktrees/feat-sha256`
> libgit2 vendor：main `939362a3c`

## 1. 版本与集成策略

- reftable 与 SHA256 先在各自分支独立实现和验证，随后整合。
- SHA256 适配不在本分支重复设计；涉及 `Oid`、typed oid API、`RepositoryInitOptions.OidType`、`GIT_STATIC`、CI 和发布流程时，以 `feat-sha256` 已验证实现为主要参考，集成时做冲突审计。
- 在 libgit2 尚未发布包含 reftable/SHA256 转正能力的稳定版本前，git2go 使用 **`v36.0.0-pre.N`** 预发布版本；不发布稳定 `v36.0.0`。
- 集成线已切换到 `/v36`，发布 tag 使用 `v36.0.0-pre.N`。
- vendor 已升级到含 SHA256 typed OID ABI 的 main，typed `Oid` 与 reftable 已完成整合，static/dynamic 全量验证通过。

## 2. 推荐顺序与状态

### 阶段 A：先完善当前 reftable/refdb 适配

| # | 项目 | 状态 | 交付/验证 |
| --- | --- | --- | --- |
| A1 | 自定义 backend bridge v2 设计 | ✅ 完成 | 可选 capability interfaces；完整 callback 映射 |
| A2 | iterator 生命周期/并发/泄漏修复 | ✅ 完成 | 每 iterator 独立 handle；Free/Untrack；name buffer 回收 |
| A3 | 补 `init/compress/lock/unlock` | ✅ 完成 | 四项均端到端验证；latest-main clean build 通过 |
| A4 | 修复 `Repository.SetRefdb` | ✅ 完成 | 返回 `error`；OS thread；双 KeepAlive；nil 校验 |
| A5 | CI v35/v36 ABI 边界 + main/reftable | ✅ 完成 | v1.9.4 负向 guard 测试；v36 main/reftable/race；system dynamic main；配置竞争已修复 |
| A6 | 扩展 reftable 测试矩阵 | ✅ 完成并终验 | reopen、reflog、symbolic refs、iterator/bridge v2/transaction；latest-main 全量通过 |

### 阶段 B：v36-pre 解锁最新 main

| # | 项目 | 状态 | 来源 |
| --- | --- | --- | --- |
| B1 | `Oid` 方案 A / typed OID ABI | ✅ 完成 | typed Oid/ODB/index/indexer/diff 与专项测试已整合 |
| B2 | 更新 Go/C 布局断言 | ✅ 完成 | v36 guard = 32，C size/type/id offsets + Go size；latest-main 构建通过 |
| B3 | 新式 typed oid API | ✅ 完成 | typed OID/ODB/index/indexer/diff shims 已与 bridge v2 手工合并并验证 |
| B4 | `RepositoryInitOptions.OidType` | ✅ 完成 | 统一 Options 同时包含 `OidType` + `RefdbType`；便利函数委托统一入口 |
| B5 | `GIT_STATIC` | ✅ 完成 | static 同时定义 `GIT_STATIC`/旧宏；v36 promoted ABI 集中守卫 |
| B6 | vendor 最新 main | ✅ 完成 | `939362a3c`, commit `8b4a398` |
| B7 | v36-pre 版本/发布线 | ✅ 完成 | module `/v36`、`v36.0.0-pre.N` tag workflow、CHANGELOG/迁移文档 |
| B8 | latest-main 全量回归 | ✅ 完成 | in-tree header 修复后 static 201/0/0；dynamic PASS；race 定向 PASS；DEPRECATE_HARD=ON PASS |

### 阶段 C：SHA256 + reftable 组合验证

| 组合 | 状态 |
| --- | --- |
| SHA1 + files | ✅ init/commit/branch/reopen/config |
| SHA1 + reftable | ✅ init/commit/branch/reopen/config |
| SHA256 + files | ✅ init/commit/64-hex/branch/reopen/objectFormat |
| SHA256 + reftable | ✅ init/commit/64-hex/branch/reopen/objectFormat/refStorage |

## 3. 阶段 A1-A4 实施记录

### 3.1 bridge v2 API

保留原有 13 个 mandatory callback 的 `RefdbBackendInterface`，将上游 optional callback 建模为能力接口，避免破坏已有实现：

- `RefdbBackendInitializer.Init(initialHead *string, mode, flags)`；
- `RefdbBackendCompressor.Compress()`；
- `RefdbBackendLocker.Lock/Unlock()`；
- `RefdbBackendUnlockStatus` 精确保留 cancel(0) / update(1) / delete(2)；
- `*string` 保留 C `NULL` 与显式空字符串的差异。

`init` 字段只存在于 libgit2 main。v36-pre 已整体要求 promoted latest-main ABI，因此由 `refdb_backend.go` 在所有 v36 构建中通过 package-wide `GIT2GO_HAS_REFDB_BACKEND_INIT` 接线；files-only（无 reftable tag）也支持 custom backend init。v1.9.x 的 20-byte OID ABI 由 v36 guard 明确拒绝，继续由 v35 维护线支持。

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

上述 bridge v2 局部验证随后已由 latest-main 干净 static/dynamic 全量、race 与四组合验证取代；最终结论见 §5。

额外验证：

```text
optional callback 指针（init/compress/lock+unlock）按 capability 安装     PASS
init callback head/mode/flags 端到端调用                              PASS
无 tag/files-only latest-main 构建仍支持 custom backend initializer         PASS
并行 config 测试重复 20 次（每测试独立 t.TempDir）                      PASS
reftable reopen / reflog / symbolic ref 定向测试                         PASS
```

v36-pre 不兼容 v1.9.4 的 legacy 20-byte OID ABI；CI 使用真实 v1.9.4 作为负向 guard 测试，v1.9.x 运行兼容由 v35 维护线负责。

## 4. SHA256 工作树复用原则

- 只读参考，不直接修改 `feat-sha256` 工作树。
- 优先复用已提交、已验证的符号与测试，不在 reftable 分支创建第二套 OID 模型。
- 预计高冲突文件：`repository.go`、`wrapper.c`、`Build_*.go`、`.github/workflows/ci.yml`、`Makefile`、`README.md`、`go.mod`。
- 可复用与冲突审计详见 [sha256-worktree-integration-audit.md](./sha256-worktree-integration-audit.md)。
- 预计可直接复用文件：`oid_typed.go`、`oid_type_api.go`、SHA256 专项测试与 v36-pre 发布文档/工作流；整合前仍需代码审计。
- v36-pre 不再与 v1.9.x 的 20-byte `git_oid` ABI 同二进制兼容；真实 v1.9.4 job 属于 v35 维护线。整合后的 v36 CI 应改为验证旧 ABI 被 capability guard 拒绝，而不是宣称可运行兼容。

## 5. 整合验证结果（2026-08-05）

```text
latest-main static build (default)                         PASS
latest-main dynamic build                                 PASS
go build static + reftable                                PASS
四组合 TestRepositoryFormatMatrix                         PASS (4/4)
typed OID/SHA256 + reftable + bridge 定向测试             PASS
race: matrix + bridge iterators/transactions              PASS
dynamic 全量（无跳过）                                   PASS
static 全量（无跳过，DEPRECATE_HARD=ON）                  PASS (201/0/0)
DEPRECATE_HARD=ON rebuild + typed/reftable/bridge tests    PASS
files-only latest-main/no-reftable-tag                     PASS
xdiff in-tree header 修复后原 10 个崩溃用例              PASS
审计修复（Oid零值/所有权/panic/init校验/merge泄漏）       PASS
二次审计（transaction复用/NUL/refdb owner/SHA256 shorten） PASS
CI action/发布 token/stringer 固定与隔离                  PASS
最终 static full + dynamic full + race（无跳过）           PASS
```

xdiff SIGBUS 已通过 vendored include 优先级修复闭环：原 10 个崩溃用例全部通过。导入的默认分支无关 rebase 测试同样通过，因此当前 static/dynamic 全量基线均无需跳过。

## 6. 验证分层

1. 阶段 A 局部：Go/C bridge 单测、race/多 iterator 生命周期、错误传播。
2. 阶段 A stable：真实 libgit2 v1.9.4 no-reftable 构建。
3. 阶段 A main：reftable tag 构建；在 OID 集成前只能使用已存在的 SHA1-compatible 构建产物作有限验证。
4. 阶段 B 集成：最新 main 全量编译、布局断言、typed OID 全量回归。
5. 阶段 C：四组合矩阵 + reopen/persistence/reflog/branch/symbolic ref。

## 7. 记录规范

- 每完成一个阶段项，更新本文件状态、验证命令与结果。
- 代码与文档分开提交；每个提交保持单一主题。
- 不在 reftable 分支重复提交 SHA256 分支已有的大规模实现；整合时保留来源 commit 说明。
- 任何无法在当前 latest-main vendor 下验证的改动必须明确标注“provisional”，不得宣称全量通过。
