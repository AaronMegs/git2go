# 能力边界与绑定覆盖率

> 基线：git2go `feat-sha256-reftable`；libgit2 main `0551dfd4`（= 上游 main tip）
> 审计日期：2026-09-11
>
> 本文回答两个问题：**当前能做什么、不能做什么**（能力边界），以及**哪些绑定尚未完成**
> （覆盖率缺口）。区分「上游限制」「有意不绑定」「真实缺口」三类，避免把设计决策误读为
> 缺陷、也避免把缺陷藏在「按设计如此」里。

---

## 1. 能力边界

### 1.1 可用能力

| 能力 | 状态 | 入口 | 验证 |
| --- | --- | --- | --- |
| SHA1 对象格式 | 默认 | `ObjectIdSHA1` | 全量套件 |
| SHA256 对象格式 | 可用 | `ObjectIdSHA256`、`IsSha256Supported()` | ODB/commit/index/diff/pack/clone/push 端到端 |
| files 引用存储 | 默认 | `RefdbFiles` | 全量套件 |
| reftable 引用存储 | 可用（默认编入） | `RefdbReftable`、`IsReftableSupported()` | 分支/符号引用/reflog/重开持久化 |
| 四组合（SHA1/SHA256 × files/reftable） | 全部可用 | `InitRepositoryExt` | `TestRepositoryFormatMatrix`，含真实提交与查找 |
| reftable 上的 fetch / push | 可用 | 常规 `Remote` API | `TestReftableFetchFromFilesRepo`、`TestReftablePushToReftableBare` |
| SHA256 + reftable 跨仓库传输 | 可用 | 常规 `Remote` API | `TestSHA256ReftableFetch` |
| reflog 读写 | 可用 | `Repository.ReadReflog` 等 | 13/13 API 全绑定 |
| 引用事务 | **仅 files** | `Repository.NewTransaction()` | 见 1.2.1 |
| Go 自定义 refdb 后端 | 可用（19 回调） | `NewRefdbBackendFromInterface` | 生命周期/并发/能力位/panic 转错误 |
| 运行时能力探测 | 可用 | `Features()`、`Version()`、`IsSha256Supported()`、`IsReftableSupported()` | — |

### 1.2 上游限制（git2go 无法绕过）

#### 1.2.1 reftable 不支持引用事务

`Repository.NewTransaction()` 在 reftable 仓库上可创建，但首次 `LockRef` 即失败：

```text
backend does not support locking
```

**这是架构失配，不是上游「还没写」**：libgit2 的 refdb vtable 是 per-ref 锁
（`lock(refname)` / `unlock(payload)`），为 files 后端的 `.lock` 文件模型定制；而 reftable
的原子性单位是一个持有**整库锁**的 addition。实测第二次 addition 返回
`REFTABLE_LOCK_ERROR (-5)`，即朴素映射恰好在多引用场景下失败。

详细论证与四方案评估见 `docs/reftable-transaction-research.md`。

**替代方案**：reftable 的单次引用写入自身是原子的，因此只有「多引用必须同时生效」的场景
受影响。按后端分支：

```go
format, err := repo.RefStorageFormat()
if err != nil {
    return err
}
if format == git.RefdbReftable {
    _, err = repo.References.Create(name, target, true, msg)
    return err
}
tx, err := repo.NewTransaction()
// ... LockRef / SetTarget / Commit ...
```

#### 1.2.2 稳定版 `v36.0.0` 被上游阻塞

最新 release 为 **v1.9.7**，其 `include/git2/oid.h` 中：

- 无 `git_object_id_options`（promoted typed OID 的标志性类型）；
- SHA256 仍在 `#ifdef GIT_EXPERIMENTAL_SHA256` 之后；
- 默认构建的 `GIT_OID_MAX_SIZE` 仍为 20。

因此 promoted typed OID **尚未进入任何正式发布**。当前只能发 `v36.0.0-pre.N`；
本线的 ABI 守卫要求 `GIT_OID_MAX_SIZE == 32`，对 v1.9.x 会正确地编译期拒绝
（CI 的 `reject-legacy-v1-9-4` job 覆盖）。

#### 1.2.3 `CloneOptions` 无法指定引用格式

libgit2 的 `git_clone_options` 不含 `refdb_type`，因此**无法直接 clone 出一个 reftable
仓库**。唯一路径是 init-then-fetch：

```go
repo, err := git.InitRepositoryExt(path, &git.RepositoryInitOptions{
    Flags:     git.RepositoryInitMkpath,
    RefdbType: git.RefdbReftable,
})
// 然后创建 remote 并 Fetch
```

此路径已由 `TestReftableFetchFromFilesRepo` 覆盖。

#### 1.2.4 reftable 不支持 namespace

`refdb_reftable.c` 对 namespace 返回 `GIT_ENOTSUPPORTED`（源码注释：
"TODO: this backend does not yet have support for namespaces"）。

**对 git2go 用户不可达** —— git2go 未绑定 namespace API（见 2.3），因此不构成实际风险。

### 1.3 平台验证边界

