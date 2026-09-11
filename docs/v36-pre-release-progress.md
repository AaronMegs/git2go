# git2go v36-pre 发布准备执行记录

> Module：`github.com/libgit2/git2go/v36`
>
> 预发布标签：`v36.0.0-pre.N`
>
> libgit2 基线：promoted-SHA256 `main @ 0551dfd4ad989b6a3d5683c0d4cf326c6efef929`

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
| 10 | 升级 libgit2 pin 至 `0551dfd4` 并清除全部 hard-deprecated 依赖 | ✅ 已完成 |
| 11 | 修复本地默认（dynamic）配置无法解析 promoted libgit2 | ✅ 已完成 |
| 12 | 修复 GitHub CI `check-generate` 失败并现代化 workflow | ✅ 已完成 |
| 13 | 整合 `feat-reftable`，使本线同时具备 SHA256 与 reftable 完整能力 | ✅ 已完成 |
| 14 | 以上游 main 最新提交为准复核适配；调研 reftable 事务方案 | ✅ 已完成 |

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

2026-08-04 查询 libgit2 官方 GitHub Releases：最新正式版为 **v1.9.6**，发布说明未声明
SHA256 已转为正式/非实验能力；promoted typed oid 目前只存在于 pinned main commit。

2026-09-03 复核：上游已发布 **v1.9.7**，阻塞仍未解除。逐个检查 v1.9.5 / v1.9.6 / v1.9.7 的
`include/git2/oid.h`，**均不含** `git_object_id_options`，即 promoted typed object-id API 仍未
进入任何正式发布。

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

- `vendor/libgit2`：C 子模块（pin 当时为 `939362a3`，第 10 节起为 `0551dfd4`）；
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

---

## 10. libgit2 pin 升级与 hard-deprecated 依赖清零 — ✅ 已完成

### 升级动因

对照上游 `main` 最新提交发现 pin `939362a3` 落后 57 个 commit，缺少两个安全修复：

- `5948ef3` **CVE-2026-5917**：`ssh_libssh2.c` 的 `gen_proto()` 未转义仓库路径，存在
  SSH 命令注入。git2go 的 Go managed SSH 早已独立修复同类问题且更严格，因此 Go 侧不受影响；
  但使用 libgit2 自带 SSH transport 的用户必须升级 pin 才能获得修复。
- `5254f5dc` **zstream**：截断的 zlib 流会导致无限循环，可被恶意 loose object 触发 DoS。

### API 影响评估

`939362a3..0551dfd4` 的公共头文件变更仅两处，均不破坏现有绑定：

- `include/git2/index.h`：新增 `git_index_extension_lookup/add/remove`（纯追加）；
- `include/git2/sys/refdb_backend.h`：仅 `@param` 文档名修正。

`oid.h` 与 `object.h` **零变更**，`GIT_OID_DEFAULT` 仍为 SHA1，SHA256 仍默认启用
（`experimental.h` 已是空 stub，CMake 仅保留 `USE_SHA256` provider 选择）。因此 SHA256
适配无需改动。pin 已更新为 `0551dfd4ad989b6a3d5683c0d4cf326c6efef929`。

### 关键发现：此前的 hard-deprecated 审计不充分

原审计只做 `DEPRECATE_HARD=ON` 构建 libgit2 再跑测试。该方式只能证明 git2go 未**链接**
弃用符号——因为 `libgit2.pc` 的 `Cflags` 不含 `-DGIT_DEPRECATE_HARD`，头文件仍向 git2go
暴露全部弃用声明。补充 `CGO_CFLAGS=-DGIT_DEPRECATE_HARD` 后暴露出 6 类真实依赖：

