# Reftable 适配调研报告

> 目标：分析上游 libgit2 master 中 reftable 的支持现状，结合本项目（git2go fork）当前绑定情况，给出本轮兼容性适配的范围、落地内容，以及后续 roadmap。

调研日期：2026-06-30
本项目分支：`feat-reftable`
本项目模块：`cnb.cool/cnb/git2go/v35`

---

## 1. 上游 libgit2 master 的 reftable 现状

### 1.1 来源与时间线

- PR：[libgit2/libgit2#7117 "Reftables support"](https://github.com/libgit2/libgit2/pull/7117)，作者 `pks-gitlab`（Patrick Steinhardt）。
- 合并 commit：`af1e2fa3d`（`main`），合并日期约为 2026-05-06。
- 关键前驱：PR #7102（refStorage 扩展机制）、PR #7113（reftables 准备工作）。
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
| `main` HEAD（截至 `32b564e63`） | 是 |

> **重要**：reftable 目前仅存在于未发布的 master/main 分支，所有 1.9.x 发布版均不含。任何依赖 reftable 的工程都必须自行构建 master，并接受其 API 与 ABI 的不稳定性。

### 1.3 新增/变更的公共 API 面

1. `include/git2/refdb.h` 新增枚举：

   ```c
   typedef enum {
       GIT_REFDB_FILES    = 1, /**< Files backend using loose and packed refs. */
       GIT_REFDB_REFTABLE = 2  /**< Reftable backend. */
   } git_refdb_t;
   ```

   - 由仓库配置 `extensions.refStorage` 驱动（`files` / `reftable`）。
   - **refdb.h 本身没有新增 reftable 专用的公开函数**。reftable 是一个内部后端，对调用方完全透明。

2. `include/git2/repository.h` 的 `git_repository_init_options` 末尾新增字段：

   ```c
   git_refdb_t refdb_type;  /* 0 = libgit2 默认（当前为 files） */
   ```

   通过 `git_repository_init_ext()` 在初始化时指定。`GIT_REPOSITORY_INIT_OPTIONS_VERSION` 保持为 `1`，未升版。

3. `include/git2/common.h` 中：
   - **没有** `GIT_FEATURE_REFTABLE`。`git_feature_t` 枚举最大值仍为 `GIT_FEATURE_HTTP = (1 << 12)`。
   - `git_libgit2_opt_t` 中也没有 reftable 相关选项。
   - 这意味着：**特性检测不能走 `git_libgit2_features()`**，调用方需要通过尝试 init / 读取 `extensions.refStorage` 配置来探测。

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

---

## 4. API 差异对比表（上游 master vs 本项目绑定）

| C API / 字段 | 上游 master | 本项目绑定（适配后） | 备注 |
| --- | --- | --- | --- |
| `git_repository_init` | 存在 | `InitRepository` | 仅 path + isbare，保留兼容 |
| `git_repository_init_ext` | 存在 | **`InitRepositoryExt`（新增）** | 完整 options |
| `git_repository_init_options` | 完整结构体 | **`RepositoryInitOptions`（新增）** | 含 RefdbType |
| `git_repository_init_flag_t` | 6 个 flag | **`RepositoryInitFlag`（新增）** | 全部映射 |
| `git_repository_init_mode_t` | 3 个 mode | **`RepositoryInitMode`（新增）** | 全部映射 |
| `git_refdb_t` | 2 个枚举值 | **`RefdbType`（新增）** | RefdbFiles / RefdbReftable |
| `refdb_type` 字段 | 新增 | `RepositoryInitOptions.RefdbType` | 直接透传 |
| `GIT_FEATURE_REFTABLE` | **不存在** | 无 | 不可通过 Features() 探测 |
| `git_libgit2_opt_t` reftable 项 | **不存在** | 无 | 不可通过 opts 控制 |
| `extensions.refStorage` 配置 | 由 libgit2 写/读 | 通过现有 `Config` API 可访问 | 见 §5 探测方案 |
| `git_reference__is_per_worktree_ref` | 内部函数 | 不绑定 | 非公开 API |
| `refdb_reftable.c` 后端 | 内部实现 | 不直接绑定 | 调用方对其透明 |

---

## 5. 后续 Roadmap

### 5.1 短期（下一个 PR 周期）

1. **本地/CI 重建并通过测试**
   - 执行 `script/build-libgit2.sh --static`（或 `--dynamic`）使用新 vendor 重建 libgit2。
   - 跑 `go test ./...` 验证现有用例不回归。
   - 已知不相关的 `TestConfigLookups` / `TestConfigEntryBackendType` 共享 `./temp.gitconfig` 的并发竞争问题（历史问题）独立处理。

2. **reftable 冒烟测试（本轮已落地基础三项，扩展项待办）**

   已落地（见 `repository_init_ext_test.go`）：
   - 初始化 reftable 仓库、断言 `extensions.refStorage == "reftable"`、`reftable/` 目录存在、引用迭代器可用。

   待扩展：
   - 在 reftable 仓库上跑 `CreateBranch` / `LookupBranch` / 删除分支的完整生命周期。
   - 跑一次提交流程（`Commit.Create` 写入 HEAD，验证 reftable 写入语义）。
   - 与文件后端的并发性能对比基准。

3. **特性探测助手**
   - 新增 `func IsReftableSupported() bool`：尝试在临时目录 init 一个 reftable 仓库并立即清理，捕获 `MakeGitError`。比依赖 build tag 更鲁棒。
   - 新增 `func (r *Repository) RefStorageFormat() RefdbType`：读取 `extensions.refStorage`，无配置时返回 `RefdbFiles`。

### 5.2 中期

1. **CI 矩阵**
   - 在 CI 上跑两种 vendor：发布版 `v1.9.x` 与 master HEAD。
   - master 矩阵跑额外的 reftable 测试 tag。
   - 用 build tag（如 `libgit2_master`）隔离 reftable 专属测试用例，避免污染稳定版用户。

2. **文档与 README**
   - README 新增"Reference Storage Backends"小节，明确 RefdbReftable 的可用前提与风险。
   - `RepositoryInitOptions` GoDoc 补充例子。

3. **版本守卫策略**
   - 当 libgit2 主线 bump 到 1.10 或 2.0 时，及时同步 `Build_*.go` 的版本范围。
   - 考虑引入运行时 `git_libgit2_version()` 检查作为兜底（vs 当前的编译期 `#if`）。

### 5.3 长期

1. **完整反向探测能力**
   - 若上游后续补充 `GIT_FEATURE_REFTABLE` 或 `git_libgit2_opts` 选项，及时映射到 `Features()` / 新 API。
2. **per-worktree 引用语义**
   - 若上游公开 `git_reference__is_per_worktree_ref` 或等价 API，补绑定。
3. **reftable 维护操作**
   - 若上游公开 reftable compaction、stack 操作等专用 API，扩展 `Refdb` 绑定。
4. **SHA-256 与 reftable 组合**
   - reftable 是 git SHA-256 转型路上的关键依赖。等 `GIT_EXPERIMENTAL_SHA256` 稳定后，验证 RefdbReftable + Sha256 oid_type 组合在 git2go 上的行为。

---

## 6. 风险与权衡

| 风险 | 缓解措施 |
| --- | --- |
| master 不稳定，API 可能再变 | vendor pin 到具体 commit `32b564e63`，不随 main 自动滚动；遇上游 break 时同步更新本项目。 |
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