| 平台 | 状态 |
| --- | --- |
| macOS (darwin/arm64) | **本地全量实测**，五轨道通过 |
| Linux | 依赖 CI |
| Windows | 未验证，归入正式包阶段 |

### 1.4 测试可靠性边界

7 个测试需访问 `github.com`，非 hermetic。已由 `requiresNetwork(t)` 门控：
`go test -short` 或 `GIT2GO_SKIP_NETWORK_TESTS=1` 时跳过，或用
`make test-static-offline`。

---

## 2. 绑定覆盖率

按模块统计 libgit2 公开 `GIT_EXTERN` 与 git2go 实际引用（审计脚本见 §2.5）。

### 2.1 本次适配域：完整

| 模块 | 覆盖 | 说明 |
| --- | --- | --- |
| `refdb` | **4/4 100%** | 含 `git_refdb_backend_reftable` |
| `reflog` | **13/13 100%** | |
| `transaction` | **8/8 100%** | 绑定完整；运行期受 1.2.1 限制 |
| `oid` | 2/22 | **20 项为有意不绑定**，见 2.2 |
| `odb` | 23/36 | typed 入口齐备；缺口见 2.4 |
| `indexer` | 5/7 | 缺口非 SHA256 相关，见 2.2 |

reftable / SHA256 的核心 API **无缺口**。

### 2.2 有意不绑定（非缺陷）

**`oid` 模块的 20 项** —— 在 Go 侧原生实现，语义更贴合 Go 且修正了一处上游缺陷：

| libgit2 | Go 等价 |
| --- | --- |
| `git_oid_cpy` | `Oid.Copy` |
| `git_oid_equal` / `git_oid_cmp` / `git_oid_ncmp` | `Oid.Equal` / `Cmp` / `NCmp`（类型感知） |
| `git_oid_is_zero` | `Oid.IsZero` |
| `git_oid_tostr` / `tostr_s` / `fmt` / `nfmt` | `Oid.String` |
| `git_oid_fromstr` / `fromstrn` / `fromstrp` / `from_string` | `NewOid`（Go 侧 hex 解析） |
| `git_oid_fromraw` / `from_raw` | `NewOidFromBytes` / `NewOidFromBytesWithType` |
| `git_oid_shorten_new` / `add` / `free` | `ShortenOids` |

其中 `ShortenOids` 是**必须**用 Go 重写的：`git_oid_shorten` 只检查前 40 个 hex 字符，
对第 40 位之后才分叉的 SHA256 id 会返回不足以区分的前缀长度。

**`git_indexer_hash`** —— 上游已标注 `@deprecated use git_indexer_name`，
git2go 正确使用了 `git_indexer_name`。

**`git_object_id_options_init`** —— C 胶水层使用等价的 `GIT_OBJECT_ID_OPTIONS_INIT` 宏
（`wrapper.c:570`、`578`），初始化语义相同。

**`git_odb_exists_ext`** —— 是 lookup flags 功能，与对象格式无关。

### 2.3 整模块未绑定（历史既有，非本次范围）

| 模块 | 未绑定 | 影响 |
| --- | --- | --- |
| `worktree` | 0/15 | 无法创建/枚举 worktree。**连带效应**：上游 reftable 的 main/worktree 双 stack 对 git2go 不可达 |
| `pathspec` | 0/13 | 无路径规格匹配 API |
| `filter` | 0/11 | 无自定义 filter（clean/smudge） |
| `attr` | 0/10 | 无 gitattributes 查询 |
| `mailmap` | 0/7 | 无 mailmap 支持 |
| `email` | 0/2 | 无 `git format-patch` 等价 |
| `trace` | 0/1 | 无 libgit2 内部追踪 |
| `credential_helpers` | 0/1 | 无 credential helper 集成 |
| `deprecated` | 0/68 | **有意**：本线已清除全部硬废弃依赖 |

这些在本次整合前即为空白，与 SHA256/reftable 无关。`deprecated` 的 0/68 是主动成果，
已由 `DEPRECATE_HARD=ON` + cgo 层 `-DGIT_DEPRECATE_HARD` 双重验证。

### 2.4 真实缺口

#### 2.4.1 `git_odb_backend_pack` —— 已于 2026-09-11 绑定

此前 git2go 只绑定了 loose 与 one-pack 两种 ODB 后端，缺少 pack 目录后端，而
`git_odb_backend_pack_options` 含 `oid_type`，因此在 SHA256 语境下是有意义的缺口。

现已补齐 `NewOdbBackendPack` / `NewOdbBackendPackWithOidType`，与另两个后端对齐。
`odb_backend` 模块覆盖率由 2/5 升至 3/5。

> 审计过程中的一个教训：`wrapper.c` 里出现的 `git_odb_backend_pack_options` 是 `one_pack`
> 复用了同一个 options 类型（上游两个后端共用），**不代表 `git_odb_backend_pack` 函数
> 已被绑定**。纯文本的覆盖率扫描会把这类复用计为「已引用」，结论必须逐项复核。
> 审计脚本已改为只匹配函数调用形态。