| 位置 | 弃用别名 | 正式名 |
| --- | --- | --- |
| 三个 `Build_*.go` | `LIBGIT2_VER_MAJOR` / `LIBGIT2_VER_MINOR` | `LIBGIT2_VERSION_MAJOR` / `LIBGIT2_VERSION_MINOR` |
| `credentials.go` | `git_cred_userpass_plaintext`、`git_cred_ssh_key` | `git_credential_userpass_plaintext`、`git_credential_ssh_key` |
| `indexer.go`、`odb.go`、`remote.go`、`wrapper.c` | `git_transfer_progress` | `git_indexer_progress` |
| `reference.go` | `GIT_REF_OID`、`GIT_REF_SYMBOLIC` | `GIT_REFERENCE_DIRECT`、`GIT_REFERENCE_SYMBOLIC` |
| `revparse.go` | `GIT_REVPARSE_*` | `GIT_REVSPEC_*` |
| `remote.go`、`wrapper.c` | `git_remote_completion_type` | `git_remote_completion_t` |

版本守卫这一项尤其危险：`LIBGIT2_VER_*` 位于 `deprecated.h` 的 `#ifndef GIT_DEPRECATE_HARD`
内，缺失时预处理器把它当 `0`，守卫会静默退化并误报版本不符。因此除改用正式宏外，还先用
`#if !defined(...)` 显式检查宏是否存在，缺失时给出明确错误。

### 连带修复：`UpdateTipsCallback` 从未触发

`git_remote_callbacks.update_tips` 已被 hard-deprecate。查证 `remote.c` 确认 libgit2 的调用
优先级是 `if (update_refs) ... else if (update_tips)`，而 git2go 在
`_go_git_populate_remote_callbacks` 中**无条件**注册 `update_refs`，因此弃用槽位永远不会被
调用——公开的 `UpdateTipsCallback` 实际是死代码。

修复方式：移除 C 侧 `update_tips` 注册与其 shim，改由 Go 的 `updateRefsCallback` 在
`UpdateRefsCallback` 为 nil 时回退分发 `UpdateTipsCallback`（其签名不含 refspec，故丢弃该参数）。
这同时消除了硬弃用依赖并让该回调恢复可用。

### `ConfigLevelProgramdata` 处理

`GIT_CONFIG_LEVEL_PROGRAMDATA` 已被上游从 `git_config_level_t` 移除，仅作为「被忽略」的宏保留在
`deprecated.h`，**没有正式替代**。为避免破坏下游编译，Go 常量保留并固定为字面量 `1`，同时标注
Deprecated 并说明 libgit2 会忽略该级别。

### CI 强化

`build-static-deprecate-hard` job 在原有 `DEPRECATE_HARD=ON` 构建之后，新增一步
`CGO_CFLAGS=-DGIT_DEPRECATE_HARD make test-static`，把审计从链接期提升到编译期，防止回归。

### pin 升级验证

- 新 pin 源码中确认存在 `git_str_puts_escaped(request, repo, ...)` 与 zstream 截断处理；
- 版本头仍报告 1.9.0，现有能力宏守卫策略保持有效；
- 默认（`DEPRECATE_HARD=OFF`）bundled-static 全量测试通过；
- `DEPRECATE_HARD=ON` 构建 + 全量测试通过；
- `CGO_CFLAGS=-DGIT_DEPRECATE_HARD` 编译期审计 + 全量测试通过；
- SHA256/Oid/HashFile/SSH/Push 定向测试全部通过；
- 新增 `remote_update_callbacks_test.go`：`TestUpdateTipsCallbackIsInvokedViaUpdateRefs`
  证明回调可触发，`TestUpdateRefsCallbackTakesPrecedenceOverUpdateTips` 证明优先级语义；
- `go run script/check-MakeGitError-thread-lock.go` 通过。

---

## 11. 本地开发环境：默认（dynamic）配置无法解析 — ✅ 已完成

### 现象与根因

编辑器与 `go list` 在**默认标签**（无 `static`）下会通过 `pkg-config libgit2` 找到本机系统库。本机是
Homebrew **libgit2 1.9.7**（release），实测缺少 git2go 已强依赖的 `git_object_id_options`、
`git_object_id_from_buffer`、`git_object_id_from_file`、`git_oid_from_prefix`，于是
`Build_system_dynamic.go` 的能力守卫触发 `#error`，并连带使 `remote.go` 等文件出现数百条
`undefined: C.*`。

