# git2go v36-pre 发布准备执行记录

> Module：`github.com/libgit2/git2go/v36`
>
> 预发布标签：`v36.0.0-pre.N`
>
> libgit2 基线：promoted-SHA256 `main @ 939362a3cb575de5f2aaebe1b1732c4ec8c1aebb`

本文件按执行顺序记录 v36-pre 发布准备工作、验证证据和仍受外部条件阻塞的事项。

## 总览

| # | 工作项 | 状态 |
| --- | --- | --- |
| 1 | 修复 managed SSH 远端命令注入与 URL/path 校验 | ✅ 已完成 |
| 2 | 升级 CI Go/actions/runner 并增加跨平台验证 | ✅ 已完成（Linux + macOS；Windows 留待正式包阶段） |
| 3 | 将自动 Tag 流程改造为受控的 `v36.0.0-pre.N` 发布 | ✅ 已完成 |
| 4 | 生成正式 Changelog / pre-release notes | ✅ 已完成 |
| 5 | 增加 SHA256 push/receive-pack 测试 | ✅ 已完成（本地真实 receive-pack；外部网络服务留待第 7 项） |
| 6 | 审计 deprecated libgit2 API，并验证 `DEPRECATE_HARD=ON` | ✅ 已完成 |
| 7 | 等待上游正式版本、更新守卫和跨平台正式包验证 | ⏸ 外部阻塞（最新正式版仍为 v1.9.6） |
| 8 | 绑定 `git_object_id_from_file` 文件路径类型化 hash | ✅ 已完成 |
| 9 | 修复 Go 1.14+ 自动 vendor 模式导致的 GitHub CI 全任务失败 | ✅ 已完成 |
| 10 | `feat-reftable` 分支收敛：自动 vendor 模式、线程锁检查、动态链接可移植性、managed HTTP 竞态 | ✅ 已完成 |

---

## 1. managed SSH 命令安全 — ✅ 已完成

### 问题

`ssh.go` 原先用 `\'` 转义位于 POSIX 单引号内的 apostrophe：

```go
uPath = strings.Replace(uPath, `'`, `\'`, -1)
cmd = fmt.Sprintf("git-upload-pack '%s'", uPath)
```

POSIX shell 在单引号内不解释反斜杠，因此路径 `/repo'; id; #` 可以提前结束参数并注入额外
命令。原实现还会把合法的单个反斜杠改成两个，改变远端仓库路径。

### 修复

- `quotePOSIXShellArg` 使用标准单引号拼接：`'` → `'\''`。
- service 名只由 `SmartServiceAction` 映射到固定的 `git-upload-pack` / `git-receive-pack`，
  不接受调用方传入任意命令。
- `parseSSHRemoteLocation` 明确支持：
  - `ssh://`、`ssh+git://`、`git+ssh://` URL；
  - Git SCP-like `[user@]host:path`；
  - bracketed IPv6 host。
- URL path 由 Go `net/url` **仅解码一次**；SCP-like path 保持字面 percent 序列。
- 在任何网络连接前拒绝：空 host/path、leading `-` 选项歧义、query/fragment/opaque URL、
  NUL/C0/DEL 控制字符、非法 percent escape。
- 使用 `net.JoinHostPort` 正确处理 IPv6。
- SSH client/session/pipe 初始化中途失败时统一关闭资源；`Close` 改为 nil-safe。

### 验证

新增 `ssh_test.go` 纯单元测试（**不执行 shell、不启动任何恶意命令**）：

- 普通路径、空格、反斜杠、单引号、`$()`、backtick、`; & | # !`、Unicode；
- 注入载荷经 `shlex.Split` 后始终恰好为“固定 service + 一个原样 path”；
- URL percent decode 一次、SCP-like percent 保持字面值、IPv6；
- 所有 `0x00..0x1f` 与 `0x7f` 控制字符拒绝；
- upload/receive 四类 action 与非法 action。

执行结果：SSH 定向测试通过；bundled-static 全量测试通过。

---

## 2. CI 现代化与跨平台验证 — ✅ 已完成

### 改动

