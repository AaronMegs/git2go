# SHA256 + reftable 整合适配报告

> 分支：`feat-sha256-reftable`
> 整合来源：`feat-sha256-reftable`（SHA256 主体，HEAD `55cf351`）+ `feat-reftable`（reftable 主体，`3d65b2d`）
> libgit2 基线：`vendor/libgit2` pin 至 main `0551dfd4ad989b6a3d5683c0d4cf326c6efef929`
> （2026-09-07 经 `git ls-remote` 复核，该提交即上游 main tip，落后 0 个提交）
> 目标：单一分支同时具备 libgit2 main 的 SHA256 与 reftable 完整能力，为后续 libgit2 v2 适配奠定基础

---

## 1. 结论摘要

整合已完成并全量验证通过。

- SHA1/SHA256 × files/reftable **四组合全部实测通过**，无跳过。
- 默认静态轨道 **205 个顶层测试 + 40 个子测试全部通过，0 失败 0 跳过**。
- `gofmt`、`go vet` 全绿；`go vet` 此前报告的 5 处 `unsafe.Pointer` / `reflect.SliceHeader` 误用已全部消除。
- 竞态检测、动态链接、`DEPRECATE_HARD=ON`、`-DGIT_DEPRECATE_HARD`、`libgit2_no_reftable` 退化构建均通过。

整合过程中另外修复了 **两处内存泄漏、一处数据竞争、一处双重释放、一处静默失败**，详见第 5 节。这些均为两分支各自遗留的真实缺陷，而非合并引入。

### 1.1 关键决策：不做整体 merge

两分支自共同基线 `1f7dbc4`（libgit2 v1.9.4）后独立演进：当前分支 18 个独有提交，`feat-reftable` 46 个独有提交，且在 `wrapper.c`、`repository.go`、`oid*.go`、`remote.go`、`Build_*.go`、CI、Makefile 上存在**语义冲突**（同一处两侧都改，且改法不同）。

因此采用**按功能域手工整合**，逐文件判定采信方，而非 `git merge` 后择一侧。每个决策点记录在第 3 节。

---

## 2. 整合后的能力矩阵

| 能力 | 状态 | 入口 |
|---|---|---|
| SHA1 对象格式 | 默认 | `ObjectIdSHA1` |
| SHA256 对象格式 | 支持 | `ObjectIdSHA256`、`IsSha256Supported()` |
| files 引用存储 | 默认 | `RefdbFiles` |
| reftable 引用存储 | 支持（默认编入） | `RefdbReftable`、`IsReftableSupported()` |
| 统一仓库初始化 | 支持 | `InitRepositoryExt(path, *RepositoryInitOptions)` |
| 引用存储格式探测 | 支持 | `Repository.RefStorageFormat()` |
| Go 自定义 refdb 后端 | 支持（19 回调桥接） | `NewRefdbBackendFromInterface` |
| reflog 读写 | 支持 | `Repository.ReadReflog` 等 |
| 引用事务 | 支持（files 后端） | `Repository.NewTransaction()` |
| 运行时版本报告 | 支持 | `Version()` / `VersionString()` |

`RepositoryInitOptions` 是 SHA256 与 reftable 两条线的**收口点**：

```go
repo, err := git.InitRepositoryExt(path, &git.RepositoryInitOptions{
    Flags:     git.RepositoryInitMkpath | git.RepositoryInitBare,
    OidType:   git.ObjectIdSHA256,  // 对象格式
    RefdbType: git.RefdbReftable,   // 引用格式
})
```

---

## 3. 逐文件整合决策

### 3.1 采信当前分支（SHA256 侧）

