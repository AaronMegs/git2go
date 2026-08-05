# `feat-sha256` 工作树复用与整合审计

> 审计日期：2026-08-04
> 只读参考：`/Users/aaronmegs/workspace/libgit2/git2go-worktrees/feat-sha256`
> SHA256 分支 HEAD：`38a8571`
> reftable 分支与 SHA256 分支共同基线：`1f7dbc4`（libgit2 v1.9.4）
> vendor：两分支均 pin `939362a3c`

## 1. 结论摘要

`feat-sha256` 已完成 promoted typed OID ABI 的主体适配、端到端 SHA256 测试和 `v36.0.0-pre.N` 发布流程，可作为后续整合的主来源；不应在 reftable 分支重复实现第二套 OID 模型。

但两个分支从 `1f7dbc4` 后独立演进：SHA256 分支有 16 个功能提交，reftable 分支有 27 个独有提交，且在 `repository.go`、`wrapper.c`、`Build_*.go`、CI、Makefile、README、配置测试等关键文件上均存在语义冲突。**不能直接整体 merge/cherry-pick 后接受单侧结果，必须按功能域手工整合。**

## 2. 可复用实现

### 2.1 typed `Oid` ABI（优先复用）

- `oid_typed.go`：
  - `Oid{kind uint8; id [32]byte}` 精确匹配 C `git_oid`；
  - `unsafe.Sizeof(Oid{}) == unsafe.Sizeof(C.git_oid{})` 和 `GIT_OID_MAX_SIZE` 运行时断言；
  - `newOidFromC`/`toC` 保留零拷贝；
  - SHA1/SHA256 的 `Type`/`Bytes`/String/parse/zero 语义。
- `oid.go`：
  - `ObjectIdType uint8`、`ObjectIdSHA1`/`ObjectIdSHA256`；
  - 类型感知的 `Cmp`/`Equal`/`NCmp`/`ShortenOids`。
- 测试：`oid_test.go`、`oid_sha256_test.go`。

整合决策：采用 SHA256 分支的 `ObjectIdType` 作为 v36 权威类型。reftable 分支临时新增的 `OidType` 不再保留为第二套枚举；如需迁移兼容，可在 v36-pre 内提供类型别名和 deprecated 常量别名，但不维持两套实现。

### 2.2 typed OID API 与 wrapper

可复用：

- `oid_type_api.go`：repository oid type、typed repository init、typed ODB/index/indexer/diff/hash API；
- `wrapper.c` 中 `git_oid_from_prefix` / `git_oid_from_raw`、`git_repository_init_ext`、`git_object_id_from_buffer/file`、ODB/index/indexer options shims；
- `repository_sha256_test.go`：ODB/commit/pack/index/diff/clone/push 端到端验证。

注意：reftable 分支也大幅修改了 `wrapper.c`（backend bridge v2），必须按函数块手工合并，不能整文件取 SHA256 侧。

### 2.3 v36-pre 发布线

已实现并应复用：

- module path：`github.com/libgit2/git2go/v36`；
- tag：`v36.0.0-pre.N`（Go module 路径不能使用 `/v36-pre`）；
- `.github/workflows/tag.yml` 只允许手动触发、正整数 N、精确 tag、不创建稳定 `v36.0.0`；
- `CHANGELOG.md` 与 `docs/v36-pre-release-progress.md`；
- 正式 `v36.0.0` 等待上游发布含 promoted typed OID/reftable 的正式 libgit2，并完成跨平台验证。

## 3. 必须手工解决的整合点

### 3.1 `RepositoryInitOptions`

reftable 分支已有完整 `RepositoryInitOptions` + `RefdbType`；SHA256 分支主要提供 `InitRepositoryWithOidType` 便利入口，并未包含该 reftable 结构。

最终 v36 应合并为：

```go
RepositoryInitOptions{
    // flags/mode/workdir/template/head/origin...
    OidType:   ObjectIdSHA1 | ObjectIdSHA256,
    RefdbType: RefdbFiles | RefdbReftable,
}
```

`InitRepositoryWithOidType` 可保留为便利函数，但应委托给统一的 `InitRepositoryExt`，避免两套初始化逻辑漂移。

### 3.2 `ObjectIdType` vs `OidType`

- SHA256 分支：`ObjectIdType uint8`，与 `git_oid.type` 布局一致；
- reftable 分支：`OidType int`，仅用于探测，不能作为 `Oid` 字段。

最终应统一为 SHA256 分支的 `ObjectIdType uint8`。`Repository.OidType()` 返回该类型。reftable 分支的 `oid_type.go` 在整合时删除或变为兼容别名，避免公开 API 重名/语义重复。

### 3.3 `wrapper.c`

必须同时保留：