- 最低 Go 测试版本与 `go.mod` 对齐为 **1.18**，并同时测试 `stable`；删除无效的
  Go 1.11–1.17 矩阵。
- 所有 CI job 更新为 `actions/setup-go@v5`、`actions/checkout@v4`、`ubuntu-latest`；
  checkout 统一使用 `submodules: recursive`，消除额外的手工 submodule update；backport workflow
  也同步到 `checkout@v4` / `ubuntu-latest`。
- dynamic、system-dynamic、system-static、generate job 统一使用 stable Go。
- 新增 `build-static-macos`，在 `macos-latest` 上从 pinned submodule 构建 bundled static
  libgit2 并运行全量测试。
- Linux 保留 bundled-static、dynamic、system-dynamic、system-static 四条构建/链接路径。

### 验证与边界

- YAML 由 IDE workflow lint 校验，无结构错误。
- 本地 bundled-static 全量套件继续通过。
- Windows 在本项目历史 CI 中从未覆盖，且需要验证 CMake 生成器、静态 archive 命名与
  Windows 系统库链接；为避免添加必然误红的未验证 job，将其明确保留到上游正式 libgit2
  package 阶段（第 7 项）。

---

## 3. v36-pre Tag 流程 — ✅ 已完成

### 原问题

旧 `.github/workflows/tag.yml` 在每次向 main/release 分支 push 时调用第三方 action 自动递增 patch
版本，既不能保证生成 `v36.0.0-pre.N`，也可能在上游 libgit2 正式发布前误打正式 tag。

### 新流程

- 改为仅 `workflow_dispatch` 手工触发，输入：
  - `prerelease_number`：仅允许大于 0 的数字 N；
  - `target_ref`：待标记的分支或 commit，默认 main。
- 固定生成轻量 tag `v36.0.0-pre.N`，工作流没有创建 `v36.0.0` 的路径。
- 权限最小化为 `contents: write`，并用 concurrency 防止两个 tag job 并发。
- tag 前验证：
  1. module 必须是 `github.com/libgit2/git2go/v36`；
  2. tag 本地和远端均不存在；
  3. 从 pinned submodule 构建 libgit2；
  4. bundled-static 全量测试通过。
- 不再依赖第三方自动 tag action；测试通过后使用 Git 原生命令推送单个精确 tag。

正式 `v36.0.0` 不由此工作流生成，必须等第 7 项完成后采用独立、显式的正式发布流程。

---

## 4. Changelog / pre-release notes — ✅ 已完成

新增根目录 `CHANGELOG.md` 的 `v36.0.0-pre.1 (unreleased)` 章节，内容覆盖：

- module `/v35` → `/v36` 与旧 libgit2 ABI 兼容边界；
- `Oid [20]byte` → typed SHA1/SHA256 value 的完整 breaking changes；
- 常规化的所有 type-aware API；
- build tag/实验安装布局移除；
- SHA256 端到端覆盖范围；
- managed SSH 命令注入修复；
- CI、构建头污染、rebase/Oid/config 隔离修复；
- `v36.0.0-pre.N` → `v36.0.0` 的发布门槛。

详细迁移示例继续由 `sha256-breaking-changes.md` 维护，Changelog 链接至该文档，避免重复维护
大段示例。

---

## 5. SHA256 push / receive-pack — ✅ 已完成

新增 `TestSHA256PushToLocalBare`：

1. 创建含真实 commit 的 SHA256 source repository；
2. 创建 bare SHA256 target repository；
3. 通过 `Remote.Push` 和完整 refspec 推送，触发 libgit2 pack generation、push negotiation、
   receive-pack 与 ref update；
4. 在 target 断言 ref target 等于源 commit、类型为 SHA256、hex 长度为 64；
5. 从 target ODB lookup commit 并校验 message，证明服务端对象写入成功。

该测试不依赖互联网或外部凭证，适合默认 CI。它覆盖真实 push/receive-pack 数据路径，但使用
local transport；SSH/HTTP 外部服务进程下的 push 留待第 7 项正式跨平台/集成环境验证。

验证结果：定向测试通过。

---

## 6. Deprecated API / `DEPRECATE_HARD=ON` 审计 — ✅ 已完成