| 文件 | 理由 |
|---|---|
| `credentials.go` | 已用 `git_credential_*` 提升名，`feat-reftable` 仍用硬废弃的 `git_cred_*` |
| `indexer.go` | 已用 `git_indexer_progress`，对侧仍用废弃的 `git_transfer_progress` |
| `reference.go`（枚举） | 已用 `GIT_REFERENCE_DIRECT/SYMBOLIC`，对侧仍用 `GIT_REF_*` |
| `revparse.go` | 已用 `GIT_REVSPEC_*`，对侧仍用 `GIT_REVPARSE_*` |
| `config.go` | `ConfigLevelProgramdata` 已按上游移除处理 |
| `*_string.go` | stringer 输出已重新生成，覆盖新增 5 个 ErrorCode；对侧为过期产物 |
| `remote.go`（回调派发） | 已移除 `updateTipsCallback` 导出，改由 `update_refs` 统一派发 |
| `wrapper.c`（typedef） | 已用 `git_remote_completion_t` / `git_indexer_progress` |
| `vendor/` Go 依赖树 | 对侧无 Go vendor 树 |
| libgit2 pin | 已是 `0551dfd4`，含 CVE-2026-5917 修复 |

### 3.2 采信 feat-reftable（reftable 侧）

| 文件 | 理由 |
|---|---|
| `refdb.go` | `owner` 归属校验 + `SetBackend` 双重释放修复 + `RefStorageFormat` |
| `refdb_backend.go`（新增） | 19 回调 Go 自定义后端桥接 |
| `reflog.go` / `transaction.go`（新增） | reflog 与事务 API |
| `oid.go` | `ObjectIdTypeDefault`、`validateObjectIdType`、纯 Go `ShortenOids`（SHA256 正确性修复） |
| `oid_typed.go` | 非变异 `toC()` + `outC()`（map key 稳定性修复） |
| `handles.go` | `GetOk`，析构回调容忍已释放句柄 |
| `features.go` | `Version` / `Prerelease` / `VersionString` |
| `repository.go` | `RepositoryInitOptions` 全套 + `SetRefdb` 返回 error |
| `merge.go` | `git_oidarray_dispose` 泄漏修复 + nil 校验 |
| `http.go` | `httpError` 数据竞争修复 |
| `message.go` / `rebase.go` | `unsafe.Slice` 替代裸指针运算 |
| `Makefile`（dynamic） | macOS `@rpath` 修复 |

### 3.3 手工合并（两侧都不能直接采用）

**`wrapper.c`** — 保留当前分支的 typed OID shims 与已清理的 `_go_git_populate_remote_callbacks`（9 回调，无 `update_tips`），在文件尾部追加 `feat-reftable` 的 21 个 refdb 桥接符号。**未**回带对侧的废弃 typedef，**未**恢复 `update_tips_callback`。

**`oid_type_api.go`** — 保留当前分支的 `_go_git_odb_hash` / `_go_git_repository_oid_type`，移除已被 `InitRepositoryExt` 取代的 `_go_git_repository_init` shim，并补齐 `validateObjectIdType` 前置校验。

**`remote.go`** — 保留当前分支的回调派发逻辑，但把 `sptr` + `uintptr` 步进的字符串数组处理换成 `unsafe.Slice`（对侧和本侧都不理想，两者取优后重写）。

**CI (`ci.yml`)** — 以当前分支结构为主（含 `check-generate`、`STRINGER_VERSION` pin、`DEPRECATE_HARD` 双重验证），补入对侧的旧 ABI 负向测试与 reftable 专项 job，新增 `lint` 与 `no-reftable` job。全部 26 处 action 引用已固定到 commit SHA。

**发布流程 (`tag.yml`)** — 采信 `feat-reftable` 的安全设计。整合初期一度误判 diff 方向而漏取，后经复审补回：

| 项 | 补回前 | 补回后 |
|---|---|---|
| 权限 | 全局 `contents: write` | 顶层 `read`，仅 tag job 提权 |
| **job 分离** | 单 job：执行测试代码时持有 write 凭据 | validate（跑代码/无写权）+ tag（有写权/不跑代码） |
| 凭据持久化 | 默认持久化 | `persist-credentials: false` |
| 可打 tag 范围 | 任意 ref | 必须是 `origin/main` 的祖先提交 |
| 编号校验 | 接受 `007` | 拒绝前导零 |
| TOCTOU | 无防护 | 打 tag 前校验 `HEAD == validated SHA` |

