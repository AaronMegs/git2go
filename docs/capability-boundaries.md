# 能力边界与绑定覆盖率

> 基线：git2go `feat-sha256-reftable`；libgit2 main `42a89d7`（本次直查官方 main，版本 `2.0.0-rc.1`）
> 审计日期：2026-10-21；以下结论限定于这一 pin，不表示未来 main 自动兼容。
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
| SHA256 + reftable 跨仓库传输 | 可用 | 常规 `Remote` API | `TestReftableTransportPersistence`：push/fetch、typed reflog、Drop 后重开 |
| 直接 clone 到四种格式 | 可用 | `CloneOptions.RepositoryCreateCallback` | `TestCloneRepositoryCallbackFormats` |
| 本地 bundle clone | 可用 | `Clone` | `TestCloneBundleFormats`：四组合、checkout 与重开 |
| SHA256 多 pack 目录 | 可用 | `NewOdbBackendPackWithOidType` | `TestSHA256PackDirectoryMultiplePacks`：双 pack、读取/枚举与重开 |
| SHA1/SHA256 thin pack | 可用 | `NewIndexerForOidType` | `TestIndexerThinPackRetainsOdb`：仅由 indexer 保活 ODB、GC 后 delta 解析 |
| reflog 读写 | 可用 | `Repository.ReadReflog` 等 | 13/13 API 全绑定 |
| 引用事务 | **仅 files** | `Repository.NewTransaction()` | 见 1.2.1 |
| Go 自定义 refdb 后端 | 可用（19 回调） | `NewRefdbBackendFromInterface` | 生命周期/并发/能力位/panic 转错误 |
| 运行时能力探测 | 可用 | `Features()`、`Version()`、`IsSha256Supported()`、`IsReftableSupported()` | — |

### 1.2 上游限制（git2go 无法绕过）

#### 1.2.1 公共引用事务 API 未接入 reftable

`Repository.NewTransaction()` 在 reftable 仓库上可创建，但首次 `LockRef` 返回
`backend does not support locking`：当前 `refdb_reftable.c` 未装配 `lock/unlock`。

这不表示 reftable 没有事务能力。其底层 addition 可在整库锁下原子提交一批记录，
上游 rename 等内部操作已使用该机制；只是没有通过公共 `git_transaction_*` 暴露。
逐引用加锁与整库 addition 的集成需要上游设计，不能将每次 `LockRef` 机械映射为一个 addition。
历史方案分析见 `docs/reftable-transaction-research.md`。

**同时纠正 files 事务语义**：`git_transaction_commit` 逐个提交引用，首次失败即返回；
先前已提交的修改不会回滚。它也不保证整个批次同时可见或全有全无。
`TestFilesTransactionMultipleRefsStagedCommit` 只验证暂存/提交结果，不证明批次原子性。
需要单条更新时可直接使用 `References.Create`；需要多引用全有全无时，不能把 files
事务当作 reftable 原子批次的等价替代。

#### 1.2.2 稳定版 `v36.0.0` 被上游阻塞

本次查询官方标签：最新稳定系列为 **v1.9.7**，另有 **v2.0.0-rc.1**（含 promoted typed OID，
仍是候选版）。不能再称「没有任何 release 包含该 ABI」，但稳定发布条件仍未满足。
v1.9.7 的 `include/git2/oid.h` 中：

- 无 `git_object_id_options`（promoted typed OID 的标志性类型）；
- SHA256 仍在 `#ifdef GIT_EXPERIMENTAL_SHA256` 之后；
- 默认构建的 `GIT_OID_MAX_SIZE` 仍为 20。

因此 promoted typed OID **尚未进入任何正式发布**。当前只能发 `v36.0.0-pre.N`；
本线的 ABI 守卫要求 `GIT_OID_MAX_SIZE == 32`，对 v1.9.x 会正确地编译期拒绝
（CI 的 `reject-legacy-v1-9-4` job 覆盖）。

#### 1.2.3 clone 引用格式：绑定缺口已补齐

此前「无法直接 clone 出 reftable」的结论不准确：`git_clone_options` 虽无 `refdb_type`，
但已有 `repository_cb`。现在 `CloneOptions.RepositoryCreateCallback` 暴露了它，
回调中用 `InitRepositoryExt` 选择 `OidType`、`RefdbType` 即可。

