# libgit2 最新 main 下的 reftable 适配完整性审计

> 审计日期：2026-08-04
> 上游基线：libgit2 main `939362a3c`
> 当前 vendor：`939362a3c`（已于 commit `8b4a398` 同步至该审计基线）
> 项目分支：`feat-reftable`
> 审计范围：公开 API 映射、ABI/构建兼容、自定义 refdb 后端、测试矩阵、上游已知限制、后续实施方案。

---

## 0. 结论摘要

### 0.1 总体结论

当前实现对原 vendor `ddf3b5c85` 的 **SHA1 + reftable** 场景已经具备较完整的使用能力；vendor 现已更新为 `939362a3c`，因此下一步构建会由 `git_oid` ABI 守卫主动阻止，直至完成 `Oid` 重构：

- 能初始化/打开 reftable 仓库；
- 能识别引用存储格式；
- 通用引用/分支 API 可透明工作；
- 能显式创建 files/reftable 后端；
- 能执行 reftable stack compaction；
- reflog 公开 API 已全部绑定；
- v1.9.x（无 reftable）可通过 build tag 安全降级；
- 已有针对 files/reftable 的测试与 CI job。

但是，**不能把当前状态描述为“已完整适配最新 libgit2 main”或“全部 refdb 公开 API 100% 完成”**。本次审计发现以下阻塞/缺口：

| 等级 | 发现 | 影响 |
| --- | --- | --- |
| **P0** | 最新 main 已将 SHA256 转正，`git_oid` 从 20 字节变为 33 字节；当前 `Oid [20]byte` 不兼容 | 现有 ABI 守卫会（正确地）阻止编译；因此当前项目**无法链接最新 main** |
| **P1** | 自定义 `git_refdb_backend` 桥接只覆盖 **13/17** 个 backend 回调，缺 `init` / `compress` / `lock` / `unlock` | 自定义后端无法初始化、无法实现 backend compaction、**无法使用 transaction API** |
| **P1** | 自定义 backend iterator 以 backend 级单例保存 Go iterator | 多 iterator/并发场景相互覆盖；旧 iterator 泄漏/失效 |
| **P1** | iterator `free` 未调用 Go `RefdbBackendIterator.Free()`；`next_name` 每次 `strdup` 且不回收 | Go 资源泄漏 + C 字符串泄漏 |
| **P1** | CI 的“stable-compatible” job 实际仍链接 main，只是不加 tag；system dynamic 矩阵仍写 `v1.5.0`，与 `>=1.9` 守卫冲突 | CI 没有真实持续验证 v1.9.4；旧 job 预计无法通过版本守卫 |
| **P1** | 最新 main 静态可见性宏改为 `GIT_STATIC`；当前 Build 文件仍定义 `LIBGIT2_STATIC` | 升级后 Windows/静态消费者存在符号导入/可见性风险 |
| **P2** | reftable 测试集中在初始化、分支 CRUD、format、compress；未覆盖 reopen 持久化、reflog-on-reftable、namespace/worktree/concurrency 等 | 核心路径已验证，但兼容矩阵不够完整 |
| **P2** | `Repository.SetRefdb` 丢弃 C 返回码，且未 `runtime.KeepAlive(refdb)` | 发生错误时调用方不可见；生命周期表达不完整 |

**推荐表述**：

> 当前 git2go 已完成基于旧 vendor `ddf3b5c85` 的 **SHA1 + reftable 核心能力适配**，并具备 v1.9.x 安全降级能力；vendor 已同步到最新 main `939362a3c`，但编译会被 SHA256 `git_oid` ABI 守卫阻止，必须先完成 `Oid` 重构。自定义 refdb backend 桥接与测试矩阵亦仍需补齐。

---

## 1. 最新上游状态

### 1.1 上游版本差异

```text
previous vendor: ddf3b5c85
current vendor:  939362a3c
upstream main:   939362a3c
commits behind:  0（以 2026-08-04 审计时点计）
```

子模块更新已由 git2go commit `8b4a398` 提交。由于该版本含 SHA256 转正后的 33 字节 `git_oid`，当前项目在完成 `Oid` 重构前**预期无法构建**；这是 `git2go_version_check.h` 的安全守卫行为，不是构建回归。

vendor 之后与 reftable/oid 直接相关的上游 commit：

| commit | 内容 |
| --- | --- |
| `605f34a01` | `sha256: it's what's for breakfast`（SHA256 转正） |
| `3db3d5fb6` | `reftable: include testrepo_256 reftable resource` |
| `9b1ca879f` | `reftable: copy the id value, not the struct` |