### 机制

- `script/build-libgit2.sh` 支持显式 `DEPRECATE_HARD=ON|OFF`；默认仍为 OFF。
- 对其他值 fail closed，避免任意未校验值进入 CMake 选项。

### 执行与发现

经用户明确授权后，删除工作区内生成的 `static-build/`，以 `DEPRECATE_HARD=ON` 从 pinned
libgit2 main 完整重建。第一次全量链接失败，唯一缺失符号为：

```text
_git_config_find_programdata
```

该 API 在上游 `deprecated.h` 中明确标注：Git >= 2.24 已不再支持 ProgramData config，且无
项目内调用/测试。已删除导出的 `ConfigFindProgramdata` Go 绑定。这是 **breaking API removal**，
已加入 Changelog。

删除后重新运行 bundled-static 全量套件：**通过（5.224s）**。因此当前 git2go 在 pinned main
上可与 `DEPRECATE_HARD=ON` libgit2 完整编译、链接和运行。CI 新增
`build-static-deprecate-hard` job，持续守护该结论。

### 后续策略

v36-pre 构建脚本暂时仍默认 OFF，以保持与上游常规安装行为一致；审计/CI 可显式设 ON。正式
v36.0.0 发布时可根据上游正式包配置决定是否把默认值改为 ON。

---

## 7. 上游正式版本与正式 v36.0.0 — ⏸ 外部阻塞

2026-08-04 查询 libgit2 官方 GitHub Releases：最新正式版仍为 **v1.9.6**，发布说明未声明
SHA256 已转为正式/非实验能力；promoted typed oid 目前只存在于 pinned main commit。

因此以下工作不能提前完成：

- 将三个 `Build_*.go` 从临时的 1.9 + capability guard 改为正式版本下限；
- 把子模块从 main commit 切到正式 release commit/tag；
- 验证上游正式 Linux/macOS/Windows 包；
- 发布稳定 `v36.0.0`。

当前可发布范围是 `v36.0.0-pre.N`。Windows 正式 CI 和外部 SSH/HTTP SHA256 push 服务端测试
也归入本阶段；它们需要可复现的正式包/服务环境，不能用当前本地条件完整闭环。

---

## 8. 文件路径类型化 hash — ✅ 已完成

### API

- `(*Odb).HashFile(path, objectType)`：使用 ODB/Repository 自身对象格式（SHA1 或 SHA256）。
- `(*Odb).HashFileWithType(path, objectType, oidType)`：显式选择 SHA1/SHA256。

C shim 构造 `git_object_id_options` 并调用转正后的 `git_object_id_from_file`，不再依赖已废弃的
`git_odb_hashfile`。路径在传给 `C.CString` 前拒绝内嵌 NUL，防止截断成另一个文件。

### 语义边界

与上游一致，该 API 哈希文件的 raw content，不应用 `.gitattributes`、CRLF 等 repository filters；
需要过滤语义时应使用 repository-aware hash API。

### 文件 hash 验证

新增 `TestHashFileWithOidType`，覆盖：

- SHA256 repository ODB 的 `HashFile` 自动返回 64-hex SHA256；
- 显式 SHA256 file hash 与 buffer `HashWithType` 完全一致；
- 显式 SHA1 file hash 为 40-hex 且与 SHA1 buffer hash 一致；
- standalone `NewOdb()` 的 `HashFile` 继续默认 SHA1；
- SHA1/SHA256 不相等；
- 含 NUL 路径和不存在文件均返回错误；
- 文件名包含空格和 apostrophe、文件内容包含 NUL 字节。

定向测试与 bundled-static 全量测试均通过。

---

## 9. GitHub CI vendor 一致性 — ✅ 已完成

### CI 现象

Go 1.18、stable、macOS、dynamic、system static/dynamic、`DEPRECATE_HARD=ON` 等所有 job 在
执行首个 Go 命令时共同失败：

```text
go: inconsistent vendoring in .../git2go:
  github.com/google/shlex ... is explicitly required in go.mod,
  but not marked as explicit in vendor/modules.txt
  golang.org/x/crypto ...
  golang.org/x/sys ...
```