该现象是守卫**正确工作**的结果，不是源码缺陷：已用改动前的守卫原文对同一系统库预处理验证，
同样触发，因此与本轮改动无关。

### 处理方式

在项目内独立前缀构建并安装 promoted libgit2，**不触碰 Homebrew 现有安装**：

```sh
SYSTEM_INSTALL_PREFIX="$PWD/system-build-promoted" \
  ./script/build-libgit2.sh --dynamic --system
```

安装结果确认齐备：`GIT_OID_SHA256_SIZE`、`GIT_OBJECT_ID_OPTIONS_VERSION`、
`GIT_INDEX_OPTIONS_VERSION`、`GIT_DIFF_PARSE_OPTIONS_VERSION`、`LIBGIT2_VERSION_MAJOR/MINOR`
以及四个 promoted API 全部 present。

### 为什么不替换 Homebrew 的 libgit2

评估后放弃全局替换。本机 `bat` 与 `eza` 均链接 `libgit2.1.9.dylib`，而 promoted 版与 1.9.7 的
`git_oid` ABI 不兼容（typed 结构 vs 旧 20 字节）。替换 Homebrew 链接会让这两个工具加载
ABI 不匹配的库，存在崩溃风险，代价高于收益。

### 编辑器配置

编辑器配置属于个人偏好，**不纳入版本控制**：`.vscode/` 已加入 `.gitignore`。在本地创建
`.vscode/settings.json`，让 Go 工具链使用与 `make test-static`、CI 相同的 bundled static
配置，并为默认 dynamic 配置保留 promoted 前缀作为回退：

```json
{
  "go.buildTags": "static",
  "gopls": { "build.buildFlags": ["-tags=static"] },
  "go.toolsEnvVars": {
    "PKG_CONFIG_PATH": "${workspaceFolder}/system-build-promoted/lib/pkgconfig",
    "CGO_LDFLAGS": "-Wl,-rpath,${workspaceFolder}/system-build-promoted/lib"
  }
}
```

两处路径均使用 `${workspaceFolder}`，可移植。JetBrains 系可在 Go 构建标签中填 `static`，
等效。

`system-build-promoted/` 同样已加入 `.gitignore`（并补上 `.idea`、`.codebuddy`、`.vscode/`，
修复原文件缺失的行尾换行）。

### 环境修复验证

- `go vet -tags static ./...`：通过（仅剩 git2go 既有的 `unsafe.Pointer`/`SliceHeader` 告警）；
- `PKG_CONFIG_PATH=<promoted> go build ./...`：默认标签编译通过；
- `PKG_CONFIG_PATH=<promoted> CGO_LDFLAGS=-Wl,-rpath,<promoted>/lib go test ./...`：
  默认 dynamic 链接模式全量测试通过（macOS SIP 会剥离 `DYLD_LIBRARY_PATH`，必须用 rpath）；
- Homebrew libgit2 1.9.7 保持原状，`bat`/`eza` 不受影响。

---

## 12. GitHub CI `check-generate` 失败与 workflow 现代化 — ✅ 已完成

### 现象

第 9 项修复后仍需实际平台验证，`gh run list` 显示 run `30907959432` 整体 **failure**：7 个
构建/测试 job 全绿，唯独 `Check generated files were not modified` 失败，报告
`delta_string.go`、`errorclass_string.go`、`errorcode_string.go` 在 `make generate` 后发生变化。

### 根因一：生成文件过期，且是真实缺陷

`errorclass_string.go` 与 `errorcode_string.go` 停留在上游旧提交 `137c05e`，此后 `git.go` 新增的
枚举值从未重新生成，导致 `String()` 对它们回退到数字形式（如 `"ErrorClass(32)"`）而非名称：

| 文件 | 此前缺失的值 |
| --- | --- |
| `errorclass_string.go` | `Worktree`(32)、`SHA`(33)、`HTTP`(34)、`Internal`(35)、`Grafts`(36) |
| `errorcode_string.go` | `Owner`(-36)、`Timeout`(-37)、`Unchanged`(-38)、`NotSupported`(-39)、`ReadOnly`(-40) |

重新生成后这些值恢复正确名称，越界值仍回退到数字形式。

