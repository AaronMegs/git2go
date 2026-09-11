# reftable 事务处理方式调研与方案建议

> 调研日期：2026-09-07
> libgit2 基线：main `0551dfd4ad989b6a3d5683c0d4cf326c6efef929`（2026-08-15，**即当前上游 main tip**）
> git2go 分支：`feat-sha256-reftable`
> 结论摘要：这不是「上游还没写」的工时问题，而是 **libgit2 refdb 后端 vtable 与 reftable 存储模型的架构性失配**。git2go 侧的最佳方案是**保持现状 + 补齐可发现性**，不要自造绕过。

---

## 1. 问题陈述

`Repository.NewTransaction()` 在 reftable 仓库上可以创建，但首次 `LockRef` 即失败：

```text
backend does not support locking
```

原因是 libgit2 的 reftable 后端没有装配 `lock` / `unlock` 回调。

---

## 2. 证据链

### 2.1 上游确实未装配（一手证据）

`src/libgit2/refdb_reftable.c` 的后端构造函数装配了 12 个回调，并显式留下 TODO：

```c
backend->parent.init          = refdb_reftable_init;
backend->parent.exists        = refdb_reftable_exists;
backend->parent.lookup        = refdb_reftable_lookup;
backend->parent.iterator      = refdb_reftable_iterator_new;
backend->parent.write         = refdb_reftable_write;
backend->parent.rename        = refdb_reftable_rename;
backend->parent.del           = refdb_reftable_delete;
backend->parent.has_log       = refdb_reftable_has_log;
backend->parent.ensure_log    = refdb_reftable_ensure_log;
backend->parent.free          = refdb_reftable_free;
backend->parent.reflog_read   = refdb_reftable_reflog_read;
backend->parent.reflog_write  = refdb_reftable_reflog_write;
backend->parent.reflog_rename = refdb_reftable_reflog_rename;
backend->parent.reflog_delete = refdb_reftable_reflog_delete;
backend->parent.compress      = refdb_reftable_compress;
/* TODO: transaction API */          <-- lock/unlock 缺失
```

libgit2 允许这种缺失：`src/libgit2/refdb.c` 的完整性校验只要求 `lock` 与 `unlock`
**成对**出现，两者同时缺失是合法的：

```c
(backend->lock && !backend->unlock)   /* 只有单边才算不完整 */
```

失败点在 `git_refdb_lock`：

```c
if (!db->backend->lock) {
    git_error_set(GIT_ERROR_REFERENCE, "backend does not support locking");
    return -1;
}
```

### 2.2 根因：锁粒度失配（一手证据）

| | files 后端 | reftable 后端 |
| --- | --- | --- |
| 锁对象 | `loose_lock()` → 单个 `refs/heads/x.lock` | `flock_acquire(st->list_file)` → 整个 `tables.list` |
| 粒度 | **单引用** | **整个引用数据库** |
| 并发多把锁 | 支持（N 个 ref = N 个 .lock 文件） | **不支持**（只有一把库级锁） |

libgit2 的事务 API 是**逐引用**的：`git_transaction_lock_ref` 每次调用
`git_refdb_lock(refname)`，一个含 N 个引用的事务会持有 N 把锁。

### 2.3 实测验证：朴素映射必然失败

若把 `lock(refname)` 直接映射到 `reftable_stack_new_addition`，第二个引用就会失败。
用 C 探针直连 libgit2 静态库实测（`lock_timeout_ms = 100`，与 `refdb_reftable.c` 一致）：

```text
reftable_new_stack        -> 0
1st new_addition (ref A)  -> 0 ok
2nd new_addition (ref B)  -> -5 <-- BLOCKED: whole-DB lock already held
```

`-5` = `REFTABLE_LOCK_ERROR`。**即：朴素实现恰好在事务最有价值的场景（多引用原子更新）下崩溃。**

### 2.4 提交语义同样失配

`transaction.c` 的 `git_transaction_commit` 逐节点调用
`git_refdb_unlock(payload, success=true, ...)`，**每次 unlock 独立落盘**。

而 reftable 的原子性单位是一个 addition：

```c
int reftable_stack_new_addition(...);  /* 开启，副作用：锁住整个 ref 数据库 */
int reftable_addition_add(...);        /* 暂存 */
int reftable_addition_commit(...);     /* 一次性原子提交 */
void reftable_addition_destroy(...);   /* 回滚 */
```

libgit2 的 vtable 里**没有「全部暂存完毕，现在统一提交」的钩子**，因此无法表达
reftable 的 all-or-nothing 语义。当前 `refdb_reftable_write` 每次写入都是一个完整的
`reftable_stack_add`（自带 acquire→commit→release），所以多次写入之间本就不原子。

### 2.5 额外约束：双 stack

reftable 后端维护**两个独立 stack**，各有自己的 `tables.list` 锁：

```c
static int refdb_reftable_stack_for_refname(...)
{
    refdb_reftable_stack_t type = REFDB_REFTABLE_STACK_MAIN;
    if (git_reference__is_per_worktree_ref(refname))
        type = REFDB_REFTABLE_STACK_WORKTREE;
    ...
}
```

因此**跨 stack 的事务（例如同时更新 `refs/heads/x` 与 `HEAD`）即使实现了单 stack
的正确版本也无法原子**——需要两个 addition，而 reftable 不提供跨 stack 的两阶段提交。

### 2.6 对照：git 自身为什么没有这个问题

git 的 `struct ref_storage_be`（`refs/refs-internal.h`）是**事务优先**设计：

```c
ref_transaction_prepare_fn *transaction_prepare;  /* OPEN     -> PREPARED */
ref_transaction_finish_fn  *transaction_finish;   /* PREPARED -> CLOSED  */
ref_transaction_abort_fn   *transaction_abort;    /*          -> CLOSED  */
```