### vendor 失败根因

项目将 libgit2 子模块放在 `vendor/libgit2`，但没有 Go 的 `vendor/modules.txt`。自 `go 1.14`
起，只要 `go.mod` 的 Go 版本 >= 1.14 且存在 vendor 目录，Go 命令会自动启用 vendor mode。
CI 升级到 Go 1.18/stable 后因此把 `vendor/` 视为 Go vendor tree，并在任何测试执行前拒绝不一致
状态。这不是 macOS 专属问题，所有 job 根因相同。

### vendor 修复方案

运行 `GOFLAGS=-mod=mod go mod vendor` 生成与 `go.mod` 一致的依赖源码和 `vendor/modules.txt`，
随后恢复同目录下 pinned `vendor/libgit2` 子模块。Makefile 新增 `vendor-go` 目标固化该顺序；后续
依赖升级应执行 `make vendor-go`，避免 `go mod vendor` 清空 C 子模块工作树。最终 vendor tree
同时包含：

- `vendor/libgit2`：C 子模块（pin `939362a3`）；
- `vendor/github.com/google/shlex`；
- `vendor/golang.org/x/crypto`、`x/sys`；
- `vendor/modules.txt`：三个 Go module 均标记 `explicit`。

选择提交 Go vendor（约 134 个文件、1.1 MiB），而不是仅在 CI 强制 `-mod=mod`，原因是下游
使用 Go 1.14+ 直接运行 `go test` 也会遇到同样问题；完整 vendor tree 才能从根因上修复默认行为。

### CI vendor 验证

无需 `GOFLAGS=-mod=mod`：

- `go list -m` → `github.com/libgit2/git2go/v36`；
- `go run script/check-MakeGitError-thread-lock.go` → 通过；
- `go list -mod=vendor all` → 215 个 package；
- `go test -tags static --count=1 ./...` → 全量通过；
- `go mod verify` → `all modules verified`。

