# Reftable / SHA256 长期项调研与实施结论

> 初始调研：2026-08-03
> 最新复核与整合：2026-08-05
> libgit2 vendor：main `939362a3c`
> git2go 线：`github.com/libgit2/git2go/v36`，上游正式发版前仅发布 `v36.0.0-pre.N`

## 1. 执行结论

| 项 | 上游/项目状态 | 结论 |
| --- | --- | --- |
| reftable feature flag | 上游仍无 `GIT_FEATURE_REFTABLE` | 保留 build tag + init 探测 + repository format 三层策略 |
| per-worktree 判定 | 上游仍只有内部 `git_reference__is_per_worktree_ref` | 不绑定内部符号，等待公开 API |
| promoted SHA256 OID | 上游 main 已正式启用 typed `git_oid` | v36-pre 已完成 typed OID 整合 |
| SHA256 + reftable | 上游实现并提供测试资源 | v36-pre 四组合矩阵已通过 |

## 2. Feature 探测

libgit2 main 的 `git_feature_t` 仍无 `GIT_FEATURE_REFTABLE`。reftable 是内置后端，不是类似 SSH/HTTPS 的可选依赖；上游可能不会增加 feature bit。

当前项目策略：

1. 编译期：`libgit2_reftable` build tag；
2. 库能力：`IsReftableSupported()` 创建临时 reftable repo 探测；
3. 仓库格式：`Repository.RefStorageFormat()` 读取 `extensions.refStorage`。

SHA256 则直接使用 `FeatureSHA256` / `IsSha256Supported()`。

## 3. Per-worktree refs

`git_reference__is_per_worktree_ref` 仍位于 `src/libgit2/refs.h`，不属于公开 ABI。上游同时收紧静态符号可见性，因此不应从 git2go 绑定该内部符号。

如业务需要，可在 Go 层按 Git 规范实现名称判定；当前不作为 libgit2 绑定暴露，避免未来内部规则/符号变化导致 ABI 风险。

## 4. Promoted typed OID 整合

上游 SHA256 转正后：

```c
typedef struct git_oid {
    unsigned char type;
    unsigned char id[32];
} git_oid;
```

v36-pre 采用 `feat-sha256` 已验证实现：

- `Oid` 使用 `uint8 kind + [32]byte id`；
- Go/C 大小与字段偏移断言；
- `ObjectIdType` 统一 SHA1/SHA256 类型；
- typed OID、ODB、index、indexer、diff/hash APIs；
- `RepositoryInitOptions` 同时表达 `OidType` 与 `RefdbType`；
- `InitRepositoryWithOidType` 委托统一 `InitRepositoryExt`；
- `Oid{}` 在进入 C 前规范化为 typed SHA1；
- `GIT_STATIC` 与 promoted ABI 集中守卫。

旧 20-byte `git_oid` ABI（libgit2 v1.9.x）不与 v36-pre 兼容，继续由 git2go v35 维护。

详细影响面历史审计见 [oid-refactor-audit.md](./oid-refactor-audit.md)。

## 5. SHA1/SHA256 × files/reftable

整合后的矩阵：

| OID | ref backend | 结果 |
| --- | --- | --- |
| SHA1 | files | PASS |
| SHA1 | reftable | PASS |
| SHA256 | files | PASS |
| SHA256 | reftable | PASS |

覆盖：init、config、commit、branch CRUD、reopen、refdb compress、OID 长度和 format 探测。

## 6. 其他完成项

- custom refdb backend bridge v2：17/17 callback（4 项可选 capability）；
- per-iterator / lock payload 独立 handle；
- callback panic → libgit2 user error；
- backend/reference/reflog 所有权转移与幂等 Free；
- Transaction API；
- reftable reflog、symbolic refs、reopen persistence；
- xdiff 系统头污染修复；
- `DEPRECATE_HARD=ON` 构建；
- `v36.0.0-pre.N` 发布门禁。

## 7. 最终验证

```text
latest-main static full, tag on, no skips     PASS (201/0/0)
latest-main dynamic full, tag on, no skips    PASS
files-only latest-main, tag off               PASS
race: matrix/bridge/transaction               PASS
DEPRECATE_HARD=ON static full                 PASS
original xdiff crash set                      PASS
```

## 8. 剩余上游限制

- reftable transactions：内置 reftable backend 当前不提供 lock/unlock；
- namespace：上游测试仍记录 reftable 特殊限制；
- Windows 并发：`tables.list` rename/FILE_SHARE_DELETE 问题仍由上游处理；
- per-worktree ref classification 尚未公开。

这些限制不由 git2go 私自绕过，应随上游演进持续复核。
