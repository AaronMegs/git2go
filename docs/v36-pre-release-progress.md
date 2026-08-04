# git2go v36-pre 发布准备执行记录

> Module：`github.com/libgit2/git2go/v36`
>
> 预发布标签：`v36.0.0-pre.N`
>
> libgit2 基线：promoted-SHA256 `main @ 939362a3cb575de5f2aaebe1b1732c4ec8c1aebb`

本文件按执行顺序记录 v36-pre 发布准备工作、验证证据和仍受外部条件阻塞的事项。

## 总览

| # | 工作项 | 状态 |
|---|---|---|
| 1 | 修复 managed SSH 远端命令注入与 URL/path 校验 | ✅ 已完成 |
| 2 | 升级 CI Go/actions/runner 并增加跨平台验证 | ✅ 已完成（Linux + macOS；Windows 留待正式包阶段） |
| 3 | 将自动 Tag 流程改造为受控的 `v36.0.0-pre.N` 发布 | ✅ 已完成 |
| 4 | 生成正式 Changelog / pre-release notes | ✅ 已完成 |
| 5 | 增加 SHA256 push/receive-pack 测试 | ✅ 已完成（本地真实 receive-pack；外部网络服务留待第 7 项） |
| 6 | 审计 deprecated libgit2 API，并验证 `DEPRECATE_HARD=ON` | ✅ 已完成 |
| 7 | 等待上游正式版本、更新守卫和跨平台正式包验证 | ⏸ 外部阻塞（最新正式版仍为 v1.9.6） |

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