最新 4 个 commit（PR #7331/#7332）只修复 patch/index，与 reftable 无关。

### 1.2 reftable 公开 API 是否变化

`ddf3b5c85..939362a3c` 之间：

- `include/git2/refdb.h`：**无变化**；
- `include/git2/sys/refdb_backend.h`：**无变化**；
- `include/git2/reflog.h`：**无变化**；
- `include/git2/reference.h`：**无变化**；
- `git_repository_init_options.refdb_type`：签名/位置语义未变；
- `git_refdb_backend_reftable()`：签名未变。

因此，**最新 main 对 reftable API 的直接变化很小**；真正的兼容性断点来自 SHA256 转正导致的 `git_oid` ABI 变化。

### 1.3 最新 main 的 reftable + SHA256 实现

`refdb_reftable.c` 的变化：

1. 移除 `GIT_EXPERIMENTAL_SHA256` 条件编译；
2. 始终根据 `repo->oid_type` 选择 `REFTABLE_HASH_SHA1` / `REFTABLE_HASH_SHA256`；
3. 从 `git_oid` 复制时改为复制 `oid->id`，而不是错误地把包含 `type` 字段的整个 struct 起始地址当成哈希字节。

上游已增加 `tests/resources/reftable/testrepo_256`，说明 **SHA256 + reftable 已是正式支持组合**。

---

## 2. 当前项目已完成能力

### 2.1 repository init / 格式选择

| 上游 API | git2go | 状态 |
| --- | --- | --- |
| `git_repository_init_ext` | `InitRepositoryExt` | ✅ |
| `git_repository_init_options`（flags/mode/path/template/head/origin） | `RepositoryInitOptions` | ✅ |
| `git_repository_init_options.refdb_type` | `RepositoryInitOptions.RefdbType` + build-tag shim | ✅（SHA1 main） |
| `git_refdb_t` | `RefdbType`（Default/Files/Reftable） | ✅ |
| `git_repository_init_options.oid_type` | 无 | ❌（等待 `Oid` 重构） |

### 2.2 refdb 公开函数

| 上游 API | git2go | 状态 |
| --- | --- | --- |
| `git_refdb_new` | `Repository.NewRefdb` | ✅ |
| `git_refdb_open` | `Repository.OpenRefdb` | ✅ |
| `git_refdb_compress` | `Refdb.Compress` | ✅ |
| `git_refdb_free` | `Refdb.Free` | ✅ |
| `git_repository_refdb` | `Repository.Refdb` | ✅ |
| `git_repository_set_refdb` | `Repository.SetRefdb` | ⚠️ 已绑定但丢弃返回码 |
| `git_refdb_set_backend` | `Refdb.SetBackend` | ✅ |
| `git_refdb_backend_fs` | `Repository.NewRefdbBackendFs` | ✅ |
| `git_refdb_backend_reftable` | `Repository.NewRefdbBackendReftable` | ✅（tag on） |
| `git_refdb_init_backend` | C bridge 内调用 | ✅（但回调结构不完整） |

### 2.3 运行时探测

| 场景 | API | 状态 |
| --- | --- | --- |
| 构建是否启用 reftable 绑定 | `libgit2_reftable` build tag | ✅ |
| 链接库是否真的支持 reftable | `IsReftableSupported()`（临时 init 探测） | ✅ |
| 某仓库实际 ref 格式 | `Repository.RefStorageFormat()` | ✅ |
| SHA256 是否可用 | `IsSha256Supported()`（feature flag） | ✅ |
| 仓库 oid type | `Repository.OidType()` | ✅（只读探测） |

最新 main 仍无 `GIT_FEATURE_REFTABLE`，所以当前三层探测策略合理。

### 2.4 reflog

`include/git2/reflog.h` 的 13 个公开函数均已映射：

- repository：read / rename / delete；
- reflog：write / append / entrycount / entry_byindex / drop / free；
- entry：id_old / id_new / committer / message。

该部分与最新 main 保持兼容（前提仍是先解决全局 `Oid` ABI）。

### 2.5 稳定版兼容

- `libgit2_reftable` tag 隔离 main-only C 符号；
- 无 tag 时 `IsReftableSupported() == false`；
- 无 tag 时请求 `RefdbReftable` 返回明确错误；
- 已做过真实 v1.9.4 构建/编译验证：无 tag 成功、带 tag 按预期因缺符号失败。

该设计方向正确。

---

## 3. P0：最新 main 的 `git_oid` ABI 阻塞

最新 main：

```c
typedef struct git_oid {
    unsigned char type;
    unsigned char id[32];
} git_oid;  /* sizeof=33 */
```

