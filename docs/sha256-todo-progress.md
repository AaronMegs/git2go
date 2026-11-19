# SHA256 适配 · 待办执行进度

本文件记录 SHA256 兼容性适配的逐项执行历史。设计与当前结论见
`sha256-compat-design.md` §1.8 / §4.8；发版迁移见 `sha256-breaking-changes.md`。

当前约定：
- **基线**：libgit2 promoted-SHA256 `main @ 939362a3`；旧 20 字节 `git_oid` ABI 的 1.9.x
  发布包不再兼容。
- **单一常规构建**：同一构建同时支持 SHA1 与 SHA256，SHA1 仍为默认；不再使用任何实验
  CMake 选项或 Go build tag。
- 下方第 1–6 项保留的是转正前双轨阶段的**历史执行记录**，其中的实验命令不可用于当前代码。

---

## 执行清单与状态

| # | 待办项 | 状态 |
|---|---|---|
| 1 | 独立 `Index` / `Diff` 的 SHA256 变体 | ✅ 已完成 |
| 2 | CI 接入多构建路径 | ✅ 已完成 |
| 3 | 1.9.x overload 路径复验 | ✅ 已完成 |
| 4 | SHA256 远端协商（transport `oid_type`） | ✅ 已完成（结论：无需适配，原判断有误已更正） |
| 5 | SHA256 远端端到端用例（clone） | ✅ 已完成 |
| 6 | `Odb` 类型感知（`Hash` 自动跟随仓库类型） | ✅ 已完成 |
| 7 | 上游转正后合并双实现 | 🟡 **第一步已完成并经完整性复审；主版本已定为 v36，当前为 v36-pre；等待上游正式版本号完成第二步**（见设计文档 §4.8）|
| 8 | 第一阶段完整性复审补漏 | ✅ 已完成（CI、standalone ODB/backend、NCmp、能力守卫、真实 indexer commit、文档）|
| **N1** | `TestApplyDiffAddfile` 在自建 libgit2 上 SIGBUS | ✅ 已修复（根因：本机头污染 + xdiff 用 `-isystem`）|
| **N2** | 3 个 `TestRebase*` 失败 | ✅ 已修复（测试硬编码 `master` + 取分支名时机错误）|
| **N3** | `TestTransport`：`Oid{}` 零值与库返回的 SHA1 zero id 不相等 | ✅ 已修复（真实 SHA256 缺陷）|

---

## 1. 独立 `Index` / `Diff` 的 SHA256 变体 — ✅ 已完成

**问题**：`wrapper.c` 的 shim 虽已能传类型，但 Go 层固定传 `NULL`/`0`，导致脱离仓库的
裸 index 文件与 patch 文本解析只能按 SHA1 处理，无法覆盖 SHA256。

**改动**：

| 层| 内容 |
|---|---|
| `wrapper.c` | `_go_git_index_new` / `_go_git_index_open` / `_go_git_diff_from_buffer` 三个 shim 新增 `int oid_type` 参数；实验分支构造 `git_index_options` / `git_diff_parse_options`（`GIT_*_OPTIONS_INIT` + `oid_type`），`main` 分支走 `*_ext`，1.9.x 分支走 overload；非实验分支 `(void)oid_type` 忽略 |
| `index.go` | 抽出 `newIndexWithOidType` / `openIndexWithOidType`；`NewIndex` / `OpenIndex` 委托并传 `0`（行为不变） |
| `diff.go` | 抽出 `diffFromBufferWithOidType`；`DiffFromBuffer` 委托并传 `0`（行为不变） |
| `sha256_api.go` | 新增 `NewIndexWithOidType`、`OpenIndexWithOidType`、`DiffFromBufferWithOidType` |
| `sha256_default.go` | 同名对称实现，`ObjectIdSHA256` 返回显式错误 |
| `repository_sha256_test.go` | 新增 `TestSHA256IndexWithOidType`、`TestSHA256DiffFromBufferWithOidType` |

**验证**：

