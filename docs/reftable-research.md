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

### 3.4 vendor 升级暴露的独立问题（非 reftable 绑定本身）

| 问题 | 现象 | 状态 |
| --- | --- | --- |
| xdiff 崩溃（10 个用例，见 §3.7） | 所有基于 xdiff 的 diff/patch/blame/merge/rebase 在 cgo 中 SIGBUS | 已定性为本地 libgit2 构建/工具链 bug（纯 C 亦崩、跨版本/后端/优化一致），与 git2go 无关，见 §3.6 |
| `script/build-libgit2.sh` 与 main 不兼容（NTLM） | `USE_HTTPS=OFF` 时 ntlmclient CMake 报错 | ✅ 已修复（脚本加 `USE_AUTH_NTLM=OFF`） |
| GSS.framework 静态链接缺符号 | `Undefined symbols _gss_*` | ✅ 已修复（脚本加 `USE_AUTH_NEGOTIATE=OFF`） |
| `DEPRECATE_HARD=ON` 移除 `git_odb_hash` | 链接缺失 `_git_odb_hash` | ✅ 已修复（脚本 bundled 构建默认 `DEPRECATE_HARD=OFF`） |

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

### 3.6 xdiff SIGBUS 深入调研（结论：本地 libgit2 构建/工具链 bug，与 git2go 及 reftable 无关）

现象：任何经 xdiff 的操作在本机 SIGBUS，共影响 10 个测试用例（完整清单见 §3.7），涉及 `git_apply` / `git_blame_file` / `git_diff_*` / `git_patch_from_diff` / `git_merge_file` / `git_rebase_next`。经**多轮真实构建对照**，最终定性为本地 libgit2 构建/工具链问题，**与 git2go 代码、cgo、reftable、libgit2 版本均无关**。

**调研过程与证据（按时间顺序，含一次自我订正）**

1. **隔离复现**：单独跑 `TestApplyDiffAddfile` 仍崩 → 排除并发竞争。
2. **范围界定**：纯 diff（`TestDiffTreeToTree` / `TestDiffBlobs`，不经 apply）同样崩 → 不是 `git_apply` 特有，是整个 xdiff 机制全崩。
3. **原生回溯**（lldb, -O2）：`git_apply/git_diff_blobs` → `git_xdiff` → `xdl_diff` → `xdl_do_diff` → `xdl_prepare_env`（-O0 下进一步到 `xdl_optimize_ctxs` → `xdl_cleanup_records`），崩在一个损坏的函数指针 / 被当作代码执行的数据（`EXC_BAD_INSTRUCTION`，subcode 是 ASCII 文本）。
4. **排除 git2go/cgo 层**：`git_apply` 调用时 `opts=NULL`，git2go 未传任何结构体；未注册自定义 allocator。
5. **排除 regex 后端**：用 `-DREGEX_BACKEND=regcomp`（替代 builtin/pcre2）重建 main，diff **仍崩**。
6. **排除编译优化**：用 `-O0`/Debug 重建 main，diff **仍崩**。
7. **排除 xdiff 源码改动**：`v1.9.4..ddf3b5c85` 之间 `deps/xdiff` 只有 2 个 commit（`xmerge.c` malloc-0 修复 + cmake 头整理），均不涉及崩溃路径 `xprepare.c`。
8. **订正关键假设**：此前"v1.9.4 正常"是**未验证的假设**。实际用本机构建的 **v1.9.4** 库跑 diff 测试——**同样 SIGBUS**。→ 推翻"main bump 引入"，问题与 libgit2 版本无关。
9. **排除 cmake 定制参数**：用接近默认的参数（不加 `USE_*=OFF` 等）构建 v1.9.4，diff **仍崩**。
10. **决定性纯 C 复现**：写一个链接 `libgit2.a`、完全不含 Go/cgo 的 C 程序调用 `git_diff_buffers` → **SIGBUS（exit 138 = 128+10）**。

**定性结论**

纯 C 都崩，且跨 libgit2 版本、跨 regex 后端、跨优化级别一致复现 → 这是 **libgit2 的 xdiff 在本机环境（macOS arm64 + 当前 clang 工具链）经 CMake 构建后的运行时 bug**，属 libgit2 / 工具链层面，**与 git2go 绑定代码完全无关，也不影响 reftable 功能**（reftable 全部测试通过；reftable 不经 xdiff）。

**影响与后续**

- 影响面：仅 diff/patch/blame 等走 xdiff 的路径；reftable、引用、提交、config 等均不受影响。
- 后续（libgit2/环境侧，非 git2go）：在其它工具链/平台（如 CI 的 Linux）复核是否复现；若可复现则向上游 libgit2 报 issue（附纯 C 复现与本节回溯）；本机可尝试更换 clang 版本 / 关闭特定优化再排查。
- git2go 侧无需改动。