三个钩子的入参都是**整个 transaction**，不是单个 refname。这与 reftable 天然同构：

| git 钩子 | reftable 原语 |
| --- | --- |
| `transaction_prepare` | `reftable_stack_new_addition`（拿库级锁 + 校验） |
| `transaction_finish` | `reftable_addition_commit` |
| `transaction_abort` | `reftable_addition_destroy` |

**结论：libgit2 的 per-ref `lock`/`unlock` vtable 是为 files 后端的 `.lock` 文件模型
量身定制的；reftable 无法在不改动该 vtable 的前提下正确实现它。** 这解释了为什么上游
留的是 TODO 而不是简单补一个函数。

---

## 3. 候选方案评估

### 方案 A：保持现状，快速失败 + 可发现性（**推荐**）

git2go 不实现任何绕过，`LockRef` 继续返回明确错误，并提供事前探测能力。

- 成本：已完成（本分支现状）。
- 正确性：无风险。不会给出「看起来是事务、实际不原子」的假象。
- 已具备的能力：
  - `Repository.RefStorageFormat()` 事前判定后端；
  - `TestReftableTransactionUnsupported` 固化行为，并在上游实现后**主动失败**提醒更新文档；
  - `TestFilesTransactionSupported` 保证 files 后端事务不回退。

### 方案 B：git2go 侧模拟批量提交（**否决**）

思路：`LockRef` 只登记意图不真正加锁，计数到最后一次 `unlock` 时一次性 flush。

否决理由：

1. **无法可靠判定「最后一次 unlock」**。`git_transaction_commit` 出错会中途停止，剩余锁
   由 `git_transaction_free` 以 `success=false` 释放，计数启发式会 flush 出部分批次。
2. **违反 libgit2 契约**。`lock` 的语义是「已获得排他权」，延迟加锁会让并发写入在
   `unlock` 阶段才发现冲突，此时调用方已认为锁定成功。
3. 无法解决 §2.5 的跨 stack 问题。
4. 一个「假装原子」的事务比明确报错危险得多——静默数据损坏优先级高于功能缺失。

### 方案 C：git2go 直连 reftable 原语（**技术上不可行**）

实测两项阻断证据：

```text
reftable 头文件是否随 libgit2 安装 →  (空，未公开)
动态库中 reftable_* 可见符号数      →  0
对照：公开 API 可见符号数           →  2
```

- `deps/reftable/*.h` **不在** 安装的 include 目录中，不是公开 API；
- 共享库中所有 `reftable_*` 符号**已被 visibility 隐藏**，动态链接根本无法调用；
- 仅静态链接可见，但依赖内部符号无任何 ABI 稳定性承诺，且会让 static/dynamic 两种构建
  行为分叉。

### 方案 D：向上游贡献 vtable 扩展（**推荐的长期路径**）

参照 git 的设计，为 `git_refdb_backend` 增加事务级钩子（需 `GIT_REFDB_BACKEND_VERSION`
从 1 升到 2，通过版本协商保持既有后端兼容）：

```c
int (*transaction_prepare)(git_refdb_backend *, git_transaction *);
int (*transaction_finish)(git_refdb_backend *, git_transaction *);
int (*transaction_abort)(git_refdb_backend *, git_transaction *);
```

- files 后端保留现有 per-ref `lock`/`unlock`（向后兼容）；
- reftable 后端实现新三钩子，映射到 addition 生命周期；
- `git_transaction_commit` 优先走新钩子，回退到旧 lock/unlock。

这是唯一能让 reftable 获得**真正原子**事务的路径，但属于上游 API 演进，不应由 git2go
单方面模拟。**这也是 libgit2 v2 的合理观察点**：若 v2 调整 refdb vtable，git2go 的
`RefdbBackendInterface` 需相应扩展（届时是一次破坏性变更，宜与 v2 适配合并发布）。

---

## 4. 建议结论

| 项 | 建议 |
| --- | --- |
| 当前版本（v36-pre） | 采用**方案 A**。已实现，无需改动代码逻辑。 |
| 文档表述 | 从「上游尚未实现」改为说明**架构性失配**，避免读者误判为短期可解。 |
| 测试 | 保留现有两个测试；补一个多引用场景测试，固化「files 可多锁」这一被 reftable 破坏的前提。 |
| 长期 | 跟踪/推动**方案 D**；纳入 libgit2 v2 适配观察项。 |
| 用户指引 | 需要跨引用原子性且必须用 reftable 的场景，当前应改用单次 `Reference` 写入（reftable 每次写入自身是原子的），或暂留 files 格式。 |

### 4.1 给调用方的判定代码

```go
format, err := repo.RefStorageFormat()
if err != nil {
    return err
}
if format == git.RefdbReftable {
    // reftable：无事务。单次引用写入自身是原子的。
    _, err = repo.References.Create(name, target, true, msg)
    return err
}
// files：可用事务做多引用原子更新
tx, err := repo.NewTransaction()
if err != nil {
    return err
}
defer tx.Free()
// ... LockRef / SetTarget ...
return tx.Commit()
```

---

## 5. 附：复现方式

`lock`/`unlock` 缺失与错误信息：

```sh
go test --tags static -run 'TestReftableTransactionUnsupported|TestFilesTransactionSupported' -v ./...
```

锁粒度探针（§2.3）：编译一个直连 libgit2 静态库的 C 程序，连续调用两次
`reftable_stack_new_addition`，第二次返回 `REFTABLE_LOCK_ERROR (-5)`。需要的 include 路径：
`static-build/install/include`、`vendor/libgit2/deps/reftable`、`vendor/libgit2/src/util`、
`vendor/libgit2/src/libgit2`、`static-build/build/gen_headers`。
