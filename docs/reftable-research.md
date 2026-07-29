# Reftable 适配调研报告

> 目标：分析上游 libgit2 main 中 reftable 的支持现状，结合本项目（git2go fork）当前绑定情况，给出兼容性适配的范围、落地内容，以及后续 roadmap。

调研日期：2026-06-30（初版）、2026-07-29（第二轮：完整 refdb/reftable API 绑定）
本项目分支：`feat-reftable`
本项目模块：`cnb.cool/cnb/git2go/v35`

> **两轮适配概述**
> - **第一轮（06-30）**：vendor 升到 `32b564e63`（含 PR #7117），绑定 `git_repository_init_ext` + `RepositoryInitOptions`（含 `refdb_type`）+ `git_refdb_t` 枚举，产出本报告初版与 3 个 init 冒烟测试。
> - **第二轮（07-29）**：vendor 升到最新 main `ddf3b5c85`（含 reftable 更新 PR #7327），补齐**全部** refdb/reftable 公开 API 绑定（后端构造函数、compaction、refdb 获取/打开、运行时探测、后端格式识别），新增 11 个覆盖 files/reftable 双后端的测试。

---

## 1. 上游 libgit2 main 的 reftable 现状

### 1.1 来源与时间线

- PR：[libgit2/libgit2#7117 "Reftables support"](https://github.com/libgit2/libgit2/pull/7117)，作者 `pks-gitlab`（Patrick Steinhardt）。
- 合并 commit：`af1e2fa3d`（`main`），合并日期约为 2026-05-06。
- 关键前驱：PR #7102（refStorage 扩展机制）、PR #7113（reftables 准备工作）。
- 后续维护：**PR #7327 "reftable update"**（合并 commit `ddf3b5c85`，约 2026-07），包含：
  - `refdb/reftable: suppress deletions`（`59f75b27b`）
  - `refdb/reftable: adjust to split of stack and write options`（`4e24f0ffb`）
  - `deps/reftable: update to 0b75ef5168`（`9a8bdbda7`）——同步上游 git 的 reftable 库本体。
- 实现层涉及：
  - `deps/reftable/`：从上游 git 项目导入的 reftable 库本体。
  - `src/libgit2/refdb_reftable.c`：libgit2 内的 reftable 后端。
  - `src/libgit2/refs.h` 暴露 `git_reference__is_per_worktree_ref()`。

### 1.2 发布版本可用性

| 版本 | 是否包含 reftable |
| --- | --- |
| `v1.9.0` | 否 |
| `v1.9.3`（本项目原 vendor 锁定） | 否 |
| `v1.9.4`（最新发布） | **否** |
| `main` `32b564e63`（第一轮 vendor） | 是（PR #7117） |
| `main` `ddf3b5c85`（**当前 vendor**） | 是（PR #7117 + #7327 修复） |

> **重要**：reftable 目前仅存在于未发布的 main 分支，所有 1.9.x 发布版均不含。任何依赖 reftable 的工程都必须自行构建 main，并接受其 API 与 ABI 的不稳定性。

### 1.3 完整的公共 API 面

reftable 的公开 API 分布在三个头文件。**注意**：初版报告曾称"refdb.h 无专用公开函数"，这在梳理 `sys/refdb_backend.h` 后已修正——存在专用的 `git_refdb_backend_reftable()` 构造函数。

**A. `include/git2/refdb.h`**

```c
typedef enum {
    GIT_REFDB_FILES    = 1, /**< loose + packed refs */
    GIT_REFDB_REFTABLE = 2  /**< reftable backend */
} git_refdb_t;

GIT_EXTERN(int)  git_refdb_new(git_refdb **out, git_repository *repo);      /* 空 refdb，需手动 set_backend */
GIT_EXTERN(int)  git_refdb_open(git_refdb **out, git_repository *repo);     /* 带默认后端，开箱即用 */
GIT_EXTERN(int)  git_refdb_compress(git_refdb *refdb);                      /* 压缩/优化：files=pack, reftable=compact stack */
GIT_EXTERN(void) git_refdb_free(git_refdb *refdb);
```

- 枚举由仓库配置 `extensions.refStorage` 驱动（`files` / `reftable`）。

**B. `include/git2/sys/refdb_backend.h`**（后端级 API）

```c
GIT_EXTERN(int) git_refdb_init_backend(git_refdb_backend *backend, unsigned int version);
GIT_EXTERN(int) git_refdb_backend_fs(git_refdb_backend **out, git_repository *repo);        /* files 后端构造 */
GIT_EXTERN(int) git_refdb_backend_reftable(git_refdb_backend **out, git_repository *repo);  /* reftable 后端构造 ← 专用 */
GIT_EXTERN(int) git_refdb_set_backend(git_refdb *refdb, git_refdb_backend *backend);
/* 自定义后端标志：GIT_REFDB_BACKEND_INIT_IS_WORKTREE / _FORCE_HEAD */
```

**C. `include/git2/repository.h`**

```c
GIT_EXTERN(int) git_repository_refdb(git_refdb **out, git_repository *repo); /* 取仓库当前 refdb */
/* git_repository_init_options 末尾新增字段： */
git_refdb_t refdb_type;  /* 0 = libgit2 默认（当前为 files） */
/* git_repository_set_refdb（在 sys/repository.h） */
```

`GIT_REPOSITORY_INIT_OPTIONS_VERSION` 保持为 `1`，未升版。

**D. `include/git2/common.h` —— 无 feature/opt 探测入口**

- **没有** `GIT_FEATURE_REFTABLE`。`git_feature_t` 枚举最大值仍为 `GIT_FEATURE_HTTP = (1 << 12)`。
- `git_libgit2_opt_t` 中也没有 reftable 相关选项。
- 这意味着：**特性检测不能走 `git_libgit2_features()`**，需通过"尝试 init reftable 仓库"或"读取 `extensions.refStorage` 配置"来探测（本项目已实现，见 §3.5）。

### 1.4 行为约束

- reftable 仓库会在创建时写入 `extensions.refStorage = reftable` 配置项，并以 reftable 物理布局存储引用。
- 对调用方而言，绝大多数引用 API（`git_reference_*`、`git_branch_*` 等）与 files 后端语义一致；性能特征与 GC 语义不同。
- 工作树相关引用（per-worktree）的语义在 master 上有相应内部 API（`git_reference__is_per_worktree_ref`），对外行为保持兼容。

---

## 2. 本项目（git2go fork）现状

### 2.1 vendor 与构建

- 原 `vendor/libgit2` HEAD：`f7164261c`（v1.9.x backport-v19-4），**不含 reftable**。
- 本次适配已将 `vendor/libgit2` 切换到 `32b564e63`（main HEAD，含 PR #7117），见 git 子模块状态。
- 构建文件（`Build_bundled_static.go` / `Build_system_static.go` / `Build_system_dynamic.go`）的版本守卫均为：

  ```c
  #if LIBGIT2_VER_MAJOR != 1 || LIBGIT2_VER_MINOR < 9 || LIBGIT2_VER_MINOR > 9
  # error "Invalid libgit2 version; this git2go supports libgit2 between v1.9.0 and v1.9.x"
  #endif
  ```

  **master HEAD 的 `version.h` 仍标注 `1.9.0`**，因此该守卫无需修改即可兼容含 reftable 的 master。

### 2.2 适配前的 Go 绑定缺口

| 维度 | 适配前状态 |
| --- | --- |
| `git_repository_init_ext` | **未绑定**。`InitRepository` 仅调用 `git_repository_init`，无法传 flags/mode/template/initialHead/originURL。 |
| `git_repository_init_options` | **未绑定**。 |
| `git_refdb_t` 枚举 | **未绑定**。 |
| `git_feature_t` reftable | 上游本就未提供，不算缺口。 |
| `extensions.refStorage` 配置探测 | 可通过现有 `Config` API 读写，无需新绑定。 |

### 2.3 历史已绑定且本任务复用的部分

- `refdb.go`：`Refdb` / `RefdbBackend` 通用后端类型 + `NewRefdb` / `SetBackend`（已可用）。
- `repository.go`：`SetRefdb`（已有）、`RepositoryItem` 枚举（已有）。
- `Config`：可读写 `extensions.refStorage`，是 reftable 仓库的判定依据。

---

## 3. 本轮已落地的适配

> 本轮交付范围按用户确认：vendor 升级 + 完整 RepositoryInitOptions 绑定 + 调研报告。reftable 编译/冒烟测试推迟到后续阶段。

### 3.1 vendor 升级

- `vendor/libgit2` 子模块从 `f7164261c`（无 reftable）切换到 `32b564e63`（main HEAD，含 PR #7117）。
- 构建版本守卫保留 `1.9.x`，因 master HEAD 的 `version.h` 仍标 `1.9.0`，**未修改 Build_\*.go**。

### 3.2 Go 绑定新增

#### 3.2.1 `refdb.go`

新增 `RefdbType` 类型与三个常量：

```go
const (
    RefdbDefault  RefdbType = 0 // 让 libgit2 选默认
    RefdbFiles    RefdbType = 1 // GIT_REFDB_FILES
    RefdbReftable RefdbType = 2 // GIT_REFDB_REFTABLE
)
```

实现注意：常量使用 Go 端字面值而非 `C.GIT_REFDB_FILES`，原因是若未来需要为 v1.9.x 用户做兼容性回退（vendor 仍是无 reftable 版本），这些常量自身不会因 C 头缺失而编译失败。

#### 3.2.2 `repository.go`

- 新增 `RepositoryInitFlag`（位掩码）：`Bare / NoReinit / Mkdir / Mkpath / ExternalTemplate / RelativeGitlink`。
- 新增 `RepositoryInitMode`：`SharedUmask / SharedGroup / SharedAll`，亦可传入自定义八进制值。
- 新增 `RepositoryInitOptions` 结构体（Flags/Mode/WorkdirPath/Description/TemplatePath/InitialHead/OriginURL/**RefdbType**）。
- 新增 `InitRepositoryExt(path string, opts *RepositoryInitOptions) (*Repository, error)`：基于 `git_repository_init_ext`。
- 保留原有 `InitRepository(path, isbare)` 接口不变，向后兼容。

### 3.3 本地验证结果

本机已用新 vendor 重建 `./static-build/install/libgit2.a` 并通过以下验证：

| 验证项 | 结果 |
| --- | --- |
| `go build -tags static ./...` | PASS |
| 核心冒烟测试（blob / commit / repository / config 等） | PASS |
| **`TestInitRepositoryExtFiles`**（bare + initial_head + description 透传） | **PASS** |
| **`TestInitRepositoryExtNilOptions`**（nil 等价默认） | **PASS** |
| **`TestInitRepositoryExtReftable`**（reftable 后端实际可用） | **PASS** |

`TestInitRepositoryExtReftable` 验证了：

- 仓库目录下生成 `reftable/` 子目录（含 `*.ref` 表文件 + `tables.list`），而非传统 `refs/heads/...` 布局。
- `extensions.refStorage` 配置被 libgit2 自动写为 `reftable`。
- `NewReferenceIterator()` 在 reftable 后端上正常运行。

构建参数（不同于历史 `script/build-libgit2.sh --static`）：

- `USE_AUTH_NTLM=OFF`：master 上 `deps/ntlmclient/CMakeLists.txt` 在 `USE_HTTPS=OFF` 时强制报错，需关闭。
- `USE_AUTH_NEGOTIATE=OFF`：避免 GSS.framework 静态链接缺失符号。
- `DEPRECATE_HARD=OFF`：保留 `git_odb_hash`（git2go `odb.go:266` 仍在使用）。

### 3.4 vendor 升级暴露的独立问题（非本任务范围）

| 问题 | 现象 | 建议 |
| --- | --- | --- |
| `TestApplyDiffAddfile` SIGBUS | `git_apply` 在 cgo 调用中崩溃 | 独立排查 master 上 `git_apply` 的 ABI / 行为变化 |
| 构建脚本 `script/build-libgit2.sh` 默认参数与 master 不兼容 | NTLM/HTTPS/Deprecate-Hard 组合冲突 | 后续 PR 中更新脚本默认参数或新增 `--master` 模式 |
| `script/build-libgit2.sh` 默认 `DEPRECATE_HARD=ON` 与 git2go 使用 `git_odb_hash` 冲突 | 链接缺失 `_git_odb_hash` | 把 `git_odb_hash` 替换为 `git_odb_hash_object`，或脚本改用 `DEPRECATE_HARD=OFF` |

### 3.5 第二轮（07-29）：完整 refdb/reftable API 绑定

vendor 进一步升级到最新 main `ddf3b5c85`（含 reftable 修复 PR #7327）。补齐了此前遗漏的**全部** refdb/reftable 公开 API，使 git2go 对 reftable 的支持从"仅能 init"提升到"完整生命周期可控"。

#### 新增绑定（均在 `refdb.go`）

| Go API | 包装的 C 函数 | 用途 |
| --- | --- | --- |
| `(*Repository).Refdb()` | `git_repository_refdb` | 获取仓库当前 refdb |
| `(*Repository).OpenRefdb()` | `git_refdb_open` | 创建带默认后端、开箱即用的 refdb |
| `(*Refdb).Compress()` | `git_refdb_compress` | files=pack refs；**reftable=compact stack** |
| `(*Repository).NewRefdbBackendFs()` | `git_refdb_backend_fs` | 显式构造 files 后端 |
| `(*Repository).NewRefdbBackendReftable()` | `git_refdb_backend_reftable` | 显式构造 **reftable 后端** |

#### 逻辑兼容层助手（value-add，非直接 1:1 包装）

| Go API | 实现方式 | 用途 |
| --- | --- | --- |
| `(RefdbType).String()` | 纯 Go | 输出规范 config token（`files`/`reftable`），与 git/libgit2 一致 |
| `(*Repository).RefStorageFormat()` | 读 `extensions.refStorage` config | 运行时识别当前仓库用的是 files 还是 reftable，无配置默认 files |
| `IsReftableSupported()` | 临时目录试 init reftable 仓库 | 运行时探测本 libgit2 构建是否支持 reftable（弥补无 `GIT_FEATURE_REFTABLE`） |

#### 第二轮测试（`refdb_reftable_test.go`，11 个）

覆盖 files / reftable 双后端，均 PASS：

| 测试 | 验证点 |
| --- | --- |
| `TestIsReftableSupported` | 运行时探测稳定返回 true |
| `TestRefdbTypeString` | 枚举 → config token 映射 |
| `TestRefStorageFormatFiles` / `...Reftable` | 双后端格式识别 |
| `TestRepositoryRefdb` | 双后端取 refdb |
| `TestOpenRefdb` | 开箱即用 refdb |
| `TestRefdbCompressFiles` / `...Reftable` | 双后端 compaction（**reftable stack 压缩通过**） |
| `TestNewRefdbBackendFs` / `...Reftable` | 双后端显式构造 |
| **`TestReftableBranchLifecycle`** | **reftable 仓库上完整分支 CRUD**（seed commit → CreateBranch → LookupBranch → Target 校验 → Delete → 确认删除） |

> `TestReftableBranchLifecycle` 是本轮最有价值的验证：它证明 git2go 现有的高层引用 API（`CreateCommit` / `CreateBranch` / `LookupBranch` / `Branch.Delete`）在 reftable 后端上**行为完全兼容**，无需任何针对性改造——这正是 reftable 作为"透明后端"的设计目标。

---

## 4. API 差异对比表（上游 master vs 本项目绑定）

| C API / 字段 | 上游 main | 本项目绑定 | 状态 |
| --- | --- | --- | --- |
| `git_repository_init` | 存在 | `InitRepository` | ✅ 原有（保留） |
| `git_repository_init_ext` | 存在 | `InitRepositoryExt` | ✅ 第一轮 |
| `git_repository_init_options` | 完整结构体 | `RepositoryInitOptions` | ✅ 第一轮（含 RefdbType） |
| `git_repository_init_flag_t` | 6 个 flag | `RepositoryInitFlag` | ✅ 第一轮 |
| `git_repository_init_mode_t` | 3 个 mode | `RepositoryInitMode` | ✅ 第一轮 |
| `git_refdb_t` | 2 个枚举值 | `RefdbType` | ✅ 第一轮 |
| `refdb_type` 字段 | 新增 | `RepositoryInitOptions.RefdbType` | ✅ 第一轮 |
| `git_refdb_new` | 存在 | `NewRefdb` | ✅ 原有 |
| `git_refdb_set_backend` | 存在 | `Refdb.SetBackend` | ✅ 原有 |
| `git_refdb_free` | 存在 | `Refdb.Free` | ✅ 原有 |
| `git_repository_set_refdb` | 存在 | `Repository.SetRefdb` | ✅ 原有 |
| `git_repository_refdb` | 存在 | `Repository.Refdb` | ✅ **第二轮** |
| `git_refdb_open` | 存在 | `Repository.OpenRefdb` | ✅ **第二轮** |
| `git_refdb_compress` | 存在 | `Refdb.Compress` | ✅ **第二轮** |
| `git_refdb_backend_fs` | 存在 | `Repository.NewRefdbBackendFs` | ✅ **第二轮** |
| `git_refdb_backend_reftable` | 存在 | `Repository.NewRefdbBackendReftable` | ✅ **第二轮** |
| `extensions.refStorage` 探测 | 由 libgit2 写/读 | `Repository.RefStorageFormat` | ✅ **第二轮**（助手） |
| reftable 运行时探测 | 无 feature flag | `IsReftableSupported` | ✅ **第二轮**（助手） |
| `git_refdb_init_backend` | 存在 | 未绑定 | ⬜ 仅自定义后端场景需要 |
| `GIT_REFDB_BACKEND_INIT_IS_WORKTREE/FORCE_HEAD` | 存在 | 未绑定 | ⬜ 仅自定义后端场景需要 |
| `GIT_FEATURE_REFTABLE` | **不存在** | —（用 `IsReftableSupported` 替代） | 上游未提供 |
| `git_libgit2_opt_t` reftable 项 | **不存在** | — | 上游未提供 |
| `git_reference__is_per_worktree_ref` | 内部函数 | 不绑定 | 非公开 API |
| `refdb_reftable.c` 后端 | 内部实现 | 不直接绑定 | 调用方对其透明 |

**结论**：所有 reftable/refdb **公开** API（`refdb.h` + `sys/refdb_backend.h` 中的 `GIT_EXTERN` 函数、`repository.h` 相关项）均已绑定。仅剩两项 `git_refdb_init_backend` 与 backend init flags 未绑定，它们仅在实现**自定义后端**（用 Go 实现一个 refdb backend）时才需要，不属于"使用 reftable"场景，列入长期 roadmap。

---

## 5. 后续 Roadmap

### 5.1 已完成（截至第二轮 07-29）

- ✅ vendor 升级到含 reftable 的 main（`ddf3b5c85`）并本地重建、编译通过。
- ✅ 完整 `RepositoryInitOptions` + `InitRepositoryExt`（含 `refdb_type`）。
- ✅ 全部 refdb/reftable 公开 API 绑定（后端构造、compaction、refdb 获取/打开）。
- ✅ 特性探测助手 `IsReftableSupported()`、后端识别 `RefStorageFormat()`。
- ✅ files/reftable 双后端测试矩阵（14 个测试，含分支完整 CRUD、stack compaction）。

### 5.2 短期（下一个 PR 周期）

1. **修复 vendor 升级暴露的独立回归**
   - `TestApplyDiffAddfile` SIGBUS：排查 main 上 `git_apply` 的 ABI/行为变化（见 §3.4）。
   - 更新 `script/build-libgit2.sh`：加入 `USE_AUTH_NTLM=OFF` / `USE_AUTH_NEGOTIATE=OFF` / `DEPRECATE_HARD=OFF`，或新增 `--master` 模式，使脚本能直接构建当前 vendor。
   - 已知历史问题 `TestConfigLookups` / `TestConfigEntryBackendType` 共享 `./temp.gitconfig` 的并发竞争，独立处理。

2. **CI 矩阵**
   - 在 CI 上跑两种 vendor：发布版 `v1.9.x`（reftable 测试自动 skip）与 main HEAD（跑全部 reftable 测试）。
   - 现有测试已通过 `t.Skipf` 在无 reftable 构建上优雅跳过，天然支持双轨。

3. **文档与 README**
   - README 新增"Reference Storage Backends"小节，明确 `RefdbReftable` 的可用前提与风险。
   - `RepositoryInitOptions` / `NewRefdbBackendReftable` GoDoc 补充用例。

### 5.3 中期

1. **双轨编译兼容（v1.9.x + main）**
   - 当前 `refdb.go` 直接引用 `C.git_refdb_backend_reftable` 等 main-only 符号；在 v1.9.x vendor 上会编译失败。
   - 用 build tag（如 `//go:build libgit2_reftable`）拆分 main-only 绑定，让稳定版用户仍可编译核心功能。

2. **版本守卫策略**
   - libgit2 主线 bump 到 1.10 / 2.0 时同步 `Build_*.go` 版本范围。
   - 考虑运行时 `git_libgit2_version()` 兜底（vs 编译期 `#if`）。

### 5.4 长期

1. **自定义 refdb 后端**：绑定 `git_refdb_init_backend` + `GIT_REFDB_BACKEND_INIT_*` flags，支持用 Go 实现 refdb backend（目前仅缺此两项公开 API）。
2. **完整反向探测能力**：若上游补充 `GIT_FEATURE_REFTABLE` / `git_libgit2_opts` 选项，映射到 `Features()`。
3. **per-worktree 引用语义**：若上游公开 `git_reference__is_per_worktree_ref` 或等价 API，补绑定。
4. **SHA-256 与 reftable 组合**：reftable 是 git SHA-256 转型的关键依赖。等 `GIT_EXPERIMENTAL_SHA256` 稳定后，验证 `RefdbReftable` + Sha256 `oid_type` 组合。

---

## 6. 风险与权衡

| 风险 | 缓解措施 |
| --- | --- |
| main 不稳定，API 可能再变 | vendor pin 到具体 commit `ddf3b5c85`，不随 main 自动滚动；遇上游 break 时同步更新本项目。PR #7327 已证明 reftable API 仍在演进（stack/write options 拆分），需持续跟进。 |
| 用户原先使用 v1.9.x 系统库 | `RepositoryInitOptions.RefdbType=0`（默认）行为与旧 `InitRepository` 等价；不主动启用 reftable。 |
| 未启用 reftable 时的开销 | 零运行时开销：仅多了一个 init options 字段，C 端为 0 时走默认分支。 |
| 文档/用户期望错配 | 在 GoDoc 与本文档明确：reftable 需 master 构建，发布版不可用。 |
| 编译失败（v1.9.x vendor 用户） | 本轮代码在 `repository.go` 中直接引用了 `C.git_refdb_t` 与 `copts.refdb_type`，对 v1.9.x vendor 不兼容。如需双轨支持，可在后续以 build tag（`//go:build libgit2_master`）拆出 InitRepositoryExt 的 refdb_type 赋值段。 |

---

## 7. 参考链接

- libgit2 PR #7117（Reftables support）: <https://github.com/libgit2/libgit2/pull/7117>
- libgit2 PR #7102（extensions.refStorage prerequisite）: <https://github.com/libgit2/libgit2/pull/7102>
- libgit2 `include/git2/refdb.h`（master）: <https://github.com/libgit2/libgit2/blob/main/include/git2/refdb.h>
- libgit2 `include/git2/repository.h`（master）: <https://github.com/libgit2/libgit2/blob/main/include/git2/repository.h>
- Git reftable 设计文档: <https://git-scm.com/docs/reftable>