回调必须返回存活、拥有所有权的空仓库，对象格式与源仓库一致；成功后所有权交给 libgit2，
返回的 Go 包装对象失效，不可继续使用或保留子对象。错误时回调负责清理其已创建的资源。
同时修复了 nil options 解引用和 remote-create 回调将 panic 吞成成功的问题，
普通错误保留原值，panic（包括旧 Go 语义的 `panic(nil)`）转为 callback/User 错误。

`TestCloneRepositoryCallbackFormats` 验证四组合与所有权/重开；`TestCloneBundleFormats`
覆盖最新 main 新增的 bundle clone，含 SHA256。init-then-fetch 仍是可选路径。
上游 bundle transport 支持 clone/fetch，不支持 push 或 shallow；本轮直接实测 clone。

#### 1.2.4 reftable 不支持 namespace

`refdb_reftable.c` 对 namespace 返回 `GIT_ENOTSUPPORTED`（源码注释：
"TODO: this backend does not yet have support for namespaces"）。

**对 git2go 用户不可达** —— git2go 未绑定 namespace API（见 2.3），因此不构成实际风险。

#### 1.2.5 同 pack 路径不能混用 OID 格式

当前上游 `src/libgit2/mwindow.c` 的 `git_mwindow_get_pack` 按路径缓存 pack，
命中时复用已有结构，没有将 `oid_type` 纳入缓存键或校验。因此不要在同一进程中
用 SHA1、SHA256 同时打开同一 pack 路径；后端的 OID 类型必须匹配磁盘格式。

本轮隔离探针复现：先保留指向 SHA256 pack 目录的 SHA1 后端，再用 SHA256 后端读取，
正确类型的访问也受到缓存干扰；释放相关后端后，按正确类型重开恢复。探针仅用于调查，
未作为依赖错误行为的常驻测试。绑定层没有修改 vendored 源码或加进程级缓存绕过。
正常的同格式多 pack 读取、枚举与重开由 `TestSHA256PackDirectoryMultiplePacks` 覆盖。

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

### 2.1 本次适配域：引用覆盖与语义验证

| 模块 | 引用覆盖 | 说明 |
| --- | --- | --- |
| `refdb` | 4/4 | `git_refdb_backend_reftable` 位于 sys 头，不计入这 4 项 |
| `reflog` | 13/13 | 含本轮 Drop 后重开验证 |
| `transaction` | 8/8 | 符号覆盖完整；运行期受 1.2.1 限制 |
| `clone` | 2/2 | 该数字不包含 options 字段和回调；本轮仍补出了 repository 回调缺口 |
| `oid` | 2/22 | **20 项为有意不绑定**，见 2.2 |
| `odb` | 23/36 | typed 入口齐备；缺口见 2.4 |
| `odb_backend` | 3/5 | loose/one-pack/pack 已覆盖；另两项为 options init |
| `indexer` | 5/7 | 缺口非 SHA256 相关，见 2.2 |

符号出现不等于行为正确。本轮另行验证四组合 clone、typed 读流、thin pack 生命周期、
SHA256 多 pack 和传输后的 reflog 持久化；不能由引用比例推导「所有功能零缺口」。
旧 pin 到当前 main 的公开头变化仅涉及版本、两个 config backend 符号导出和新增
`git_transport_bundle`。bundle 可经现有 `Clone`/remote 入口使用，不必另暴露底层构造器。

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

**在当前 pin 与已测场景内，SHA256/reftable 的核心路径可用**。本轮补齐了真实的 clone
回调缺口，修正读流长度与 indexer/ODB 生命周期，而非仅凭符号统计判定完整。

- 四组合初始化、直接 clone、bundle clone 和重开测试通过；
- SHA256 多 pack 读取/枚举、typed thin pack 与 GC 后 delta 解析通过；
- SHA1/SHA256 reftable push/fetch、typed reflog 与 Drop 的重开持久化通过；
- static / race / dynamic / `libgit2_no_reftable` / 硬弃用宏编译轨道通过；
- clone 回调的普通错误、nil 返回、panic 及 `panic(nil)` 兼容语义均有回归测试。

`git_odb_backend_pack` 已绑定，不再列为遗留缺口。上游公共事务接入、namespace 和混合
OID pack 缓存限制仍然存在；完整平台验证与稳定版发布条件仍待外部验证，详见 §1。
`libgit2_no_reftable` 是 Go 绑定的关闭轨道，不等于对所有关闭 reftable 的 C 构建做过测试。