### 根因二：代码生成器版本未固定

`delta_string.go` 的差异与枚举无关，纯粹是新版 stringer 的输出风格变化
（改为 `idx := int(i) - 0` 形式）。CI 使用 `stringer@latest`，上游任何格式调整都会让
`check-generate` 无故变红。现固定为 `STRINGER_VERSION: v0.49.0`（workflow 级 env）。

### workflow 现代化

- `actions/checkout` v4 → **v7**，`actions/setup-go` v5 → **v7**，消除
  「Node.js 20 is deprecated，被强制运行在 Node 24」告警；三个 workflow
  （`ci.yml`、`tag.yml`、`backport.yml`）同步升级。
- 修复 macOS job 的
  `Restore cache failed: Dependencies file is not found ... Supported file pattern: go.sum`：
  原先所有 job 都把 `setup-go` 排在 `checkout` 之前，缓存查找时仓库尚未检出，`go.sum` 自然
  不存在。现统一改为**先 checkout 再 setup-go**，模块缓存恢复生效。

### CI 修复验证

- 本地复现失败：未装 stringer 时 `go generate` 直接报错，装上 v0.49.0 后精确复现 CI 报告的
  三文件差异；
- 重新生成后 `go build --tags static ./...` 通过；
- 连续两次 `make generate` 输出一致（幂等），`check-generate` 的 `git diff --exit-code` 可满足；
- 三个 workflow YAML 均通过语法解析，逐 job 确认 checkout 已排在 setup-go 之前。

### 说明

`reference.go` 补充了 `ReferenceInvalid` 与 `ReferenceAll` 两个常量，与本节 CI 修复无关，
一并保留。

---

## 13. reftable 整合 — ✅ 已完成

### 背景

`feat-sha256`（本线）与 `feat-reftable` 自共同基线 `1f7dbc4`（libgit2 v1.9.4）后独立演进：
本线 18 个独有提交，reftable 线 46 个。两侧在 `wrapper.c`、`repository.go`、`oid*.go`、
`remote.go`、`Build_*.go`、CI、Makefile 上存在**语义冲突**（同一处两侧都改且改法不同），
因此采用按功能域手工整合，逐文件判定采信方，而非 merge 后择一侧。

完整决策记录见 `docs/sha256-reftable-integration-report.md`。

### 新增能力

- reftable 引用存储：`RefdbType`、`NewRefdbBackendReftable`、`RefStorageFormat`、
  `IsReftableSupported`；
- 统一初始化收口：`InitRepositoryExt` + `RepositoryInitOptions`，同时承载 `OidType`
  与 `RefdbType`，`InitRepositoryWithOidType` 降为其薄封装；
- Go 自定义 refdb 后端桥接（19 回调）、reflog API、引用事务 API；
- 运行时版本报告 `Version` / `Prerelease` / `VersionString`。

### 构建标签极性反转（与 reftable 线的有意分歧）

reftable 线用 `libgit2_reftable` 选择性**开启**；本线改为**默认编入**，用
`libgit2_no_reftable` 选择性关闭。理由：pin 的基线始终含 reftable，默认开启更贴合实际，
且退化路径仍完整保留并有 CI job 守护。

同时移除 `GIT2GO_HAS_REFDB_BACKEND_INIT` 条件编译（基线始终提供该回调）。

### 整合中修复的既有缺陷

交叉审查发现两侧各自遗留的 10 个真实缺陷，其中：

| 缺陷 | 位置 | 影响 |
| --- | --- | --- |
| `SetBackend` 双重释放 | `refdb.go` | 成功/失败两条路径均可能 double free |
| `git_oidarray` 未释放 | `merge.go` | 每次调用泄漏一个 oid 数组 |
| `httpError` 无同步 | `http.go` | 数据竞争 |
| `SetRefdb` 吞掉返回码 | `repository.go` | 替换失败被当作成功 |
| `toC()` 变异 map key | `oid_typed.go` | 零值 `Oid` 作键时被 C 侧改写 |
| `ShortenOids` SHA256 不正确 | `oid.go` | 返回无法区分的前缀长度 |
| CMake 选项名失效 | `script/build-libgit2.sh` | 线程/正则/认证配置静默未生效 |
| macOS `@rpath` 加载失败 | `Makefile` | `make test-dynamic` 在 macOS 完全无法运行 |