- SHA256 typed OID/ODB/index/diff shims；
- reftable backend bridge v2 的 17 callback、per-iterator handle、transaction lock payload；
- smart transport / credential 等既有修复。

建议按函数区域三方合并，并在合并后做符号清单审计，不采用整文件 ours/theirs。

### 3.4 构建守卫与静态宏

SHA256 分支的 Build 文件已经改成 promoted typed OID capability guard，但仍定义 `LIBGIT2_STATIC`。最新 libgit2 main 的 Windows visibility 判定使用 `GIT_STATIC`。

最终应：

- static CFLAGS 至少定义 `GIT_STATIC`；迁移期可同时定义 `LIBGIT2_STATIC`；
- 保留一个集中式 capability/version guard，避免三个 Build 文件复制同一组 `#if`；
- v36-pre 要求 promoted typed OID capability，不再尝试与 v1.9.x 20-byte ABI 同二进制兼容。

### 3.5 CI 轨道语义

v35 reftable 分支可真实验证 v1.9.4 no-tag；v36-pre typed OID 分支不能链接 v1.9.x。最终 CI 应按**主版本线**表达，而不是声称 v36 同时兼容两种 ABI：

- v35/stable compatibility：留在 v35 维护线验证 libgit2 v1.9.x；
- v36-pre：pinned main，SHA1 + SHA256 + files + reftable；
- 可增加一个“旧 libgit2 必须被 capability guard 拒绝”的负向编译测试，但不是可运行兼容 job。

### 3.6 已重复修复的 config 竞争

两分支都已把共享 `./temp.gitconfig` 改为 `t.TempDir()`。整合时保留任一等价实现，不重复提交。

## 4. SHA256 实现审计发现

### 4.1 正确且应保留

- Oid type 字段使用私有 `uint8`，布局为 33/1/1；
- Go/C size guard；
- 零值 Oid 归一化为 SHA1；
- `Equal`/`Cmp`/`NCmp` 类型感知；
- `NewOid` 支持 40/64 hex；
- typed ODB/index/indexer/diff/hash；
- SHA256 local clone/push/pack/commit/index round-trip；
- `DEPRECATE_HARD=ON` 审计与 CI；
- `v36.0.0-pre.N` 发布门槛。

### 4.2 整合时需修正/复核

1. `Build_*.go` 尚未使用 `GIT_STATIC`，需按最新 main visibility 规则补齐。
2. Build capability guard 在三个文件中重复，建议与 reftable 分支的集中式 guard 思路合并。
3. `InitRepositoryWithOidType` 与 reftable 的 `InitRepositoryExt` 需要收口。
4. `NewOidFromBytes` 保留旧单返回值 API，并直接切片前 20 字节；调用者传入短 slice 时会 panic。v36-pre 应决定是否保持历史行为，至少在迁移文档中明确；更安全的 typed constructor 已返回 error。
5. SHA256 分支删除/缺少所有 reftable/refdb/reflog 文件只是分支分叉结果，不代表应在整合时删除。
6. CI/发布工作流与 reftable bridge/reftable matrix 必须合并，而不是覆盖。

## 5. 推荐整合顺序

1. 当前 reftable 分支完成并文档化阶段 A（bridge v2、SetRefdb、CI、测试矩阵）。
2. 以 v36-pre 集成分支为载体，先引入 SHA256 分支的 module/Oid/typed API 主体。
3. 恢复并手工合并 reftable 的 repository init、refdb/reflog/transaction、wrapper bridge v2。
4. 统一 `ObjectIdType`、`RepositoryInitOptions`、Build guard、`GIT_STATIC`。
5. 合并 CI 与 release workflow，明确 v35/v36 ABI 边界。
6. 跑 SHA1/SHA256 × files/reftable 四组合以及全量回归。
7. 仅发布 `v36.0.0-pre.N`；上游正式 release 之前不发布 `v36.0.0`。

## 6. 整合结果（2026-08-05）

- typed Oid/ODB/index/indexer/diff 与 reftable bridge v2 已手工合并；
- `ObjectIdType` 已成为统一类型，临时 `OidType` 已移除；
- `RepositoryInitOptions` 同时包含 `OidType` 与 `RefdbType`；
- `wrapper.c` 同时保留 typed OID shims 与 17-callback backend bridge v2；
- static CFLAGS 已增加 `GIT_STATIC`；module 已切 `/v36`；
- SHA1/SHA256 × files/reftable 四组合全部通过；
- latest-main static/dynamic 无跳过全量、race 定向和 DEPRECATE_HARD=ON 均通过；xdiff 系统头污染已修复；
- `v36.0.0-pre.N` workflow/CHANGELOG/迁移文档已整合。

剩余外部边界：上游尚未发布包含 promoted typed OID + reftable 的正式版本，因此暂不发布稳定 `v36.0.0`。