其中 job 分离最关键：修复前，一个被污染的测试用例可直接利用 write 凭据推送任意内容。

同时修正该文件自身的三处问题：`BUILD_DEPRECATED_HARD` → `DEPRECATE_HARD`（构建脚本读的是后者，原 job 静默失效）、移除已反转的 `libgit2_reftable` 标签、去掉与 Go vendor 树矛盾的 `GOFLAGS` 注释。

### 3.4 与 feat-reftable 的有意分歧

| 项 | feat-reftable | 本分支 | 理由 |
|---|---|---|---|
| reftable 构建标签 | `libgit2_reftable` 选择性**开启** | `libgit2_no_reftable` 选择性**关闭** | pin 的基线始终含 reftable，默认开启更贴合实际；退化路径仍保留 |
| `GIT2GO_HAS_REFDB_BACKEND_INIT` | `#ifdef` 条件编译 | 移除 | 基线始终提供 `git_refdb_backend.init` |
| `git2go_version_check.h` | 用 `LIBGIT2_VER_MAJOR` | 用 `LIBGIT2_VERSION_MAJOR` | 前者在 `GIT_DEPRECATE_HARD` 下未定义，预处理器会静默按 0 比较，守卫失效 |
| deprecate-hard CI 变量 | `BUILD_DEPRECATED_HARD=ON` | `DEPRECATE_HARD=ON` | 构建脚本读取的是后者，前者不生效 |

---

## 4. 破坏性变更（Breaking Changes）

完整列表见 `CHANGELOG.md`。以下是**本次整合新引入**的部分，按迁移成本排序。

### 4.1 编译期即可发现（安全）

**`Repository.SetRefdb` 增加 error 返回值**

```go
// 旧
repo.SetRefdb(refdb)
// 新
if err := repo.SetRefdb(refdb); err != nil { /* 处理 */ }
```

原实现丢弃了 libgit2 的返回码，替换失败时表现为成功。这是编译期断裂，不会静默。

### 4.2 运行期行为变化（需要审查）

**`Refdb.SetBackend` 所有权语义修正**

原实现在**失败**时调用 `backend.Free()`（此时 libgit2 并未接管，属于释放自己不拥有的内存），在**成功**时保留 Go finalizer（此时 libgit2 已接管，finalizer 会二次释放）。两条路径都可能双重释放。

现在：失败不释放（调用方可重试或自行释放），成功清空包装器并解除 finalizer。

> **迁移**：删除失败分支里的 `backend.Free()` 调用。

**`ShortenOids` 语义收紧**

改为纯 Go 实现，因为 `git_oid_shorten` 只检查前 40 个十六进制字符，对第 40 位之后才分叉的 SHA256 id 会返回**不足以区分**的前缀长度。

现在对 nil oid、混合类型、重复 id、越界 `minlen` 返回 `ErrorCodeInvalid`（原先部分情况返回错误结果或 panic）。

**`NewOidFromBytes` 短输入返回 nil**

原先对 20 字节以下的输入直接切片 panic。

> **迁移**：检查返回值是否为 `nil`。

**typed 构造函数校验类型**

传入非法 `ObjectIdType` 时返回 `ErrorCodeInvalid`，不再把原始值透传给 C。

### 4.3 构建层变化

- `Build_*.go` 改为 `#include "git2go_version_check.h"`。若下游复制过这些文件，需要一并取得该头文件。
- 构建脚本改用 libgit2 当前的 CMake 选项名。**旧名 `THREADSAFE` / `REGEX_BACKEND` / `USE_NTLMCLIENT` / `USE_GSSAPI` 已不被 libgit2 读取**——此前这些设置是静默失效的（CMake 会报 `unused-cli` 警告但不失败）。
- 新增 `libgit2_no_reftable` 标签用于链接无 reftable 的 libgit2。

---

## 5. 整合中修复的既有缺陷

这些缺陷存在于整合前的某一侧或两侧，由本次交叉审查发现。