后两项为本轮实测复现：CMake 报 `unused-cli` 警告列出 `THREADSAFE`；dyld 报
`Library not loaded: @rpath/libgit2.1.9.dylib ... no LC_RPATH's found`。

### 发布流程安全回退修复

整合初期误判 diff 方向，漏取了 reftable 线 `tag.yml` 的加固设计，已补回：

- 顶层 `permissions: contents: read`，仅 tag job 提权为 write；
- **job 分离**：validate 跑测试代码但无写权，tag 持写权但不执行仓库代码。修复前单 job
  在执行测试的同时持有 write 凭据，被污染的测试用例可直接推送任意内容；
- `persist-credentials: false`；
- 只允许对 `origin/main` 的祖先提交打 tag；
- 拒绝前导零编号；tag 前校验 `HEAD == validated SHA`（防 TOCTOU）。

同时修正该文件自身的三处问题：`BUILD_DEPRECATED_HARD` → `DEPRECATE_HARD`（构建脚本读的是
后者，原 job 静默失效）、移除已反转的 `libgit2_reftable` 标签、去掉与 Go vendor 树矛盾的
`GOFLAGS` 注释。

### 验证矩阵

| 轨道 | 结果 |
| --- | --- |
| 静态（reftable 默认开启） | ok — 209 顶层 + 40 子测试，0 失败 0 跳过 |
| 静态 + 竞态检测 | ok |
| 静态 + `libgit2_no_reftable` | ok — reftable 相关用例正确降级为 SKIP |
| 动态链接 | ok（修复 macOS `@rpath` 后） |
| `DEPRECATE_HARD=ON` + cgo `-DGIT_DEPRECATE_HARD` | ok |
| `gofmt` + `go vet` | clean（原有 5 处 unsafe 误用已消除） |

核心验收项 SHA1/SHA256 × files/reftable 四组合**全部实际执行**，非跳过：

```text
--- PASS: TestRepositoryFormatMatrix/sha1-files
--- PASS: TestRepositoryFormatMatrix/sha1-reftable
--- PASS: TestRepositoryFormatMatrix/sha256-files
--- PASS: TestRepositoryFormatMatrix/sha256-reftable
```

### 测试可靠性

7 个测试依赖访问 `github.com`，此前在离线环境以 TLS 超时失败，且失败信息与本仓库代码无关。
现由 `requiresNetwork(t)` 统一门控，`go test -short` 或 `GIT2GO_SKIP_NETWORK_TESTS=1`
时跳过，并提供 `make test-static-offline`。

### 面向 libgit2 v2

- 零硬废弃依赖（库级 + cgo 级双重验证），v2 移除废弃 API 时不会断裂；
- ABI 守卫集中于 `git2go_version_check.h`，v2 若调整 `git_oid` 布局会在编译期报错而非
  运行期静默损坏；
- 能力探测优先于版本比较（libgit2 main 仍自报 1.9.0，版本号本就不可信）；
- CMake 选项已对齐上游当前命名；`go vet` 无 `reflect.SliceHeader` 等会被新 Go 收紧的用法。

---

## 14. 上游 main 复核与 reftable 事务调研 — ✅ 已完成

### 14.1 pin 与上游 main tip 一致

2026-09-07 复核（`git ls-remote` 直查远端，避免过期本地引用）：

```text
上游 main tip     0551dfd4ad989b6a3d5683c0d4cf326c6efef929
本仓库 pin        0551dfd4ad989b6a3d5683c0d4cf326c6efef929
落后提交数        0
```

网页侧交叉核对：该提交为 2026-08-15 合入的 PR #7346（zstream 死循环修复），
其后上游 main **无新提交**。因此当前适配即针对 main 最新状态，无需追平。

同时复核了 main 上与本线相关的近期动向：