当前 git2go：

```go
type Oid [20]byte

func (oid *Oid) toC() *C.git_oid {
    return (*C.git_oid)(unsafe.Pointer(oid))
}
```

如果直接升级 vendor，所有 OID 读写都会错位/越界。已经落地的：

```c
#if GIT_OID_MAX_SIZE != 20
# error "Incompatible libgit2 ..."
#endif
```

因此现在的状态是：

- ✅ vendor 已指向最新 main `939362a3c`；
- ✅ 不会静默损坏内存；
- ❌ 当前会在编译期阻止链接该 vendor；
- ❌ `RepositoryInitOptions.OidType` 尚不能安全提供；
- ❌ SHA256 + files/reftable 组合不能运行。

专项审计 `docs/oid-refactor-audit.md` 已确认应采用 **方案 A**：

```go
type Oid struct {
    Type uint8
    ID   [32]byte
}
```

该布局精确匹配 C 的 33/1/1，能保留唯一的 `unsafe.Pointer` 强转与全部 44 个调用点（包含 20 个 OUT 参数）。但数组 API 会破坏，需 v36 major bump。

**结论**：在完成 v36 `Oid` 重构前，项目只能支持 pinned SHA1 main，而不能宣称最新 main 兼容。

---

## 4. P1：自定义 refdb backend 桥接并不完整

### 4.1 回调覆盖率

最新 main（也包括当前 vendor）的 `git_refdb_backend` 有 **17 个 backend 回调**：

```text
init
exists
lookup
iterator
write
rename
del
compress
has_log
ensure_log
free
reflog_read
reflog_write
reflog_rename
reflog_delete
lock
unlock
```

当前 `RefdbBackendInterface` 覆盖 13 个，缺：

| 缺失回调 | 上游语义 | 影响 |
| --- | --- | --- |
| `init` | 可选；初始化新 refdb、创建 HEAD | 自定义后端无法参与 repository init |
| `compress` | 可选；backend-specific optimization | `Refdb.Compress()` 对自定义后端只能 no-op/失败，无法委托 Go 实现 |
| `lock` | 可选，但没有它 transaction API 会失败 | 自定义后端无法支持引用事务 |
| `unlock` | 提供 lock 时必须提供 | 无法提交/回滚事务更新、reflog 更新 |

因此此前文档中的“13 个回调 = 完整桥接”“全部公开 refdb API 100% 完成”是**不准确的**。

### 4.2 iterator 生命周期/并发缺陷

当前 Go state：

```go
type refdbBackendState struct {
    backend  RefdbBackendInterface
    iterator RefdbBackendIterator  // backend 级单例
}
```

问题：

1. 每次创建 iterator 会覆盖 `state.iterator`；多个 iterator 无法并存；
2. 并发 iterator 存在数据竞争；
3. C `_go_refdb_iter_free()` 只 `free(iter)`，**未调用 Go iterator.Free()**；
4. backend free 只释放最后一个 iterator；更早的 iterator 泄漏；
5. `next_name` 通过 `strdup()` 每次分配字符串，但 managed iterator 没有保存/回收上次分配，形成逐项泄漏。

### 4.3 正确重构方案

应把 iterator 改为**每实例独立 handle**：

```c
typedef struct {
    git_reference_iterator parent;
    void *iterator_handle;       /* 每个 Go iterator 独立 */
    char *last_name;             /* next_name 返回缓冲，下一次/Free 时回收 */
} _go_managed_refdb_iterator;
```

Go 侧：

- backend state 只保存 backend；
- `Iterator()` 返回后单独 `pointerHandles.Track(iter)`；
- `next` / `next_name` 根据 iterator handle 找到实例；
- iterator C `free` trampoline：调用 Go `Free()` + Untrack + free last_name + free C wrapper。

同时给 `RefdbBackendInterface` 补 `Init` / `Compress` / `Lock` / `Unlock`。为避免让现有实现一次性增加四个方法，可拆分可选能力接口：

```go
type RefdbBackendInitializer interface { Init(...) error }
type RefdbBackendCompressor interface { Compress() error }
type RefdbBackendLocker interface {
    Lock(refName string) (RefdbBackendLock, error)
    Unlock(lock RefdbBackendLock, update RefdbBackendUpdate) error
}
```

C wrapper 根据 interface assertion 是否成功决定是否设置对应 function pointer。这样与上游“optional callback”语义一致，也保持已有 `RefdbBackendInterface` 实现兼容。

---

## 5. P1：构建与 CI 差异

### 5.1 静态可见性宏