| # | 缺陷 | 位置 | 影响 |
|---|---|---|---|
| 1 | `git_oidarray` 未释放 | `MergeBases`、`MergeBasesMany` | 内存泄漏，每次调用泄漏一个 oid 数组 |
| 2 | `httpError` 无同步 | `http.go` | 数据竞争：后台 goroutine 写、`Write` 不等待即读 |
| 3 | `SetBackend` 双重释放 | `refdb.go` | 成功/失败两条路径均可能 double free |
| 4 | `SetRefdb` 吞掉返回码 | `repository.go` | 替换失败被当作成功 |
| 5 | `toC()` 变异 map key | `oid_typed.go` | 零值 `Oid` 作 map key 时键被 C 侧改写 |
| 6 | `ShortenOids` SHA256 不正确 | `oid.go` | 返回无法区分的前缀长度 |
| 7 | CMake 选项名失效 | `script/build-libgit2.sh` | 线程/正则/认证配置静默未生效 |
| 8 | macOS `@rpath` 加载失败 | `Makefile` | `make test-dynamic` 在 macOS 上完全无法运行 |
| 9 | `calloc` 失败未检查 | `remote.go`、`repository.go` | OOM 时空指针解引用 |
| 10 | 缺少 `//go:build` 约束 | `script/check-MakeGitError-thread-lock.go` | `gofmt` 不合规 |
| 11 | 发布流程 job 持写权限执行仓库代码 | `.github/workflows/tag.yml` | 被污染的测试可利用 write 凭据推送 |
| 12 | `BUILD_DEPRECATED_HARD` 变量名错误 | `tag.yml` 发布门禁 | 硬废弃审计在发布门禁中静默未生效 |

缺陷 8 为本次实测复现：

```text
dyld[88989]: Library not loaded: @rpath/libgit2.1.9.dylib
  Reason: no LC_RPATH's found
```

---

## 6. 验证记录

全部在 macOS (darwin/arm64) + libgit2 `0551dfd4` 下实测。

### 6.1 测试轨道

| # | 轨道 | 命令 | 结果 |
|---|---|---|---|
| 1 | 静态（reftable 默认开启） | `go test --tags static` | **ok** 209 顶层 + 40 子测试 / 0 failed / 0 skipped |
| 2 | 静态 + 竞态检测 | `go test --tags static -race` | **ok** |
| 3 | 静态 + reftable 关闭 | `go test --tags static,libgit2_no_reftable` | **ok** reftable 用例正确降级为 SKIP |
| 4 | 动态链接 | `make test-dynamic` | **ok** |
| 5 | `DEPRECATE_HARD=ON` 库 | `go test --tags static` | **ok** |
| 6 | 叠加 `-DGIT_DEPRECATE_HARD` | `CGO_CFLAGS=-DGIT_DEPRECATE_HARD` | **ok** |
| 7 | `gofmt` + `go vet` | `make lint` | **clean** |
| 8 | 线程锁检查 | `go run script/check-MakeGitError-thread-lock.go` | **ok** |
| 9 | 离线模式 | `make test-static-offline` | **ok** 202 passed / 7 skipped |

### 6.2 四组合矩阵（核心验收项）

```text
--- PASS: TestRepositoryFormatMatrix
    --- PASS: TestRepositoryFormatMatrix/sha1-files
    --- PASS: TestRepositoryFormatMatrix/sha1-reftable
    --- PASS: TestRepositoryFormatMatrix/sha256-files
    --- PASS: TestRepositoryFormatMatrix/sha256-reftable
```

四组合**均实际执行**，非跳过。在 `libgit2_no_reftable` 构建下，两个 reftable 组合正确降级为 SKIP 而非 FAIL。

### 6.3 reftable / refdb / reflog 专项

37 个相关测试全部通过，覆盖：