- `605f34a` / `c0d2d4a`：SHA256 转正，移除 experimental 构建；
- `1e6aef7`：CI **移除 SHA256 构建、改为 nightly 跑 reftable 构建**；
- `42ba2b8`：新增 `CLAR_REF_FORMAT` 变量以按引用格式跑测试套件；
- `9b1ca87` / `3db3d5f`：reftable 修复与 SHA256 测试资源。

上游 CI 从「SHA256 专项」转向「reftable 专项」，与本线把 reftable 绑定改为**默认编入**
的判断一致。

### 14.2 正式发布阻塞：已取得硬证据

此前依据发布说明推断 v1.9.x 不含 promoted typed OID，本轮改为直接检查 tag 内容。
最新 release 为 **v1.9.7**（`49e408b3`）：

```text
v1.9.7:include/git2/oid.h
  git_object_id_options  -> 0 处   （promoted typed OID 的标志性类型，缺失）
  git_oid_from_prefix    -> 0 处
```

且 SHA256 仍在实验开关之后：

```c
#ifdef GIT_EXPERIMENTAL_SHA256
	GIT_OID_SHA1 = 1, GIT_OID_SHA256 = 2
#else
	GIT_OID_SHA1 = 1        /* 默认构建只有 SHA1 */
#endif

#ifdef GIT_EXPERIMENTAL_SHA256
# define GIT_OID_MAX_SIZE  GIT_OID_SHA256_SIZE   /* 32 */
#else
# define GIT_OID_MAX_SIZE  GIT_OID_SHA1_SIZE     /* 20 */
#endif
```

**结论：阻塞成立且已被一手证据确认。** 本线的 ABI 守卫要求
`GIT_OID_MAX_SIZE == 32`，因此对 v1.9.7 会正确地编译期拒绝（CI 的
`reject-legacy-v1-9-4` job 覆盖同类场景）。稳定 `v36.0.0` 仍须等待上游转正。

### 14.3 reftable 事务：根因是架构失配，非工时问题

完整调研见 `docs/reftable-transaction-research.md`。要点：

| 维度 | files 后端 | reftable 后端 |
| --- | --- | --- |
| 锁粒度 | 单引用（`x.lock`） | **整个引用数据库**（`tables.list`） |
| 可同时持有多把锁 | 是 | **否** |
| 原子性单位 | 每次 unlock 独立落盘 | 一个 addition 全量提交 |

libgit2 的 refdb vtable 是 per-ref `lock`/`unlock`，为 files 的 `.lock` 模型定制；
reftable 无法在不改该 vtable 的前提下正确实现。实测证据（C 探针直连静态库）：

```text
1st new_addition (ref A)  -> 0 ok
2nd new_addition (ref B)  -> -5  REFTABLE_LOCK_ERROR（库级锁已被持有）
```

即朴素映射恰好在事务最有价值的多引用场景下失败。对照 git 自身的
`ref_storage_be` 使用 `transaction_prepare/finish/abort` 三个**事务级**钩子，与
reftable 的 addition 生命周期天然同构——这解释了上游为何留 TODO 而非补一个函数。

**决策**：git2go 侧保持快速失败 + 可发现性（`RefStorageFormat` 事前判定），
不实现绕过。已评估并否决两条绕过路径：

- git2go 侧模拟批量提交：无法可靠判定「最后一次 unlock」，且会产出「看似原子实则不原子」
  的假象；
- git2go 直连 reftable 原语：实测头文件未公开安装，且**共享库中 `reftable_*` 符号全部被
  visibility 隐藏（可见数 0）**，动态链接不可行。

长期路径是向上游贡献 vtable 扩展（`GIT_REFDB_BACKEND_VERSION` 1→2），已纳入
libgit2 v2 适配观察项。

### 14.4 本轮代码改动

- `transaction.go`：文档从「上游尚未实现」改为说明架构失配与根因；
- `refdb_reftable_test.go`：新增 `TestFilesTransactionMultipleRefsAtomic`，固化
  「多引用同时加锁 + 统一提交」这一被 reftable 破坏的前提（经变异验证：移除 `Commit`
  后测试确实失败）。