- 实验 `main` + `libgit2_next`：17 个用例全 PASS，含
  - `TestSHA256IndexWithOidType` — 独立打开真实 SHA256 index 文件，条目 id 为 64-hex/SHA256 类型；
    in-memory SHA256 index 可向 SHA256 仓库写 tree。
  - `TestSHA256DiffFromBufferWithOidType` — 64-hex patch 按 SHA256 解析成功（1 个delta），
    **按 SHA1 解析必须失败**（区分性断言，证明 `oid_type` 真正生效而非被忽略）。
- 默认构建：`go build ./...` 通过；`-run 'Index|Diff|Oid|Patch'` 全过，无回退。

**结论**：SHA256 的 index / patch 解析能力补齐；默认路径行为逐字节不变。

---

## 2. CI 接入多构建路径 — ✅ 已完成

**问题**：三条构建路径（默认 SHA1 / 实验 SHA256+`libgit2_next` / 实验 SHA256 overload）全靠手工
执行，`libgit2_next` 标签误配只能靠构建脚本的 `NOTE` 提示兜底，缺少自动回归。

**改动**（`.github/workflows/ci.yml`，新增两个 job）：

1. **`build-static-sha256`** — 以 `EXPERIMENTAL_SHA256=ON` 构建libgit2 并跑实验测试。
   由于 main 与 1.9.x 的实验 oid API 形态无法用版本号区分（main 仍报 1.9.0），该 job
   **探测已安装头文件**是否含 `git_oid_from_string`，据此自动选择
   `test-static-sha256-next` 或 `test-static-sha256` —— 无论子模块 pin 到哪个基线都正确。
2. **`check-oid-abi-guard`** — 断言 ABI 错配被编译期拒绝：以
   `CGO_CFLAGS=-DGIT_EXPERIMENTAL_SHA256=1` 做**不带** tag 的构建，要求它失败且错误信息
   含 `rebuild git2go with -tags git_experimental_sha256`。这道防线守护的是"静默产出错误
   object id"这一最危险的场景（§3.7.1），必须有回归保护。

**验证**：本地以 CI 完全相同的命令形式执行
`make TEST_ARGS="-test.v -test.run 'SHA256|Oid'" test-static-sha256-next` → 17 个用例全 PASS；
探测逻辑在当前 main 基线上正确输出 "main API shape"；两个 make 目标 `-n` 展开正确。

**遗留**：无。N1 修复后该 job 已放开为**全量**套件（此前因 N1 崩溃临时用 `-test.run` 限定范围）。

---


## 3. 1.9.x overload 路径复验 — ✅ 已完成

**目的**：第 1 项（index/diff shim新增 `oid_type` 参数）在 `wrapper.c` 的 overload 分支使用了
`git_index_options` / `GIT_INDEX_OPTIONS_INIT` / `git_diff_parse_options`，需确认这些符号在
1.9.4 实验 ABI 上同样存在且语义一致——否则 overload 路径会编译或运行失败。

**执行**：子模块临时切至 `f7164261`（1.9.4）→ `EXPERIMENTAL_SHA256=ON` 重建实验静态库 →
`make TEST_ARGS="-test.v -test.run 'SHA256|Oid'" test-static-sha256`（**不带** `libgit2_next`）。

**结果**：16 个用例全PASS，其中包含第 1 项新增的
`TestSHA256IndexWithOidType`、`TestSHA256DiffFromBufferWithOidType`。
`TestSHA256RepositoryOidType` 正确未出现（它门控在 `libgit2_next` 下，overload 路径不编译它）。
构建脚本的形态探测也正确输出 "1.9.x experimental oid API"。

**附带收获**：在该库上取得了 N1 的关键判据（见上）。

**历史收尾**：该轮复验后曾切回当时的 main 基线 `ddf3b5c8`。当前子模块 pin 已进一步更新为 SHA256 转正后的 **`main @ 939362a3`**；最新状态与收敛计划见设计文档 §1.8 / §4.8。

---

## 4. SHA256 远端协商（transport `oid_type`） — ✅ 已完成（结论：**无需适配**）

**原判断（有误）**：早前基于 `git2/sys/transport.h` 的头文件阅读，把"为 git2go 的 managed smart
transport 实现 `git_transport.oid_type` 回调"列为待办的能力增强项。

**核实过程**：查上游实现而非仅头文件：