最新 main 的 `include/git2/common.h` 改为：

```c
#if defined(_WIN32) && !defined(GIT_STATIC)
# define GIT_EXTERN_DECL __declspec(dllimport)
...
#endif
```

而 git2go 当前：

```go
#cgo CFLAGS: -DLIBGIT2_STATIC
```

上游最新 main 使用的是 `GIT_STATIC`，不是 `LIBGIT2_STATIC`。在 Unix 上可能仅影响 visibility；在 Windows 静态链接上可能把 API 声明为 `dllimport`，导致链接/符号问题。

**升级 vendor 时必须**：

- static 构建 CFLAGS 改为至少 `-DGIT_STATIC`（可暂时同时保留 `-DLIBGIT2_STATIC` 兼容旧版）；
- 验证 Windows static job。

### 5.2 CI 的 stable/main 双轨不是真正双轨

当前 `build-reftable-disabled`：

- 仍构建 vendored **main**；
- 只是 Go 编译时不加 `libgit2_reftable` tag。

它只能验证“main 库 + stable Go API 子集”，不能证明真实 v1.9.x 链接兼容。

此外 `build-system-dynamic` matrix 仍写：

```yaml
libgit2: v1.5.0
```

但本项目集中式版本守卫要求 `>= 1.9.0`，该 job 与支持范围冲突。

**修复方案**：

1. 将 system dynamic 改为 v1.9.4；
2. 新增/改造 stable job：真正构建 v1.9.4 + no reftable tag；
3. main job：当前 vendor/latest main + `libgit2_reftable` tag；
4. 最新 main job在 v36 `Oid` 重构前应预期被 ABI 守卫阻止，不应贸然启用；
5. actions/ubuntu/Go 版本统一现代化。

---

## 6. P2：测试覆盖矩阵差距

### 6.1 已有覆盖

新增相关测试共 26 个，覆盖：

- InitRepositoryExt（files/nil/reftable）；
- IsReftableSupported / RefStorageFormat；
- Refdb/OpenRefdb/Compress；
- files/reftable backend constructor；
- reftable 分支 CRUD；
- 自定义 backend lookup/free 基础桥接；
- reflog 全生命周期（但主要在 files repo）；
- OidType/Version 探测。

全量回归（排除已知本机 xdiff/环境项）为 141 个用例基线。

### 6.2 建议补充场景

| 优先级 | 场景 | 当前状态 |
| --- | --- | --- |
| P1 | **关闭并重新打开 reftable 仓库**后验证引用/HEAD/分支持久化 | 未覆盖 |
| P1 | **reftable reflog**：append/write/drop/rename/delete | 现有 reflog 测试只覆盖默认 files repo |
| P1 | 自定义 backend 全 17 回调 + iterator 多实例/并发/lifecycle | 仅 lookup/free 基础测试 |
| P1 | SHA1/SHA256 × files/reftable 四组合 | SHA256 被 `Oid` 阻塞 |
| P2 | symbolic ref create/rename/delete/resolve（reftable） | 无专项覆盖 |
| P2 | namespace 行为（reftable） | 无；上游已知 `git_reference_list` 会失败 |
| P2 | worktree + per-worktree refs | git2go 缺完整 worktree API / upstream per-worktree 判定未公开 |
| P2 | 并发 ref CRUD/compaction | 无 |
| P2 | Windows threaded reftable | 无；上游目前明确 skip（`tables.list` rename / FILE_SHARE_DELETE 问题） |
| P2 | transaction API | git2go 无 transaction 绑定；且上游 reftable backend 不实现 lock/unlock，上游 transaction tests 对非-files 直接 skip |

### 6.3 上游已知 reftable 限制（不能由 git2go“修复”）

从上游最新 main 测试可见：

1. **Transactions**：`tests/libgit2/refs/transactions.c` 对非-files format 直接 skip；reftable backend 也未设置 lock/unlock。
2. **Namespaces**：reftable 下 `git_reference_list` 当前被预期为失败。
3. **Windows 并发 refdb**：reftable 线程测试被 skip，原因是 `tables.list` 文件 rename / `FILE_SHARE_DELETE` / POSIX rename semantics 尚未解决。
4. **per-worktree 判定**：`git_reference__is_per_worktree_ref` 仍为内部 API。

项目文档应明确这些上游限制，避免将“透明后端”理解为所有高级场景都已等价。

---

## 7. 其他代码质量问题

### 7.1 `Repository.SetRefdb`

当前：

```go
func (v *Repository) SetRefdb(refdb *Refdb) {
    C.git_repository_set_refdb(v.ptr, refdb.ptr) // 返回码被丢弃
    runtime.KeepAlive(v)                         // 未 KeepAlive(refdb)
}
```