> **注**：以上是 `feat-sha256` 分支当时的处理方式（提交 Go vendor 树）。`feat-reftable` 改用
> `GOFLAGS=-mod=readonly` 显式退出自动 vendor 模式，`vendor/` 仅保留 libgit2 C 子模块。
> 取舍理由见 [§10.1](#101-自动-vendor-模式阻塞所有-go-命令)。

---

## 10. `feat-reftable` 分支收敛 — ✅ 已完成

第 9 项的 Go vendor 修复此前只落在 `feat-sha256`。本轮在 `feat-reftable`（vendor 已 pin 到
main `939362a3c`）上重新执行端到端验证时，暴露并修复了四个彼此独立的阻塞点（§10.1–§10.4）。

### 10.1 自动 vendor 模式阻塞所有 Go 命令

`make test-static` 在第一个 Go 命令即失败：

```text
go: inconsistent vendoring in .../git2go:
  github.com/google/shlex ... is explicitly required in go.mod,
  but not marked as explicit in vendor/modules.txt
```

根因与第 9 项相同：自 Go 1.14 起，**只要 `vendor/` 目录存在**（判定条件是目录本身，不是
`vendor/modules.txt`）且 `go.mod` 声明的 Go 版本 ≥ 1.14，工具链就自动选择 `-mod=vendor`；
本项目的 `vendor/libgit2` C 子模块恰好让该目录存在，随后校验 `vendor/modules.txt` 必然失败。

但**本分支的修复方式与第 9 项不同**：`vendor/` 只承载 pinned 的 libgit2 C 子模块，不提交任何
Go 依赖源码，因此改为显式退出自动 vendor 模式：

- `Makefile` 顶层 `export GOFLAGS ?= -mod=readonly`，覆盖所有 recipe；
- `.github/workflows/{ci,tag}.yml` 顶层 `env.GOFLAGS`，覆盖不经 Makefile 直接调用 `go` 的
  job（`reject-legacy-v1-9-4`、`build-reftable`、`build-system-*`、`check-generate`、
  发布校验）。

选择 `readonly` 而非 `mod`：两者都能退出 vendor 模式，但 `-mod=mod` 允许 Go 在解析依赖时改写
`go.mod` / `go.sum`，`readonly` 不会，因此构建过程对版本文件保持只读。二者本地实测均可通过，
取更保守的一个。

不采用“提交 Go vendor 树”的原因：本仓库的 `vendor/` 语义是 C 子模块目录，混入约 134 个
Go 依赖文件（约 1.1 MiB）会让该目录承担两种互相冲突的职责——`go mod vendor` 会重建整个
`vendor/`，从而清空 C 子模块工作树，必须靠额外的 make 目标兜底。相比之下一行 `GOFLAGS`
不引入这种耦合。此选择对下游无影响：Go module zip 不包含子模块内容，下游取到的包里不存在
`vendor/` 目录，因此不会触发自动 vendor 模式。

### 10.2 `MakeGitError` 线程锁检查 6 处违规

`script/check-MakeGitError-thread-lock.go` 报出 1 处生产代码 + 5 处测试回调：

| 位置 | 性质 | 处理 |
| --- | --- | --- |
| `refdb_backend.go` `NewRefdbBackendFromInterface` | **真实缺陷**：`_go_git_refdb_backend_alloc` 失败后 `MakeGitError` 读取线程局部 `git_error_last()`，未锁线程可能读到其他 goroutine 的错误 | 补 `runtime.LockOSThread()` / `defer runtime.UnlockOSThread()` |
| `refdb_backend_test.go` 的 `Lookup` / `Iterator` / `Rename` / `ReflogRead` / `Next` | **语义误用**：这些 Go 回调是错误的*产生方*，却用 `MakeGitError2` 去*读取* libgit2 的线程局部错误 | 改为直接构造 `&GitError{...}`（`refdbBackendTestError`）。C 桥接的 `setCallbackError` 按 `GitError.Code` 回传错误码，因此行为等价且语义正确 |

注：加锁不是唯一正确解。对回调而言，正确做法恰恰是不去读 libgit2 的 last error，因此这里选择
消除误用而非机械加锁。

### 10.3 macOS 动态链接不可用

bundled dylib 的 install name 是 `@rpath/libgit2.1.9.dylib`，而 `make test-dynamic` 只导出
`LD_LIBRARY_PATH`——dyld 并不使用该变量，且测试二进制没有 `LC_RPATH`：

```text
dyld[...]: Library not loaded: @rpath/libgit2.1.9.dylib
  Reason: no LC_RPATH's found
```

修复：`Makefile` 在 `test-dynamic` 中额外注入 `CGO_LDFLAGS=-Wl,-rpath,$(DYNAMIC_LIBDIR)`
（绝对路径），Linux 下冗余但无害，从而保持单一代码路径，同时保留 `LD_LIBRARY_PATH`。

### 10.4 managed HTTP 传输的数据竞态

全量 `--race` 回归（而非仅定向用例）暴露出 2 处 `DATA RACE`，分别由
`TestCloneWithExternalHTTPUrl` 与 `TestCertificateCheck` 触发，读写点都在
`http.go` 的 `httpSmartSubtransportStream`。该文件属上游既有代码，与 reftable 工作无关，
但确认是真实缺陷而非误报：

```go
// 修复前
func (self *httpSmartSubtransportStream) sendRequestBackground() {
	go func() {
		self.httpError = self.sendRequest() // 赋值发生在 recvReply.Done() 之后
	}()
	self.sentRequest = true
}
```

`sendRequest` 内部 `defer self.recvReply.Done()`，因此对 `httpError` 的赋值发生在 `Done()`
**之后**，`Read` 里的 `recvReply.Wait()` 无法为该写入建立 happens-before。此外
`Write` 完全不经过 `Wait` 就读取 `httpError`；`sendRequest` 结尾还会在后台 goroutine 里写
`sentRequest`，与 `Read` 的读取并发。

修复（三点，均保持原有语义）：

1. `httpError` 由 `httpErrorMu` 保护，新增 `setHTTPError` / `getHTTPError`，覆盖 `Write`
   这条不经过 `Wait` 的读路径；
2. 把原 `sendRequest` 主体抽为 `doSendRequest`，新 `sendRequest` 在 `Done()` 之前完成
   `setHTTPError`，使 `Wait()` 的返回方必然观察到错误与 `resp`；
3. `sentRequest` 只由发起请求的 goroutine（`Read` 同步路径 / `sendRequestBackground`）写入，
   后台 goroutine 不再写它。`Read` 在 `sendRequest` 成功后才置位，与修复前“仅成功时置位”一致。

### 10.5 全量回归矩阵（vendor `939362a3c`，2026-08-31）

测试共享 `./temp.gitconfig`，因此全部串行执行（`-p=1`），每轮前清理残留 lock。

| # | 轨道 | 命令 | 结果 |
| --- | --- | --- | --- |
| 1 | 线程锁检查 | `go run script/check-MakeGitError-thread-lock.go` | PASS |
| 2 | 依赖解析（`-mod=readonly`，无 Go vendor 树） | `go mod verify`、`go list all` | PASS（219 package，`go.mod`/`go.sum` 未被改写） |
| 3 | vet（双 tag） | `go vet --tags "static libgit2_reftable"` / `--tags static` | PASS |
| 4 | 编译（双 tag） | `go build --tags static` / `--tags "static libgit2_reftable"` | PASS |
| 5 | 静态库重建 | `make build-libgit2-static` | PASS |
| 6 | 动态库重建 | `make build-libgit2-dynamic` | PASS |
| 7 | **静态全量** | `go test --tags "static libgit2_reftable" -count=1 -p=1 -v ./...` | **201 PASS / 0 FAIL / 0 SKIP** |
| 8 | **动态全量** | `make TEST_ARGS='-count=1 -p=1 -v' test-dynamic` | **201 PASS / 0 FAIL / 0 SKIP** |
| 9 | **无 tag 全量** | `go test --tags static -count=1 -p=1 -v ./...` | **191 PASS / 0 FAIL / 10 SKIP**（SKIP 全为 reftable 用例） |
| 10 | **静态竞态全量** | `go test --tags "static libgit2_reftable" --race ...` | 修复 §10.4 前 199 PASS / 2 FAIL（2 DATA RACE）；修复后 **201 PASS / 0 FAIL / 0 DATA RACE** |
| 11 | 无 tag 竞态全量 | `go test --tags static --race ...` | PASS |
| 12 | 动态竞态全量 | `--tags libgit2_reftable --race`（带 rpath） | PASS |
| 13 | `DEPRECATE_HARD=ON` 重建 + 全量 | `BUILD_DEPRECATED_HARD=ON ./script/build-libgit2-static.sh` → 静态全量 | 构建 PASS、**201 PASS / 0 FAIL / 0 SKIP** |
| 14 | 恢复默认产物 | `./script/build-libgit2-static.sh`（`DEPRECATE_HARD=OFF`）+ 复测 | PASS |

第 9 项的 10 个 SKIP：`TestIsReftableSupported`、`TestRefStorageFormatReftable`、
`TestRepositoryRefdb`、`TestRefdbCompressReftable`、`TestNewRefdbBackendReftable`、
`TestReftableBranchLifecycle`、`TestReftableReopenPersistence`、`TestReftableReflogLifecycle`、
`TestReftableSymbolicReferenceLifecycle`、`TestInitRepositoryExtReftable`——
191 + 10 = 201，与其余轨道用例总数一致，说明降级路径只裁掉 reftable 能力。

#### 本地未覆盖的 CI 轨道

以下三条属 CI 环境专有，本地未执行（需要外部授权动作）：

| 轨道 | 未执行原因 |
| --- | --- |
| `reject-legacy-v1-9-4`（ABI 守卫负向测试） | 需 `git -C vendor/libgit2 fetch --tags` 并临时 checkout `v1.9.4` 重建，会改动 vendor 工作树与 `static-build/` |
| `build-system-static` / `build-system-dynamic-main` | 需 `sudo ./script/build-libgit2.sh --system` 写入 `/usr` |
| `check-generate` | 需联网 `go install golang.org/x/tools/cmd/stringer@v0.1.12`（本机未安装） |