1. git2go 的 transport 扩展点是 **smart subtransport**，不是 `git_transport` 本身：
   `wrapper.c` 的 `_go_git_transport_smart()` 只填充 `git_smart_subtransport_definition`
   然后调用 **libgit2 内置的** `git_transport_smart()`；`RegisterManagedHTTPTransport` /
   `RegisterManagedSSHTransport` 注册的同样是 subtransport。git2go **从不**自己实现
   `git_transport` 结构体。
2. libgit2 内置 smart transport **已实现**该回调
   （`src/libgit2/transports/smart.c`）：

```c
#ifdef GIT_EXPERIMENTAL_SHA256
static int git_smart__oid_type(git_oid_t *out, git_transport *transport) {
    if (t->caps.object_format == NULL) *out = GIT_OID_DEFAULT;
    else *out = git_oid_type_fromstr(t->caps.object_format);
}
...
t->parent.oid_type = git_smart__oid_type;   /* 内置装配 */
#endif
```

**结论**：远端对象格式协商（读取远端 `object-format` capability）完全在 libgit2 内部完成，
git2go 的 subtransport 只搬字节流、与 oid 类型无关。**该项 git2go 侧零代码工作量**，SHA256
远端在实验构建下开箱可用。

**改动**：仅文档更正 —— `sha256-compat-design.md` §4.3 增补"由 libgit2 内置 smart transport
提供"的专项说明并附上游代码，清空"可选增强项"，§4.7.2 的限制表把该行标记为**已更正/无缺口**。

---

## 5. SHA256 远端端到端用例 — ✅ 已完成

**改动**：`repository_sha256_test.go` 新增 helper `seedTestRepoSHA256`（提交一个 SHA256 commit）
与 `TestSHA256Clone`。

**断言**：克隆一个 SHA256 仓库后
1) 源与克隆的 ref 一致（`Cmp == 0`）；
2) 克隆 ref 的 target 等于源 commit id，且**类型为 SHA256、hex 长度 64**；
3) 克隆能`LookupCommit` 到该 commit 并读出 message —— 证明接收到的 pack 是按正确对象格式索引的。

这覆盖了 fetch/negotiation + pack 索引路径，同时**实证了第 4 项的结论**（协商由libgit2 完成，
git2go 无需介入）。

**说明**：未断言 `clone.OidType()`，因为该getter 只在 `libgit2_next` 下有真值，写进非门控测试
会让 1.9.x overload 路径误红；上面的 64-hex 断言已足够。push未覆盖（需真实服务端）。

**验证**：`TestSHA256Clone` 在 main + `libgit2_next` 路径 PASS。

---

## 6. `Odb` 类型感知 — ✅ 已完成

**问题**：`Odb.Hash` 固定按 libgit2 默认类型（SHA1）哈希，因此在 SHA256 仓库上会返回一个与
仓库对象格式不符的 SHA1 值——是个静默的语义陷阱。

**约束**：libgit2 **没有** `git_odb` 的oid_type getter（`git_odb_options.oid_type` 只在创建时
传入），故只能由 `Repository` 侧透传。

**改动**：

| 文件 | 内容 |
|---|---|
| `odb.go` | `Odb` 新增私有字段 `oidType C.int`（0 = 未知/用libgit2 默认）；`Hash` 改为传 `v.oidType` 而非硬编码 0；补`runtime.KeepAlive(v)` |
| `repository.go` | `Repository.Odb()` 填充 `odb.oidType = C.int(v.OidType())` |

行为：从仓库取得的 `Odb` 按仓库对象格式哈希（SHA256 仓库→SHA256 id）；`NewOdb()` 的独立
`Odb` 保持 `oidType = 0`，即 libgit2 默认（SHA1），与历史一致。

**对默认构建零影响**：默认构建下 `Repository.OidType()` 恒为 `ObjectIdSHA1`，传1 与原先传 0
等价（且非实验分支的 shim 本就 `(void)oid_type`）。

**已知限制**：1.9.x 实验构建下 `Repository.OidType()` 因缺 `git_repository_oid_type` 符号而
回退报告 SHA1，故该路径上 SHA256 仓库的 `Odb.Hash` 仍产出 SHA1；需显式用
`Odb.HashWithType`。main（`libgit2_next`）路径无此限制。

