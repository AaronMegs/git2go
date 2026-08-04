# SHA256 适配 · 待办执行进度

本文件记录 SHA256 兼容性适配的剩余待办项及其逐项执行情况。设计与结论见
`sha256-compat-design.md`（尤其 §3.7 ABI 守卫与 API 对称、§4.7 兼容性完整度自查）。

约定：
- **基线**：libgit2 `main`（子模块 pin，前瞻推荐），编译默认兼容 1.9.x。
- **三条验证路径**：默认 SHA1（系统库/ static）、实验 SHA256 + `libgit2_next`（main）、
  实验 SHA256（1.9.x overload）。

---

## 执行清单与状态

| # | 待办项 | 状态 |
|---|---|---|
| 1 | 独立 `Index` / `Diff` 的 SHA256 变体 | ✅ 已完成 |
| 2 | CI 接入多构建路径 | ✅ 已完成 |
| 3 | 1.9.x overload 路径复验 | ✅ 已完成 |
| 4 | SHA256 远端协商（transport `oid_type`） | ⏳ 进行中 |
| 5 | SHA256 远端端到端用例（clone/fetch/push） | ⬜ 未开始 |
| 6 | `Odb` 类型感知（`Hash` 自动跟随仓库类型） | ⬜ 未开始 |
| 7 | 上游转正后合并双实现 | ⏸ 触发式，条件未满足 |
| **N1** | **`TestApplyDiffAddfile` 在实验构建下 SIGBUS（本轮新发现，预存缺陷）** | ⬜ 未开始（阻塞 CI 全量） |

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

**遗留**：该 job 目前**限定测试范围**为 `SHA256|Oid`，原因见待办 **N1**（全量套件在实验构建下
存在预存崩溃，会导致 CI 误红）。N1 修复后应移除 `-test.run` 过滤。

---

## N1. `TestApplyDiffAddfile` 在实验构建下 SIGBUS — ⬜ 未开始（本轮新发现）

**发现过程**：为验证第 2 项的 CI job，首次在实验构建下运行**全量**测试套件
（此前只跑过 `-run 'SHA256|Oid'`），立刻崩溃。

**现象**：

```
--- TestApplyDiffAddfile/check_does_not_apply_to_current_tree_because_file_exists
SIGBUS: bus error
PC=0x12 m=0 sigcode=1 addr=0x12
signal arrived during cgo execution
_Cfunc_git_apply(0x105cfeb70, 0x105d02500, 0x2, 0x0)   <- diff.go:1041
```

`PC=0x12` 表示调用了被垃圾数据覆盖的函数指针，是典型的结构体布局/ABI 错配征兆。第4 参数为
`NULL`（git2go 在 `opts == nil` 时传 NULL），而 libgit2 的 `git_apply` 明确支持 NULL
（`if (given_opts) memcpy(...)`），故不是空指针误用。

**归属判定（已确认）**：`git stash` 掉本轮全部改动、在**已提交**状态下复现同一崩溃 →
**属预存缺陷，与第 1 项的 index/diff 改动无关**。

**尚未完成的区分**：需要在 **main 的非实验库**上跑同一用例，以区分两种可能：

- (a) libgit2 `main` 自身相对 1.9.x 的回归/ git2go 与 main 的不兼容（与 SHA256 无关）；
- (b) 仅在 `EXPERIMENTAL_SHA256=ON` 下出现（与 SHA256 的 `git_oid` 布局变化有关）。

**判定结果（已在第 3 项复验时顺带取得）：结论为 (b)。**

| 组合 | `TestApplyDiffAddfile` |
|---|---|
| 默认 SHA1（系统 libgit2 1.9.4，无 tag） | ✅通过 |
| 实验 SHA256 + libgit2 **1.9.4**（overload） | ❌ SIGBUS |
| 实验 SHA256 + libgit2 **main**（`libgit2_next`） | ❌ SIGBUS |

即：**与 libgit2 基线（1.9.4 / main）无关，只在 `EXPERIMENTAL_SHA256=ON` 构建下发生；
SHA1 默认路径完全不受影响。**

**已排除的原因**：

- 非空指针误用：`git_apply` 明确支持 `opts == NULL`（`if (given_opts) memcpy(...)`）。
- 非上游 apply 的类型假设：`apply.c` 已正确使用 `git_index__new(&postimage, repo->oid_type)`，
  未硬编码 SHA1。
- 非本轮改动：已提交状态下同样复现（`git stash` 验证）。

**下一步排查建议**（需 C 层手段，已超出纯 Go 层定位能力）：

1. 以 `-DCMAKE_BUILD_TYPE=Debug` + `-DEXPERIMENTAL_SHA256=ON` 重建 libgit2，用 lldb 取 C侧
   完整调用栈，确认被调用的野函数指针属于哪个结构体字段（`PC=0x12` 提示某字段被误当作
   函数指针）。
2. 用最小 C 程序复现（init仓库 → diff_tree_to_tree → git_apply），以判定是上游 libgit2 在
   实验构建下的 bug，还是 git2go 侧某处结构体使用不当；若为上游则向 libgit2 上报。
3. 重点怀疑对象：`git_diff`/`git_diff_delta`/`git_diff_file`（内嵌 `git_oid`，实验宏下尺寸变化）
   在 apply 路径中的 reader/index 交互。

**影响**：阻塞 CI 在实验构建下跑全量套件（当前已用 `-test.run 'SHA256\|Oid'` 规避）。
SHA256 的本地仓库全生命周期用例不受影响（全部通过）。

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

**收尾**：子模块已切回 main 基线 `ddf3b5c8`并重建实验库，工作区状态一致。

---

## 4. SHA256 远端协商（transport `oid_type`） — ⏳ 进行中