- 后端构造：`NewRefdbBackendReftable`、`NewRefdbBackendFs`、`OpenRefdb`、`Refdb.Compress`
- 生命周期：分支增删改查、符号引用、reflog、**重新打开后持久化**
- 格式探测：`RefStorageFormat` 正确遵循 `core.repositoryformatversion >= 1` 门控（v0 仓库即使声明 `extensions.refStorage=reftable` 也报告 files）
- Go 桥接：多迭代器生命周期、**并发迭代器**、能力位（init/compress/lock）、回调 panic 转错误
- 事务：files 后端支持、**reftable 后端正确报告不支持**、提交后不可复用、取消回滚

### 6.4 外网依赖测试（已治理）

7 个测试需要访问 `github.com`，在离线或受限网络下以连接/TLS 握手超时失败，且失败信息与
本仓库代码无关：

```text
clone_test.go:86: cannot clone remote repo via https, error: ... TLS handshake timeout
```

涉及 `TestCloneWithExternalHTTPUrl`、`TestCertificateCheck`、`TestRemoteConnect`、
`TestRemoteConnectOption`、`TestRemoteLs`、`TestRemoteLsFiltering`、
`TestRemoteCredentialsCalled`。这些测试均非本次整合引入。

现由 `git_test.go` 中的 `requiresNetwork(t)` 统一门控：`go test -short` 或
`GIT2GO_SKIP_NETWORK_TESTS=1` 时跳过，并提供 `make test-static-offline`。

实测：默认模式 209 通过 / 0 跳过；离线模式 202 通过 / 7 跳过，跳过数与门控测试数一致。

---

## 7. 面向 libgit2 v2 的准备情况

本次整合为 v2 适配清理了以下障碍：

1. **零硬废弃依赖**。`DEPRECATE_HARD=ON` 构建 + cgo 层 `-DGIT_DEPRECATE_HARD` 双重验证通过，说明既不链接也不引用任何废弃声明。v2 移除废弃 API 时不会断裂。
2. **集中式 ABI 守卫**。`git2go_version_check.h` 是唯一的版本/布局断言点，v2 若调整 `git_oid` 布局会在编译期立即报错，而非运行期静默损坏。
3. **能力探测优先于版本比较**。`IsSha256Supported()` / `IsReftableSupported()` 基于实际探测，不依赖版本号（libgit2 main 仍自报 1.9.0，版本号本就不可信）。
4. **CMake 选项已对齐上游当前命名**，不再依赖已废弃的选项名。
5. **`go vet` 干净**，无 `reflect.SliceHeader` 等在新 Go 版本中会被收紧的用法。

### 7.1 遗留事项

| 项 | 说明 |
|---|---|
| 稳定版 `v36.0.0` | 阻塞于上游发布含 promoted typed OID 的正式版本。2026-09-07 直查最新 release v1.9.7 的 `oid.h`：无 `git_object_id_options`，SHA256 仍在 `GIT_EXPERIMENTAL_SHA256` 之后，默认 `GIT_OID_MAX_SIZE` 仍为 20。当前仅发 `v36.0.0-pre.N` |
| reftable 事务 | **架构失配，非工时问题**：libgit2 refdb vtable 是 per-ref 锁，reftable 只有库级锁。已评估并否决两条绕过路径，长期需上游 vtable 扩展。详见 `docs/reftable-transaction-research.md` |
| 跨平台验证 | 本次仅在 macOS 实测；Linux/Windows 依赖 CI |
| Windows 平台 | 归入正式包阶段，依赖上游正式发布 |

---

## 8. 复现方式

```sh
git submodule update --init --checkout vendor/libgit2
make build-libgit2-static

make lint
make TEST_ARGS=--count=1 test-static
make TEST_ARGS=--count=1 test-static-race
make TEST_ARGS=--count=1 test-static-no-reftable

make build-libgit2-dynamic
make TEST_ARGS=--count=1 test-dynamic

DEPRECATE_HARD=ON make build-libgit2-static
CGO_CFLAGS=-DGIT_DEPRECATE_HARD make TEST_ARGS=--count=1 test-static
```

离线或网络受限环境改用：

```sh
make TEST_ARGS=--count=1 test-static-offline
```