建议改为：

```go
func (v *Repository) SetRefdb(refdb *Refdb) error {
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    ret := C.git_repository_set_refdb(v.ptr, refdb.ptr)
    runtime.KeepAlive(v)
    runtime.KeepAlive(refdb)
    if ret < 0 {
        return MakeGitError(ret)
    }
    return nil
}
```

Go 允许把有返回值的函数调用作为 statement 丢弃，因此多数现有 `repo.SetRefdb(refdb)` 调用仍可编译；需要检查赋值/接口用法。

### 7.2 config 测试竞争

`TestConfigLookups` 与 `TestConfigEntryBackendType` 都 `t.Parallel()` 且共享 `./temp.gitconfig`，`-p 1` 只限制 package 并行，**不会禁止同一 package 内 `t.Parallel()`**。此前文档称“`-p 1` 可规避”并不严谨。

正确修复：每个测试使用 `t.TempDir()` 下的独立 config 路径；不要依赖串行参数。

### 7.3 `RefStorageFormat` 的未知值处理

当前遇到未知/未来 `extensions.refStorage` 值时返回 `RefdbFiles, nil`。这可能掩盖新格式。建议增加 `RefdbUnknown` 或返回错误，避免把未知格式误报为 files。

---

## 8. 分阶段修复路线

### 阶段 A：修复当前 vendor 下的完整性问题（不升级 libgit2）

1. **自定义 backend bridge v2**
   - 可选 capability interfaces：Init/Compress/Lock/Unlock；
   - per-iterator 独立 handle；
   - iterator Free/Untrack；
   - next_name 缓冲管理；
   - 多 iterator + lifecycle + callback 错误传播测试。
2. `Repository.SetRefdb` 返回错误并修正 KeepAlive。
3. reftable 测试扩展：reopen persistence、reftable reflog、symbolic refs。
4. CI 修复：真实 v1.9.4 stable job、移除 v1.5.0、修复 config 测试竞争。
5. 文档修正：撤回“全部公开 API 100% 完成”，记录上游 transactions/namespaces/Windows/per-worktree 限制。

### 阶段 B：解锁最新 main（v36）

1. ✅ vendor 已升级到 main `939362a3c`（commit `8b4a398`）；
2. 按 `docs/oid-refactor-audit.md` 方案 A 重构 `Oid`；
3. 更新 ABI 守卫：`GIT_OID_MAX_SIZE == 32` + Go/C layout 断言；
4. 更新所有 `git.go` 数组/长度依赖；
5. 接入新式 `git_oid_from_*` API；
6. 给 `RepositoryInitOptions` 增加 `OidType`；
7. static CFLAGS 增加 `GIT_STATIC`；
8. 全量回归。

> vendor 更新被有意提前执行；在第 2–7 步完成前，工作分支处于“vendor 已同步、Go 绑定尚待 ABI 迁移”的预期不可构建状态。

### 阶段 C：SHA256 + reftable 完整矩阵

- SHA1 + files；
- SHA1 + reftable；
- SHA256 + files；
- SHA256 + reftable；
- 每个组合验证 init/reopen/commit/reference/branch/reflog/config；
- SHA256 断言 64 hex、`extensions.objectFormat=sha256`；
- reftable 断言 `extensions.refStorage=reftable` 与 stack persistence。

---

## 9. 最终判定

| 维度 | 判定 |
| --- | --- |
| 当前 pinned vendor 上的 SHA1 + reftable 核心功能 | ✅ 可用，覆盖较好 |
| v1.9.x 无 reftable 安全降级 | ✅ 已真实验证 |
| reftable/refdb 直接公开函数映射 | ✅ 基本完整 |
| 自定义 refdb backend 完整桥接 | ❌ 13/17，iterator 设计有生命周期/并发问题 |
| 最新 main 编译兼容 | ❌ 被 `git_oid` ABI 守卫阻止（正确行为） |
| 最新 main SHA256 + reftable | ❌ 尚未落地，需 v36 `Oid` 重构 |
| 高级场景（transaction/namespace/worktree/Windows concurrency） | ⚠️ 部分为上游限制，部分缺绑定/测试 |
| “整体完整性适配完成” | **尚不能下此结论** |

**建议下一步**：先执行阶段 A，把当前 vendor 下能解决的完整性问题补齐；再决定 v36 并执行阶段 B/C。这样能把“绑定自身缺陷”与“上游 SHA256 ABI 大迁移”解耦，降低回归风险。