### 3.7 全量回归基线（2026-08-03）

在 vendor = main `ddf3b5c85`、`-tags "static libgit2_reftable"` 下做了一次完整全量回归。由于 xdiff 崩溃会终止整个 test binary，采用「逐轮排除崩溃点」的方式定位全部受影响用例，最终得到干净基线。

**基线结果**

```sh
go test -tags "static libgit2_reftable" -count=1 -p 1 \
  -skip "<下表 10 个 xdiff 用例>|TestRebaseAbort$|TestRebaseNoConflicts$|TestRebaseGpgSigned$" ./...
# → EXIT=0    PASS: 135    FAIL: 0    SKIP: 0
```

**135 个用例全绿**，涵盖本次新增的全部绑定（reftable、refdb、自定义 refdb backend 桥接、reflog、版本 API）以及既有功能（引用、分支、提交、tag、remote、note、config、tree、index 等）。

**被排除用例的分类（均为 pre-existing，与本次工作无关）**

| 类别 | 用例 | 原因 |
| --- | --- | --- |
| xdiff SIGBUS（10 个） | `TestApplyDiffAddfile`、`TestApplyToTree`、`TestRebaseInMemoryWithConflict`、`TestBlame`、`TestDiffBlobs`、`TestPatch`、`TestDiffTreeToTree`、`TestFindSimilar`、`TestMergeSameFile`、`TestMergeTreesWithoutAncestor` | §3.6 的本地 libgit2/工具链 bug。全部崩在 xdiff 相关 C 调用（`git_apply` / `git_blame_file` / `git_diff_*` / `git_patch_from_diff` / `git_merge_file` / `git_rebase_next`），纯 C 亦可复现 |
| 环境差异（3 个） | `TestRebaseAbort`、`TestRebaseNoConflicts`、`TestRebaseGpgSigned` | 报错 `cannot locate local branch 'master'`。本机 `git config --global init.defaultBranch = main`，新建仓库实际为 `refs/heads/main`，而 `rebase_test.go` 硬编码查找 `master` |

**另需注意（历史问题）**

- `TestConfigLookups` 与 `TestConfigEntryBackendType` 均调用 `t.Parallel()` 且共享同一路径 `./temp.gitconfig`，并行写会产生 `temp.gitconfig.lock` 冲突。用 `-p 1` 串行即可规避；此外若此前有测试进程崩溃（如上述 xdiff SIGBUS），会遗留 `temp.gitconfig.lock` 导致后续跑测试误报失败，需先 `rm -f temp.gitconfig.lock`。

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

1. **构建脚本适配 main** ✅ 已完成
   - `script/build-libgit2.sh` 已加入 `USE_AUTH_NTLM=OFF` / `USE_AUTH_NEGOTIATE=OFF`，bundled 构建默认 `DEPRECATE_HARD=OFF`（可用环境变量覆盖）。现可直接构建当前 vendor。

2. **CI 矩阵** ✅ 已完成
   - `.github/workflows/ci.yml` 新增 `build-reftable` job：构建 bundled main（含 reftable）并跑 reftable/refdb 测试子集，含 reftable 仓库创建与分支生命周期专项验证。
   - 稳定版（system build）上 reftable 测试通过 `t.Skipf` 优雅跳过，形成 stable/main 双轨。

3. **xdiff SIGBUS**（见 §3.6，已定性、与 reftable/git2go 无关）
   - 结论：本地 libgit2 构建/工具链 bug（纯 C 复现、跨 libgit2 版本/regex 后端/优化级别一致崩溃）。git2go 侧无需改动。
   - 后续（环境/上游侧）：在其它工具链/平台复核，可复现则报 libgit2 上游。
   - 历史问题 `TestConfigLookups` / `TestConfigEntryBackendType` 共享 `./temp.gitconfig` 的并发竞争，独立处理（`-p 1` 可规避）。

4. **文档与 README** ✅ 已完成
   - README 新增 "Reference storage backends (reftable)" 小节：可用前提/风险、`IsReftableSupported` / `RefStorageFormat` 探测、`InitRepositoryExt` + `RefdbReftable` 初始化、`Refdb.Compress` 用例。
   - `InitRepositoryExt` / `NewRefdbBackendReftable` GoDoc 补充可运行风格的用例注释。

### 5.3 中期