**验证**：新增 `TestSHA256OdbHashFollowsRepository`（门控 `libgit2_next`）—— SHA256 仓库的
`Odb.Hash` 产出 64-hex SHA256 且与 `Write()` 一致；独立 `Odb` 仍为 SHA1。
main + next 路径 19 个用例全PASS；默认构建
`-run 'Odb|Repository|Index|Diff|Oid|Clone|Commit'` 全过，无回退。

---

## N1. `TestApplyDiffAddfile` SIGBUS — ✅ 已修复

**现象**：任何**自建** libgit2（1.9.4 实验 / 旧 main 实验 / 转正 main，均如此）下
`git_apply` 崩溃；链接 Homebrew 预编译 libgit2 的默认构建正常。

```
SIGBUS: bus error   PC=0x12   addr=0x12
_Cfunc_git_apply(0x105cfeb70, 0x105d02500, 0x2, 0x0)   <- diff.go:1041
```

**定位过程（lldb）**：

1. `register read lr` → 调用者是 `xdl_prepare_env + 1380`（libgit2 内嵌 xdiff）。
2. 反汇编该处：`ldr x8, [x27, #0x40]` + `blr x8` —— 调用某结构体偏移 `0x40` 的函数指针。
3. `register read x27` → **`x27 == git__allocator`**；`memory read` 显示只有前 3 项是有效函数
   指针，`+0x40` 处是整数 `0x12`（已越界到相邻全局数据）。
4. 对比核心库：`disassemble -n git_str_dispose` → `git__allocator` **`+0x10`**（3 字段布局的
   `gfree`）。**同一个 `libgit2.a` 内部对 `git_allocator` 布局认知不一致。**
5. 查include 列表：核心库用 **`-I`**（优先级高于编译器内建路径）→ 命中源码树 `alloc.h`（3 字段）；
   `deps/xdiff` 因上游 CMakeLists 写了 `SYSTEM` 关键字而用 **`-isystem`** → 与编译器内建系统路径
   竞争。
6. 本机 **`/usr/local/include/git2` 是 libgit2 1.5.2 残留**，其 `git_allocator` 有 **9 个**函数
   指针（`gmalloc, gcalloc, gstrdup, gstrndup, gsubstrdup, grealloc, greallocarray, gmallocarray, gfree`），
   `gfree` 正好在 **8×8 = 0x40** —— 与汇编完全吻合。

**根因**：**本机环境污染 + 上游 xdiff 用 `-isystem` 的脆弱设计**。xdiff 编译时 include 到了
libgit2 1.5.2 的 `git2/sys/alloc.h`，于是它按 9 字段布局取 `gfree`，而实际 `git__allocator`
只有 24 字节 → 把相邻全局数据当函数指针调用。

**与 SHA256 无关**。此前误判为"实验构建专属"，是因为默认构建总链接 Homebrew 库、实验构建总链接
自建库，两个变量被混淆了。

**修复**（`script/build-libgit2.sh`）：把源码树 include 目录以**普通 `-I`** 形式经
`CMAKE_C_FLAGS` 传入，使其对**所有** target 优先于任何 `-isystem`/内建路径：

```sh
LIBGIT2_INTREE_INCLUDE="-I${VENDORED_PATH}/include"
cmake ... -DCMAKE_C_FLAGS="-fPIC ${LIBGIT2_INTREE_INCLUDE}" ...
```

不改动系统目录、不依赖本机环境清理，可复现。

**验证**：重建后 `TestApplyDiffAddfile` PASS；实验构建全量套件从"崩溃中断"变为通过。

---

## N2. 3 个 `TestRebase*` 失败 — ✅ 已修复

**两层原因**：

1. 测试硬编码分支名 `"master"`，而本机 `init.defaultBranch` 不是 `master`
   → `cannot locate local branch 'master'`。
2. 改用 `defaultBranchName(t, repo)` 后仍失败（`expected 5, got 4`）——因为该 helper 返回
   **当前 HEAD 的分支名**，而调用点在 `setupRepoForRebase` **之后**，此时 HEAD 已切到
   feature 分支 `emile`，导致 rebase 到自身、历史少一条。

**修复**：在 `setupRepoForRebase` **之前**（HEAD 仍在初始分支时）捕获 `baseBranch :=
defaultBranchName(t, repo)`，3 个测试统一使用它。顺带 `gofmt -w rebase_test.go`
（该文件有预存的空格缩进块）。