#### 2.4.2 options 版本校验 —— 已于 2026-09-11 以启动时集中检查覆盖

`git_*_options_init` 会拒绝它不认识的版本号，而 git2go 传入的版本是**编译期常量**。
动态链接时头文件可能比运行库新，此时 init 返回 -1，而结构体仍保留栈上的残留数据——
把它交给 libgit2 是未定义行为。

上游的失败条件已核实（`src/libgit2/common.h` 的 `git_error__check_version`）：
当传入版本超出运行库支持范围时返回 -1。

由于版本偏斜是**全局静态属性**（不随调用变化），采用启动时一次性校验 22 个 options
结构，而非在约 30 个调用点分别检查：

- 报错时机更早——在任何仓库被操作之前，而不是从某个无关操作里冒出来；
- 无需改动 14 个 `populate*` 辅助函数的签名（它们返回 `*C.git_xxx_options`，
  加 error 会波及全部调用方）。

实现见 `options_version.go`，由 `initLibGit2` 调用并在失败时 panic，与既有的线程支持
检查（`git.go`）和 `git_oid` 布局检查（`oid_typed.go`）保持一致。

仍未绑定的 `git_odb_backend_loose_options_init` / `git_odb_backend_pack_options_init`
是这一策略的结果：C 胶水层使用等价的 `*_OPTIONS_INIT` 宏，版本正确性由启动校验保证。

### 2.5 复现方式

```sh
python3 script/audit-binding-coverage.py              # 全模块覆盖率
python3 script/audit-binding-coverage.py oid odb      # 指定模块的未绑定符号
```

脚本从各公开头文件提取 `GIT_EXTERN` 声明，再检查符号是否出现在绑定的 Go/C 源码中。
注意它是**文本层面**的近似：符号出现在注释中也会计为已引用，因此结果应作为「需要人工
判定的候选清单」而非结论。§2.2 的每一项都经过了逐个复核。

---

## 3. 未完成项汇总

按可行性而非严重性排序。

### 3.1 可立即进行（非破坏性）—— 已全部完成 2026-09-11

| # | 项 | 依据 |
| --- | --- | --- |
| ~~1~~ | ~~绑定 `git_odb_backend_pack`~~ | **已完成** 2026-09-11 |
| ~~2~~ | ~~`IsErrorCode` 改用 `errors.As`~~ | **已完成**（`IsErrorClass` 一并） |
| ~~3~~ | ~~`PruneRefs`、`OdbObject.Data` 补 `KeepAlive`~~ | **已完成**（另含 3 处 writer） |
| ~~4~~ | ~~`ReflogEntry` 改为深拷贝~~ | **已完成**，变异验证有效 |
| ~~5~~ | ~~收紧线程锁门禁~~ | **已完成**，并给出区分性诊断 |
| ~~6~~ | ~~迁移剩余 `reflect.SliceHeader`~~ | **已完成**，全仓库归零 |
| ~~7~~ | ~~strarray 释放函数混用~~ | **已完成** |
| ~~8~~ | ~~options init 返回值未检查~~ | **已完成**，改为启动时集中校验 |

### 3.2 破坏性，建议与 libgit2 v2 适配合并

| # | 项 | 理由 |
| --- | --- | --- |
| 9 | 统一迭代器结束语义（当前三种表达） | 一次性迁移，避免多次破坏调用方 |
| 10 | 统一 `Free()` 签名（当前两派） | 同上 |
| 11 | 移除 `git_openssl_set_locking` 的进程级副作用 | 同上 |

### 3.3 外部阻塞

| # | 项 | 阻塞点 |
| --- | --- | --- |
| 12 | 稳定 `v36.0.0` | 上游未发布含 promoted typed OID 的正式版（§1.2.2） |
| 13 | reftable 引用事务 | 需上游 refdb vtable 扩展（§1.2.1） |
| 14 | Windows / Linux 平台验证 | 依赖 CI 与第 12 项 |

### 3.4 可选的功能扩展（超出本次范围）

`worktree`、`pathspec`、`filter`、`attr`、`mailmap`、`email` 六个模块整体未绑定（§2.3）。
其中 **`worktree` 与 reftable 有关联**：上游为 per-worktree 引用维护独立的 reftable
stack，绑定 worktree 后才需要考虑该路径的测试覆盖。

---

## 4. 判断

**SHA256 与 reftable 的适配在本次范围内已完善可用**：

- 核心 API 零缺口（refdb / reflog / transaction 各 100%）；
- 四组合矩阵与传输层均实测通过，且经变异验证（改为 `RefdbFiles` 后断言确实失败）；
- 五条构建轨道全绿：static / race / dynamic / `libgit2_no_reftable` / `DEPRECATE_HARD`；
- 能力缺失正确降级为 SKIP 而非 FAIL（13 项）。

遗留项中，唯一与 SHA256 相关的绑定缺口是 `git_odb_backend_pack`（§2.4.1），
且为历史既有、影响限于手工组装 ODB 的高级场景。其余为独立模块空白或需等待上游。