1. **双轨编译兼容（v1.9.x + main）** ✅ 已完成
   - 通过 `libgit2_reftable` build tag 隔离所有 main-only 绑定：
     - `reftable_on.go`（`//go:build libgit2_reftable`）：`applyRefdbType` 真实现（写 `copts.refdb_type`）、`NewRefdbBackendReftable`（`git_refdb_backend_reftable`）、`IsReftableSupported`（临时 init 探测）。
     - `reftable_off.go`（`//go:build !libgit2_reftable`）：同名 API 的 stub——`applyRefdbType` 对非默认后端返回错误、`NewRefdbBackendReftable` 返回"需加 tag"错误、`IsReftableSupported` 返回 false。**不引用任何 main-only C 符号**。
   - 核心文件（`refdb.go` / `repository.go`）只保留 v1.9.x 也存在的符号：`git_refdb_open` / `git_refdb_compress` / `git_repository_refdb` / `git_refdb_backend_fs` 均在 v1.9.4 存在，故 `OpenRefdb` / `Compress` / `Refdb` / `NewRefdbBackendFs` 无需 tag。`RefdbType` 枚举是纯 Go 常量（数值对应 `git_refdb_t`，不引用 C 符号），也留在核心。
   - `InitRepositoryExt` 通过 `applyRefdbType(&copts, opts.RefdbType)` 间接层赋值，两个 tag 版本各一份实现。
   - 验证：
     - **不带 tag**（模拟 v1.9.x 用户）`go build -tags static ./...` 通过；reftable 测试优雅 SKIP，files 测试 PASS。
     - **带 tag** `go build -tags "static libgit2_reftable"` 通过；reftable 测试全部 PASS。
   - 默认构建：`Makefile` 的 `STATIC_TAGS = static libgit2_reftable`（vendored 是 main），默认启用 reftable；可用 `make ... REFTABLE_TAG=` 关闭。
   - CI：`build-reftable`（tag on，跑全部 reftable 测试）+ `build-reftable-disabled`（tag off，验证 stable 子集可编译且 reftable 测试跳过）双 job。

2. **版本守卫策略** ✅ 已完成
   - **编译期守卫集中化**：三个 `Build_*.go` 里重复的 `#if LIBGIT2_VER_...` 抽到单一头文件 `git2go_version_check.h`（三处均 `#include`）。守卫从"锁死 minor==9"改为"**仅下界** ≥ 1.9，无上界"——libgit2 bump 到 1.10 / 2.0 时**不再编译失败**，仍拦截过旧版本。提升最低支持版本现在是该头里的一行改动。
   - **运行时兜底**：`features.go` 新增 `Version() (major, minor, patch int)`（`git_libgit2_version`）、`Prerelease() string`（`git_libgit2_prerelease`）、`VersionString() string`，用于运行时做版本相关决策。
   - 注：当前 vendored main 的 `version.h` 仍标 `1.9.0` 且未设 prerelease，故 `Prerelease()` 返回空——运行时**可靠**区分 reftable 能力仍应用 `IsReftableSupported()`；`Version`/`Prerelease` 忠实反映 libgit2 上报值，供通用版本判断。
   - 测试：`features_test.go` 校验 `Version()` 满足 ≥1.9 下界、`VersionString()` 前缀与 prerelease 拼接正确。

### 5.4 长期

1. **自定义 refdb 后端** ✅ 已完成
   - ✅ `RefdbBackendInitFlag` 枚举（`RefdbBackendInitIsWorktree` / `RefdbBackendInitForceHead`），main-only，随 `libgit2_reftable` tag。
   - ✅ 完整回调桥接：`RefdbBackendInterface`（13 个方法：Exists/Lookup/Iterator/Write/Rename/Delete/HasLog/EnsureLog/Free/ReflogRead/Write/Rename/Delete）+ `RefdbBackendIterator`。`NewRefdbBackendFromInterface` 用 Go 实现 refdb backend，经 `Refdb.SetBackend` 挂载。
     - C 侧（`wrapper.c`）：`_go_managed_refdb_backend` 内嵌 `git_refdb_backend`+handle，13 个 trampoline 转 `//export` Go 回调，错误经 `set_callback_error` 传递；自定义 iterator 亦为内嵌 `git_reference_iterator` 的托管结构。
     - 生命周期：`pointerHandles` 锚定 Go 实现，`free` 回调触发 Untrack；引用/reflog 对象在回调内转移所有权给 libgit2（`SetFinalizer(nil)`）。
     - `Reflog` 类型（`reflog.go`）承载 reflog 回调句柄，并已扩展为**完整的 reflog entry 读写 API**（见下）。
     - 位于 `refdb_backend.go`（无 build tag，v1.9.x 也可编译——`git_refdb_backend` 结构与 `git_refdb_init_backend` 在 v1.9.4 存在）。
   - 测试：`refdb_backend_test.go` 的 `TestRefdbBackendBridge` 验证 Go 实现 attach 后，libgit2 lookup 路由进 Go 回调、`free` 回调触发、无 cgo handle 泄漏。