**与 SHA256 无关**，属测试对本机 git 配置的隐含依赖。

---

## N3. `Oid{}` 零值与库返回的 SHA1 zero id 不相等 — ✅ 已修复（真实 SHA256 缺陷）

**发现**：N1 修复后套件得以跑到 `TestTransport`，报错的 `expected` 与 `got` 打印**完全相同**
却判为不等。

**根因**：实验/转正构建下 `Oid` 带 type 字节。测试用 `&Oid{}` 构造零值（`kind = 0`），而
libgit2 返回的 all-zeroes id 是 `kind = GIT_OID_SHA1 = 1`。两者 `String()` 相同但结构体不等。
更深层的问题是**同一类型上两个比较 API 语义不一致**：

- `Cmp` 用 `bytes.Compare(oid.Bytes(), oid2.Bytes())` —— 忽略 type ✓
- `Equal` 用 `*oid == *oid2` —— 含 type ✗

**修复**：

| 文件 | 改动 |
|---|---|
| `oid_sha256.go` | `Type()` 把 `kind == 0` 归一化为 `ObjectIdSHA1`（零值 Oid 视为 SHA1）；`rawLen()` 改用 `Type()` |
| `oid.go` | `Equal` 改为 `Type()` 相同且 `Bytes()` 相等，与 `Cmp` 语义对齐 |
| `transport_test.go` | 逐字段比较（`Name` + `Id.Equal`）替代 `reflect.DeepEqual`，两种构建下均正确；移除不再使用的 `reflect` 导入 |

**验证**：两条路径全量套件均通过。

---

## 8. 第一阶段完整性复审补漏 — ✅ 已完成

对 `4c84cd4` 做全仓只读复审后发现并关闭以下遗漏：

1. **CI system-dynamic 仍固定 v1.5.0**：改为构建并系统安装 pinned promoted main；旧版会被
   typed ABI 能力守卫必然拒绝。
2. **standalone SHA256 ODB/backend 未接线**：新增 `NewOdbWithOidType`、
   `NewOdbBackendLooseWithOidType`、`NewOdbBackendOnePackWithOidType`，并把 oid type 传入
   `git_odb_options` / backend options。端到端验证 loose 写读、packbuilder → indexer commit →
   one-pack backend 读回。
3. **`Oid.NCmp` 把 n 当字节数**：修为 libgit2 定义的 hex 字符（nibble）语义，增加奇数
   nibble 区分测试。
4. **能力守卫过弱**：从只检查 `GIT_OID_SHA256_SIZE` 扩展为同时检查
   `GIT_OBJECT_ID_OPTIONS_VERSION`、`GIT_INDEX_OPTIONS_VERSION`、
   `GIT_DIFF_PARSE_OPTIONS_VERSION`，明确拒绝旧 experimental overload 头。
5. **Indexer 测试未实际索引**：现构造真实 pack、调用 `Write`/`Commit`、断言64-hex SHA256
   pack id 与 `.idx` 文件，并通过 one-pack backend 读取原对象。
6. **文档仍混用历史双轨与当前状态**：当前结论、历史章节、迁移指南与进度里程碑已明确分层。

验证过程中还修复了两个并行 config 测试共用并删除 `./temp.gitconfig` 的隔离问题，改为各自
使用 `t.TempDir()`；修复后 bundled-static 全量连续运行 3 次均通过，system-static 与 dynamic
全量也均通过。

---

## 里程碑：转正后的常规构建全量通过

| 路径 | 库 | 结果 |
|---|---|---|
| bundled static（SHA1+SHA256） | pinned `main @ 939362a3` | `go test -tags static ./...` **全绿** |
| system static（SHA1+SHA256） | 同一 promoted main 安装 | 全量 **全绿** |
| dynamic（SHA1+SHA256） | 同一 promoted main 安装 | 全量 **全绿** |

阶段四第一步已完成：不再存在实验 build tag 或 `libgit2-experimental` 路径。git2go 主版本
已确定为 v36，当前进入 v36-pre（`v36.0.0-pre.N`）阶段；第二步等待上游正式版本号，用于
确定版本守卫并完成正式发布包的跨平台验证。