2. **完整 Reflog entry 读写 API** ✅ 已完成
   - `reflog.go` 绑定 `git2/reflog.h` 全部 13 个公开函数：
     - 仓库级：`Repository.ReadReflog` / `RenameReflog` / `DeleteReflog`。
     - Reflog 级：`Write` / `Append` / `EntryCount` / `EntryByIndex` / `Drop`。
     - `ReflogEntry` 访问器：`IdOld` / `IdNew` / `Committer` / `Message`。
   - 无 build tag（reflog 在 v1.9.x 也存在），复用现有 `Signature.toC` / `newSignatureFromC` / `newOidFromC` / `Oid.toC` / `cbool`。
   - 测试：`reflog_test.go` 三个用例覆盖 read+entries、append+write+drop+持久化、rename+delete 完整生命周期。

3. **完整反向探测能力**（`GIT_FEATURE_REFTABLE`）：⏸️ **已调研，上游仍未提供** —— 上游 main `d29fe50de` 的 `git_feature_t` 最大值仍为 `GIT_FEATURE_HTTP`。现有三层探测（build tag + `IsReftableSupported()` + `RefStorageFormat()`）已是最优方案，无需改动。详见 [reftable-longterm-research.md §1](./reftable-longterm-research.md)。
4. **per-worktree 引用语义**：⏸️ **已调研，仍为内部 API** —— `git_reference__is_per_worktree_ref` 位于 `src/libgit2/refs.h`（非公开），且上游正收紧符号可见性，不应绑定。可选替代：Go 侧按 git 约定自行实现。详见 [reftable-longterm-research.md §2](./reftable-longterm-research.md)。
5. **SHA-256 与 reftable 组合**：✅ **上游已解锁**，但 ⚠️ **触发 git2go `Oid` ABI 破坏性变更** —— 上游 PR #7261 已将 SHA256 转正（`GIT_EXPERIMENTAL_SHA256` 从公开头文件移除），`git_oid` 由 20 字节变为 33 字节（新增 `type` 字段 + `id[32]`），而 git2go 的 `Oid [20]byte` 直接 `unsafe.Pointer` 强转为 `*C.git_oid` 会**越界破坏内存**。reftable 后端已原生支持 `REFTABLE_HASH_SHA256`。落地需分三阶段（编译期尺寸守卫 → `Oid` 重构 → 组合测试矩阵）。详见 [reftable-longterm-research.md §3](./reftable-longterm-research.md)。

> ⚠️ **重要风险提示**：当前 vendor（`ddf3b5c85`）尚未转正 SHA256，`git_oid` 恰为 20 字节，故一切正常。但**任何将 vendor 升级到最新 main 的操作都会触发上述内存损坏问题**，升级前必须先落地 `Oid` 尺寸守卫。

---

## 6. 风险与权衡

| 风险 | 缓解措施 |
| --- | --- |
| main 不稳定，API 可能再变 | vendor pin 到具体 commit `ddf3b5c85`，不随 main 自动滚动；遇上游 break 时同步更新本项目。PR #7327 已证明 reftable API 仍在演进（stack/write options 拆分），需持续跟进。 |
| 用户原先使用 v1.9.x 系统库 | `RepositoryInitOptions.RefdbType=0`（默认）行为与旧 `InitRepository` 等价；不主动启用 reftable。 |
| 未启用 reftable 时的开销 | 零运行时开销：仅多了一个 init options 字段，C 端为 0 时走默认分支。 |
| 文档/用户期望错配 | 在 GoDoc 与本文档明确：reftable 需 master 构建，发布版不可用。 |
| 编译失败（v1.9.x vendor 用户） | ✅ 已解决：所有 main-only 绑定用 `libgit2_reftable` build tag 隔离（`reftable_on.go` / `reftable_off.go`）。不带 tag 时核心功能照常编译，reftable API 降级为返回错误 / `IsReftableSupported()=false`。见 §5.3。 |

---

## 7. 参考链接

- libgit2 PR #7117（Reftables support）: <https://github.com/libgit2/libgit2/pull/7117>
- libgit2 PR #7102（extensions.refStorage prerequisite）: <https://github.com/libgit2/libgit2/pull/7102>
- libgit2 `include/git2/refdb.h`（master）: <https://github.com/libgit2/libgit2/blob/main/include/git2/refdb.h>
- libgit2 `include/git2/repository.h`（master）: <https://github.com/libgit2/libgit2/blob/main/include/git2/repository.h>
- Git reftable 设计文档: <https://git-scm.com/docs/reftable>
