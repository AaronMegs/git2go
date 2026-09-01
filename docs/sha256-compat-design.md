# git2go SHA1 / SHA256 兼容性适配设计文档

> **当前状态（基线：libgit2 `main` @ `0551dfd4`）：SHA256 已在上游转正，第一步代码/构建收敛已完成。**
> `GIT_EXPERIMENTAL_SHA256` 宏、`EXPERIMENTAL_SHA256` cmake 选项与实验安装布局均已移除；
> `git_oid` 无条件为 typed 结构，同一构建同时支持 SHA1 与 SHA256，且 SHA1 仍为默认。
>
> 适用范围：`github.com/libgit2/git2go/v36`。主版本已确定为 **v36**；上游 libgit2 尚未
> 发布包含 SHA256 转正的正式版本前处于 **v36-pre** 阶段（Git tag 使用
> `v36.0.0-pre.N`）。旧 20 字节 `git_oid` ABI 的 libgit2 1.9.x 发布包不再兼容，相关用户
> 应停留在 git2go v35。
>
> 本文 §1.1–§1.7、§2–§4.5 保留的是转正前双轨方案的**历史设计记录**，不可再作为当前构建
> 指南。当前实现与两步发版策略分别见 §1.8、§4.8；迁移指南见 §5 和
> `sha256-breaking-changes.md`。

---

## 1. 上游 SHA256 API 评估（需求 1）

> §1.1–§1.7 为转正前历史评估；当前结论从 §1.8 开始。

### 1.1 总体结论（历史）

截至 pin 的 commit，libgit2 中 SHA256 **仍是实验特性**，由编译期宏 `GIT_EXPERIMENTAL_SHA256` 门控：

- **未定义**该宏（默认）：`git_oid` ABI、所有 oid 相关函数签名与 git2go 当前假设完全一致。
- **定义**该宏：`git_oid` 的内存布局与一批函数签名发生**破坏性变化**，且额外暴露 `GIT_OID_SHA256` 枚举与一组 SHA256 专用宏/函数。

`oid.h` 头注释明确："SHA1 is currently the only supported object ID type"，并提示该宏"会破坏 API 兼容性，仅用于应用兼容性测试"。因此适配必须做到"宏存在与否两套都能编译"。

### 1.2 `git_oid` 结构体 ABI 变化（最关键）

```c
typedef struct git_oid {
#ifdef GIT_EXPERIMENTAL_SHA256
    unsigned char type;            /* 新增：oid 类型前缀字节 */
#endif
    unsigned char id[GIT_OID_MAX_SIZE];  /* 默认 20，实验下 32 */
} git_oid;
```

影响：
1. **新增 `type` 前缀字节** → 字段 `id` 的偏移从 0 变为 1。任何"从结构体首地址直接拷贝 20 字节"的代码都会**读偏一位**（读到 type + 前 19 个 id 字节）。
2. **`id` 数组从 20 扩到 32 字节** → 结构体总大小变化（默认 20B → 实验约 33B，含对齐）。

### 1.3 `git_oid_t` 枚举与默认类型

```c
typedef enum {
    GIT_OID_SHA1 = 1,
#ifdef GIT_EXPERIMENTAL_SHA256
    GIT_OID_SHA256 = 2,
#endif
} git_oid_t;
#define GIT_OID_DEFAULT GIT_OID_SHA1
```

### 1.4 尺寸宏的演进

| 宏 | 值 | 可用性 | 说明 |
|---|---|---|---|
| `GIT_OID_SHA1_SIZE` | 20 | 始终 | SHA1 原始字节数 |
| `GIT_OID_SHA1_HEXSIZE` | 40 | 始终 | SHA1 十六进制长度 |
| `GIT_OID_SHA256_SIZE` | 32 | 仅实验 | SHA256 原始字节数 |
| `GIT_OID_SHA256_HEXSIZE` | 64 | 仅实验 | SHA256 十六进制长度 |
| `GIT_OID_MAX_SIZE` | 20 / 32 | 始终 | 取决于是否实验 |
| `GIT_OID_MAX_HEXSIZE` | 40 / 64 | 始终 | 取决于是否实验 |
| `GIT_OID_MINPREFIXLEN` | 4 | 始终 | 前缀最小 hex 长度 |
| `GIT_OID_HEXSZ` / `GIT_OID_RAWSZ` | 40 / 20 | **已弃用** | `DEPRECATE_HARD=ON` 时不可用 |

> 注：为兼容以 `main` 为基线的 vendored 构建（`main` 上部分 git2go 仍绑定的符号被标记弃用，如 `git_odb_hash`），`script/build-libgit2.sh` 已将 vendored 与 system 构建的 `DEPRECATE_HARD` 统一设为 **OFF**（保留弃用但仍有效的符号可链接）。即便如此，本项目仍已把弃用宏 `GIT_OID_HEXSZ` 迁移到 `GIT_OID_SHA1_HEXSIZE` / `GIT_OID_MAX_HEXSIZE`，以防将来重新开启 HARD 或上游彻底移除。

### 1.7 上游 `main` 分支的演进（历史记录：写于SHA256 尚未转正时）

> 本节记录的是 `main` 处于**实验态**时的 API 演进（overload → `_ext`/`_from_`）。
> 该演进结论仍然成立且是当前 `libgit2_next` 路径的依据；但"SHA256 仍未转正"的前提
> 已被 §1.8 推翻。

对 libgit2 **最新 `main` 分支**头文件逐一复核（`oid.h`/`odb.h`/`index.h`/`diff.h`/`indexer.h`/`repository.h`/`deprecated.h`/`version.h`），关键结论：

1. **SHA256 仍是实验特性、未转正**：`main` 的 `CMakeLists.txt` 仍有 `option(EXPERIMENTAL_SHA256 ... OFF)`，`git_oid.type`、`GIT_OID_SHA256` 枚举、SHA256 尺寸宏依旧被 `#ifdef GIT_EXPERIMENTAL_SHA256` 门控。→ **本项目的双构建策略依然成立，"阶段四合并"的触发条件尚未满足。**

2. **实验 API 形态发生系统性重构（overload → 拆分为 `_ext`/`_from_` 新函数）**——这是相较 pin 的 1.9.4 **最重要的破坏性差异**：

| 关注点 | 1.9.x 发布版（兼容基线，编译默认） | libgit2 `main`（前瞻基线，子模块 pin） |
|---|---|---|
| oid 带类型解析 | **重载** `git_oid_fromstr/fromstrn/fromstrp/fromraw`（原名 +`git_oid_t`） | 旧名冻结为无类型 SHA1；**新增** `git_oid_from_string`/`git_oid_from_prefix`/`git_oid_from_raw` |
| odb 创建 | **重载** `git_odb_new(odb, opts)` | 旧名冻结 `git_odb_new(odb)`；**新增** `git_odb_new_ext(odb, opts)` |
| odb 打开 | 重载 | 新增 `git_odb_open_ext` |
| odb 哈希 | 重载 `git_odb_hash(..., oid_type)` | `git_odb_hash` **被弃用**（→`git_object_id_from_buffer`），且**不带** `git_oid_t`（SHA1-only） |
| index 创建/打开 | 重载（options 入参） | 旧名冻结；**新增** `git_index_new_ext`/`git_index_open_ext`（`git_index_options.oid_type`） |
| diff 解析 | 重载 `git_diff_from_buffer(..., opts)` | 旧名冻结；**新增** `git_diff_from_buffer_ext`（`git_diff_parse_options.oid_type`） |
| indexer 创建 | 重载（`mode`/`odb`→opts，+`oid_type`） | **形态相同**（仍重载，未改名） |
| 仓库初始化 | `git_repository_init_ext` + `opts.oid_type` | **形态相同** |
| `version.h` | 1.9.4（NUMBER=1090400） | 1.9.0（NUMBER=**1090000**，反而更低） |

3. **无法用版本号区分两种形态**：`main` 的 `version.h` 仍报告 1.9.0，**低于** 1.9.4 发布版（libgit2 仅在发布分支上 bump 版本），因此 C 预处理层面**不能**用 `LIBGIT2_VERSION_NUMBER` 判别 overload/`_ext` 两种 API。→ 必须用**显式开关**选择。

**对本项目的直接影响**：子模块基线已指向 `main`，前瞻推荐路径为 `libgit2_next`（`_ext`/`_from_` 形态，已对真实 main 端到端验证）。为保当前可用性，**编译默认仍兼容 1.9.x 发布版**（overload 形态）；两条路径经统一 shim 收口，切换仅靠构建标签。适配方案见 §3.6。

### 1.5 函数签名变化（仅在实验宏下新增 `git_oid_t type` 入参）

| 函数 | 默认签名 | 实验签名 |
|---|---|---|
| `git_oid_fromstr` | `(out, str)` | `(out, str, type)` |
| `git_oid_fromstrp` | `(out, str)` | `(out, str, type)` |
| `git_oid_fromstrn` | `(out, str, len)` | `(out, str, len, type)` |
| `git_oid_fromraw` | `(out, raw)` | `(out, raw, type)` |

格式化/比较类（`git_oid_fmt`、`git_oid_nfmt`、`git_oid_pathfmt`、`git_oid_tostr`、`git_oid_cpy`、`git_oid_cmp`、`git_oid_equal`、`git_oid_ncmp`、`git_oid_is_zero`、`git_oid_shorten_*`）签名不变，但语义按 oid 类型自适应长度。

### 1.6其它新增 oid 类型选项的 API

- `git_repository_init_options.oid_type`（经 `git_repository_init_ext`），用于创建 SHA256 仓库。
- `git_odb_options.oid_type` / `git_odb_new(..., opts)`、`git_odb_hash` 系列在实验下需要/接受类型信息。
- `git_indexer_options.oid_type`（本项目 `wrapper.c:_go_git_indexer_new` 已使用 `git_indexer_options`，仅未设置 `oid_type`）。

---

### 1.8 ⚠️ SHA256 已转正（复核基线：`main` @ `939362a3`，2026-08-03）

这是**推翻前述所有"实验态"前提**的关键状态变更。证据链如下：

| 检查项 | 结果 |
|---|---|
| `CMakeLists.txt` 的 `EXPERIMENTAL_SHA256` 选项 | **已移除** |
| `GIT_EXPERIMENTAL_SHA256` 宏（`src/`、`include/` 全仓）| **零匹配** |
| `include/git2/experimental.h.in` | **已删除**（`src/libgit2/experimental.h.in` 仅剩空 include guard）|
| `git_oid` 结构 | **无条件** `{ unsigned char type; unsigned char id[GIT_OID_MAX_SIZE]; }` |
| `GIT_OID_SHA256= 2`、`GIT_OID_MAX_SIZE = 32`、`GIT_OID_SHA256_*` 宏 | **无门控** |
| 常规构建产物 | `libgit2.a` + `include/git2/`（**无 `-experimental` 后缀**），SHA256 默认可用 |

上游 `cmake/ExperimentalFeatures.cmake` 的注释直接确认：

> "there are currently no experimental options — **SHA256 was first implemented as an experimental option**."

#### 转正后的 API 形态（实测）

- **带类型的主API**：`git_oid_from_string` / `git_oid_from_prefix` / `git_oid_from_raw`、
  `git_odb_new_ext` / `git_odb_open_ext`、`git_index_new_ext` / `git_index_open_ext`、
  `git_diff_from_buffer_ext`、`git_object_id_from_buffer`、`git_repository_oid_type`
  （**全部无门控**）。
- **保留的SHA1-only 便利函数**：`git_oid_fromstr` / `fromstrp` / `fromstrn` / `fromraw`
  仍在 `oid.h` 且**未标 deprecated**；`GIT_OID_DEFAULT` 仍为 `GIT_OID_SHA1`（向后兼容）。
- **legacy 无 options 签名已被移除**（唯一签名化）：`git_indexer_new`(3参)、
  `git_odb_backend_one_pack`(3参)、`git_odb_backend_loose`(3参)。
- `git_odb_hash` 已移入 `deprecated.h`。

#### 对本项目的即时影响（实测三种组合）

| 组合 | 结果 | 根因 |
|---|---|---|
| 默认（无 tag） | ❌ 编译失败 | `wrapper.c` 非实验分支调用的3 个 legacy 无 options 签名已被上游移除 |
| `git_experimental_sha256` | ❌ pkg-config 失败 | `Build_bundled_static_sha256.go` 指向 `libgit2-experimental.pc` / `-lgit2-experimental`，转正后不存在 |
| `+ libgit2_next` | ❌ 同上（构建接线）；**但代码路径正确** | 绕过 pc 名后（`system_libgit2` tag + `PKG_CONFIG_PATH` 指向常规产物）**编译通过且全量测试全绿** |

**结论**：为 `main` 编写的 `_ext`/`_from_` 适配（`libgit2_next`）就是转正后的正确代码路径，
唯一障碍是构建接线仍假设 `-experimental` 布局。§4.6 的阶段四收敛条件已满足，
收敛方案见 §4.8。

---

## 2. 本项目绑定现状与受影响接口清单（需求 2）

### 2.1 一个关键判断：什么会坏，什么不会坏

cgo 中**直接使用 `C.git_oid` 类型**的代码（`var x C.git_oid`、`[]C.git_oid`、`unsafe.Sizeof(C.git_oid{})`、对 `C.git_oid` 切片做 `range`/指针步长）会由 cgo 按当前编译的真实结构体大小自动计算尺寸，**两套构建下都正确**，无需逐个改尺寸。

真正会坏的是 **Go 侧对 oid 形态写死了 SHA1 假设** 的地方，集中在：

1. `Oid` 定义为 `[20]byte`（git.go:212）。
2. `newOidFromC` 用 `C.GoBytes(coid, 20)`（git.go:220）：**既写死 20，又默认 id 在偏移 0**——实验下会读到 `type` 字节。
3. `NewOidFromBytes` `copy(oid[0:20], b[0:20])`（git.go:226）。
4. `NewOid` 用 `C.GIT_OID_HEXSZ` 且校验 `len==20`（git.go:235/246/250）。
5. `String()` 全量 hex（git.go:255）——对 32 字节 oid 自然可用，但依赖 `Oid` 底层长度。
6. `ShortenOids` 写死 `make([]byte,41)`、`buf[40]=0`（git.go:292-294）。
7. `(*Oid).toC()` 的 `unsafe.Pointer` 强转（git.go:230）：默认下零拷贝安全；实验下要求 Go 结构与 C 结构布局一致。

### 2.2 受影响文件与改造要点总表

| 文件 | 位置/接口 | 影响分类 | 改造要点 |
|---|---|---|---|
| `git.go` | `type Oid [20]byte`、`newOidFromC`、`NewOidFromBytes`、`toC`、`NewOid`、`String`、`ShortenOids` | **需修改/迁移** | 迁出到 `oid*.go`，按 build tag 分实现；宏健壮化 |
| `oid.go` (新) | 构建无关公共 API | **需新增** | `ObjectIdType`、`Cmp/Equal/Copy/IsZero/NCmp`、`ShortenOids` |
| `oid_default.go` (新) | `!git_experimental_sha256` | **需新增** | `Oid [20]byte`，保持现状语义 |
| `oid_sha256.go` (新) | `git_experimental_sha256` | **需新增** | `Oid{kind;id[32]}`，类型感知构造/格式化 |
| `wrapper.c` | 新增稳定签名 shim | **需新增 shim** | `_go_git_oid_fromstrn/_fromraw`、`_go_git_repository_init`、`_go_git_odb_hash` 等 `#ifdef` 包裹 |
| `Build_bundled_static.go` / `Build_system_dynamic.go` / `Build_system_static.go` | cgo CFLAGS | **需修改** | 实验 tag 下注入 `-DGIT_EXPERIMENTAL_SHA256` |
| `script/build-libgit2.sh` | cmake 参数 | **需修改** | 可选透传 `-DGIT_EXPERIMENTAL_SHA256=ON` |
| `indexer.go` | `NewIndexer`、`Commit` | **需修改** | shim 传 `oid_type`；`Commit` 解析支持 64 hex |
| `repository.go` | `InitRepository`、`CreateCommitFromIds`、`MergeBase*`、`lookup*` | 部分修改 | init 经 shim 传 oid_type；其余 `C.git_oid`/`toC` 经核心层自适应 |
| `odb.go` | `Hash`、`Write`、`Read`、`OdbWriteStream.Id`、`ForEach` | 多数自适应 | `Hash` 在实验下需 oid_type；其余靠核心层 |
| `merge.go` | `MergeBase*`、`git_oidarray` 遍历 | **基本不动** | 已用 `[]C.git_oid`，cgo 自动定尺寸；仅复核 |
| `graph.go` | `ReachableFromAny` | **基本不动** | 同上 |
| `blob.go` | `CreateBlobFromBuffer`、`BlobWriteStream.Commit` | **不动** | `var id C.git_oid` + `newOidFromC`，自适应 |
| `tag.go`/`note.go`/`tree.go`/`rebase.go`/`stash.go` | 回调中 `*C.git_oid` + `newOidFromC` | **不动** | 靠核心层 `newOidFromC` 修复 |
| `remote.go` | `HostkeyCertificate.HashSHA1/HashSHA256` | **不动** | 与 `git_oid` 无关（主机密钥指纹） |
| `features.go` | `FeatureSHA1/FeatureSHA256` | **不动** | 已正确；补充运行期探测说明 |

> 说明：因为大量回调与函数都经由 `newOidFromC(*C.git_oid)` 这一**单一收口点**把 C oid 转回 Go，只要把 `newOidFromC`/`toC` 这对收口函数做对，绝大多数调用点（tag/note/tree/rebase/stash/blob/merge/graph 等）**无需逐个修改**。

---

## 3. 适配方案与核心设计取舍（需求 3）

### 3.1 双构建隔离：build tag `git_experimental_sha256`

- 新增 Go build tag `git_experimental_sha256`，与 libgit2 编译期 `-DGIT_EXPERIMENTAL_SHA256=ON` **配套使用**。
- 默认（无 tag）：行为与现状 100% 一致，零破坏。
- 实验（有 tag）：启用 SHA256 能力，接受受限于该 tag 的破坏性变更。

### 3.2 `Oid` 表示推荐（对应澄清 q3）

**推荐方案**：实验构建下将 `Oid` 定义为与 C 结构布局一致的结构体：

```go
// oid_sha256.go //go:build git_experimental_sha256
type Oid struct {
    kind uint8       // 对应 C 的 type 前缀字节（git_oid_t）
    id   [32]byte    // GIT_OID_MAX_SIZE
}
```

理由与取舍：

- ✅ 仍满足可 `==` 比较、可作 map key（纯值类型）。
- ✅ 与 C `git_oid` 内存布局一致，`toC()` 仍可零拷贝强转，保留高性能路径。
- ✅ 能正确承载 type 信息，`String()`/`Bytes()` 可按类型输出 40/64 hex、20/32 字节。
- ⚠️ **破坏点**：实验构建下 `Oid` 不再是 `[20]byte`，外部对其做数组下标/切片（`oid[:]`、`oid[0:20]`）的代码会失效。该破坏**严格限定在 `git_experimental_sha256` tag 内**；默认构建用户不受任何影响。提供 `Bytes()`/`String()`/`Type()` 访问器作为替代。

**备选方案（不推荐，仅记录）**：保留 `Oid [20]byte`，SHA256 走完全独立的新类型旁路。会导致两套 oid 类型割裂、所有签名重复、调用方需分支处理，违背 KISS 与"无缝兼容"，故不采用。

### 3.3 签名差异桥接：`wrapper.c` 稳定签名 shim

cgo 无法对"同名但签名随宏变化"的 C 函数做条件调用。解决方案：在 `wrapper.c` 用 `#ifdef GIT_EXPERIMENTAL_SHA256` 提供**签名稳定**的 shim，Go 侧只调用 shim：

```c
// 示例：oid 解析。Go 侧恒定传 (out, str, n, oid_type)
int _go_git_oid_fromstrn(git_oid *out, const char *str, size_t n, int oid_type) {
#ifdef GIT_EXPERIMENTAL_SHA256
    return git_oid_fromstrn(out, str, n, (git_oid_t)oid_type);
#else
    (void)oid_type;
    return git_oid_fromstrn(out, str, n);
#endif
}
```

#### 实际落地的稳定签名 shim 全清单（在实现中通过逐轮编译验证补全）

实现过程中通过对实验构建（`-tags git_experimental_sha256` + `-DGIT_EXPERIMENTAL_SHA256`）反复编译，发现**比初始评估更多**的函数在实验宏下改变了 arity（参数被收拢进 `*_options` 结构体或新增 `git_oid_t`）。全部已在 `wrapper.c` 用 `#ifdef GIT_EXPERIMENTAL_SHA256` 包裹为稳定签名 shim：

| shim | 底层函数 | 实验下的变化 | Go 调用方 |
|---|---|---|---|
| `_go_git_oid_fromstrn` | `git_oid_fromstrn` | +`git_oid_t` | 预留（NewOid 为纯 Go） |
| `_go_git_oid_fromraw` | `git_oid_fromraw` | +`git_oid_t` | 预留 |
| `_go_git_repository_init` | `git_repository_init_ext` | opts.`oid_type` | `InitRepositoryWithOidType` |
| `_go_git_odb_new` | `git_odb_new` | +`git_odb_options*` | `NewOdb` |
| `_go_git_odb_hash` | `git_odb_hash` | +`git_oid_t` | `Odb.Hash` / `Odb.HashWithType` |
| `_go_git_odb_backend_one_pack` | `git_odb_backend_one_pack` | +`git_odb_backend_pack_options*` | `NewOdbBackendOnePack` |
| `_go_git_odb_backend_loose` | `git_odb_backend_loose` | 4 散参→`*_options` | `NewOdbBackendLoose` |
| `_go_git_indexer_new` | `git_indexer_new` | `mode`/`odb`→opts，+`oid_type` | `NewIndexer` / `NewIndexerForOidType` |
| `_go_git_index_new` | `git_index_new` | +`git_index_options*` | `NewIndex` |
| `_go_git_index_open` | `git_index_open` | +`git_index_options*` | `OpenIndex` |
| `_go_git_diff_from_buffer` | `git_diff_from_buffer` | +`git_diff_parse_options*` | `DiffFromBuffer` |

> 经验法则：凡是"在没有现成 repo/odb 可推断哈希类型的上下文中创建对象/索引/ODB"的函数，实验宏下都新增了类型入口。这类函数即使原本在 Go 侧是直接 `C.xxx` 调用，也必须改走 shim，否则实验构建因 arity 不符而无法编译。

> 关于公共头 vs sys 头的细微差别：`git_odb_backend_one_pack/loose` 在 `git2/odb_backend.h`（公共头，被 `git2.h` 包含）中重复声明，因此项目里手写的 `extern` 会与之**类型冲突**——必须删除手写 `extern`、改用 shim。而 `git2/sys/mempack.h` 等 sys 头**不被 `git2.h` 包含**，项目手写的 `extern git_mempack_new(...)` 不会触发编译冲突，但其签名在实验宏下可能已过时，存在**运行期正确性隐患**，列入后续待办。

### 3.4 收口函数的正确实现（修复 type 偏移与长度）

```go
// 默认构建：与现状一致（id 在偏移 0，20 字节）
func newOidFromC(coid *C.git_oid) *Oid {
    if coid == nil { return nil }
    oid := new(Oid)
    copy(oid[:], C.GoBytes(unsafe.Pointer(coid), C.GIT_OID_SHA1_SIZE))
    return oid
}

// 实验构建：必须从 coid.id 取地址（跳过 type 前缀），并带回类型
func newOidFromC(coid *C.git_oid) *Oid {
    if coid == nil { return nil }
    oid := new(Oid)
    oid.kind = uint8(coid._type) // 或经 shim 读取
    copy(oid.id[:], C.GoBytes(unsafe.Pointer(&coid.id[0]), C.GIT_OID_MAX_SIZE))
    return oid
}
```

### 3.5 宏健壮化

- `NewOid`：`C.GIT_OID_HEXSZ` → `C.GIT_OID_MAX_HEXSIZE`；长度校验由写死 20 改为按 hex 长度推断类型（40→SHA1，64→SHA256）。
- `ShortenOids`：`make([]byte,41)` / `buf[40]` → 基于 `C.GIT_OID_MAX_HEXSIZE` 计算。

### 3.6 兼容 `main` 分支的双 API 形态（overload vs `_ext`/`_from_`）

为同时兼容 pin 的 1.9.4（overload 形态）与 libgit2 `main`（`_ext`/`_from_` 形态），且因两者版本号无法区分（§1.6），采用**显式构建标签 + 嵌套预处理门控**：

- **新增构建标签 `libgit2_next`**（文件 `libgit2_next.go`）：注入 `-DGIT2GO_LIBGIT2_OID_EXT_API=1`，与 `git_experimental_sha256` 配套使用。
- **`wrapper.c` 嵌套门控**：在既有 `#ifdef GIT_EXPERIMENTAL_SHA256` 之内再按 `#if defined(GIT2GO_LIBGIT2_OID_EXT_API)` 分叉：
  - **未定义（默认）**：走 1.9.x overload 调用（`git_oid_fromstrn(...,type)`、`git_odb_new(out,opts)`、`git_odb_hash(...,oid_type)`、`git_index_new(out,opts)`、`git_diff_from_buffer(...,opts)`）——即已端到端测试的路径。
  - **已定义（main）**：走新函数（`git_oid_from_prefix`/`git_oid_from_raw`、`git_odb_new_ext`、`git_index_new_ext`/`git_index_open_ext`、`git_diff_from_buffer_ext`）。
- **形态相同、无需分叉**：`git_repository_init_ext`（`opts.oid_type`）、`git_indexer_new`（重载）在两分支一致，不加 `_ext` 分支。经复核 main 本地头文件，`git_odb_backend_one_pack`（`git_odb_backend_pack_options*`）与 `git_odb_backend_loose`（`git_odb_backend_loose_options*`）在 main 与 1.9.4 实验态**形态一致**，同一实验分支通用。
- **`git_odb_hash` 的 main 特例（已落地）**：main 上 `git_odb_hash` 弃用且不带 `git_oid_t`，故 main 分支改走 `git_object_id_from_buffer(oid, buf, len, git_object_id_options*)`，其 options 携带 `object_type` 与 `oid_type`，使 `HashWithType(SHA256)` 在 main 上**真正生效**。
- **`git_repository_oid_type`（main 新增只读 getter，已绑定）**：main 无门控暴露 `git_repository_oid_type()`（返回 `git_oid_t`）；1.9.x 无此符号。经 `_go_git_repository_oid_type` shim + `GIT2GO_HAVE_REPO_OID_TYPE`（由 `libgit2_next` 注入）门控：main 返回真实类型，旧库回退 SHA1。Go 层暴露 `(*Repository).OidType() ObjectIdType`（`sha256_api.go`）。
- **构建脚本探测提示**：`build-libgit2.sh` 在实验构建后 `grep` 头文件是否含 `git_oid_from_string`，据此打印应使用 `make test-static-sha256` 或 `make test-static-sha256-next`。

**使用方式**：
```sh
# 针对 1.9.x（overload，本项目原 pin，已测试）
make test-static-sha256          # -tags "static git_experimental_sha256"
# 针对 libgit2 main（_ext/_from_）
make test-static-sha256-next     # -tags "static git_experimental_sha256 libgit2_next"
```

> 验证边界：`libgit2_next` 路径最初依据 `ddf3b5c8` 的本地头文件完成签名核实；子模块现已更新并固定到 **转正后的 `main @ 939362a3`**。在该基线上，`git_oid_from_prefix/from_raw`、`git_odb_new_ext`、`git_index_new_ext/open_ext`、`git_diff_from_buffer_ext`、`git_object_id_from_buffer`、`git_repository_oid_type` 均已成为无实验宏门控的正式 API；通过常规 `libgit2.pc` 接线后，全量测试已实机通过。1.9.x overload 兼容路径也已单独复验通过。

---

### 3.7 ABI 错配守卫与跨构建 API 对称性

#### 3.7.1 双向 ABI 守卫（防静默数据错乱）

`git_oid` 的内存布局在实验宏下改变（`[20]byte` → `{ type; id[32] }`），因此 **build tag 与 libgit2 实际 ABI 必须一致**，否则 `newOidFromC()`/`toC()` 会在无任何报错的情况下产出错误的 object id（读到 type 字节 + 前 19 字节）并越界读。两个方向都已设编译期守卫：

| 错配组合 | 守卫位置 | 行为 |
|---|---|---|
| 有 tag、库**无**实验宏 | `Build_bundled_static_sha256.go` | `#error "git_experimental_sha256 build tag requires a libgit2 built with -DEXPERIMENTAL_SHA256=ON"` |
| **无** tag、库**有**实验宏 | `sha256_default.go` | `#error "this libgit2 was built with -DEXPERIMENTAL_SHA256=ON; rebuild git2go with -tags git_experimental_sha256"` |

后者尤为重要：它覆盖 **system 构建**场景（系统/发行版的 libgit2 若带实验宏，默认构建将静默错乱）。另在两个 `oid_*.go` 的 `init()` 中以 `unsafe.Sizeof(Oid{}) != unsafe.Sizeof(C.git_oid{})` 做运行期兜底，应对头文件与实际链接库不一致的极端情况。

守卫已实测触发（`CGO_CFLAGS=-DGIT_EXPERIMENTAL_SHA256=1 go build ./...` 在默认构建下正确编译失败并给出可操作提示）。

#### 3.7.2 跨构建 API 对称（同一份源码两种构建均可编译）

SHA256-aware 的公共 API 在**两种构建下同名同签名**存在，差别仅在语义：实验构建真实支持 SHA256；默认构建对 `ObjectIdSHA256` 返回**显式错误**（`ErrorClassInvalid`/`ErrorCodeInvalid`），不静默降级为 SHA1。

| API | 实验构建（`sha256_api.go`） | 默认构建（`sha256_default.go`） |
|---|---|---|
| `(*Oid).Type() ObjectIdType` | 真实类型 | 恒为 `ObjectIdSHA1` |
| `(*Repository).OidType() ObjectIdType` | `git_repository_oid_type()`（main）/ SHA1 回退 | 恒为 `ObjectIdSHA1` |
| `NewOidFromBytesWithType([]byte, ObjectIdType) (*Oid, error)` | SHA1/SHA256 均支持，长度不足报错 | SHA256 报错 |
| `InitRepositoryWithOidType(path, isBare, ObjectIdType)` | 支持 SHA256 仓库 | SHA256 报错 |
| `(*Odb).HashWithType(data, ObjectType, ObjectIdType)` | 类型化哈希 | SHA256 报错 |
| `NewIndexerForOidType(path, odb, ObjectIdType, cb)` | 支持 SHA256 packfile | SHA256 报错 |

这样使用者可以写一份"SHA256-aware"的代码，在默认构建下编译通过并在运行期得到清晰的能力缺失错误，而不是编译失败或静默错值。


### 4.1 技术难点

1. **`type` 前缀字节的偏移陷阱**：最隐蔽的 bug 来源。任何从 `git_oid` 首地址直接 memcpy/GoBytes 20 字节的代码，在实验构建下都会静默读偏。必须统一改为从 `&coid.id[0]` 取址。
2. **cgo 访问匿名/保留字字段**：C 字段名 `type` 是 Go 关键字，cgo 中以 `_type` 访问；需验证 pin 版本下的实际可访问性，必要时用 C shim getter/setter 读写 type。
3. **签名分叉的维护成本**：每个新增 oid_type 入参的 API 都要配一个稳定 shim，shim 数量会增长；需建立统一命名与注释规范。
4. **运行期 vs 编译期能力错配**：`Features()&FeatureSHA256` 是**运行期**探测，而 build tag 是**编译期**开关。二者可能不一致（如编译开了 tag 但链接的 libgit2 未启用）。需在文档/初始化中明确：tag 决定绑定面，运行期特性决定实际可用性。
5. **测试数据**：`testdata/` 现有 pack/idx 均为 SHA1；SHA256 路径缺乏夹具，需新增或在测试中动态构造 SHA256 仓库。

### 4.2 风险与缓解

| 风险 | 缓解 |
|---|---|
| 实验 tag 下静默读偏 type 字节 | 统一收口 `newOidFromC`/`toC`；增加 round-trip 单测（已端到端实跑验证） |
| `DEPRECATE_HARD` 导致 `GIT_OID_HEXSZ` 缺失 | 迁移到非弃用宏 |
| 默认构建被误伤 | 所有 SHA256 改动放入 tag 文件；CI 跑默认 + 实验两套 |
| `oidarray` 步长错算 | 坚持使用 `C.git_oid` 类型让 cgo 自动定尺寸，禁止手写 20/32 |
| **实验 install 的 `-experimental` 布局** | 见下方专项说明 |
| **build tag 与库 ABI 错配导致静默错值** | 双向编译期 `#error` 守卫 + `init()` 尺寸断言，见 §3.7.1 |

#### 实验构建的安装布局陷阱（落地实测发现）

以 `-DEXPERIMENTAL_SHA256=ON` 构建的 libgit2，安装产物**全部带 `-experimental` 后缀且不再安装常规头**：

- 静态库：`libgit2-experimental.a`（而非 `libgit2.a`）
- 头目录：`include/git2-experimental/`，伞头 `git2-experimental.h`（**不安装** `git2.h` / `git2/`）
- pkg-config：`libgit2-experimental.pc`
- 伞头 `git2-experimental.h` 内部 `#define GIT_EXPERIMENTAL_SHA256 1`，故**包含该头即自动选中实验 ABI**，无需手动 `-D`（`sha256_experimental.go` 的 `-D` 注入退化为对 system/dynamic 构建的同值冗余定义，无害）。

由于 git2go 全仓共享的 cgo 文件（`wrapper.c` 等）统一 `#include <git2.h>` 与 `#include <git2/sys/...>`，直接链接实验产物会找不到头。**采用零侵入方案**：`build-libgit2.sh` 在实验静态构建后自动创建兼容符号链接 `git2.h → git2-experimental.h`、`git2 → git2-experimental`，使共享 cgo 文件无改动即可解析到实验头；实验专属构建文件 `Build_bundled_static_sha256.go`（tag：`static && !system_libgit2 && git_experimental_sha256`）链接 `-lgit2-experimental`、走 `libgit2-experimental.pc`，并通过 `#ifndef GIT_EXPERIMENTAL_SHA256 #error` 守卫确保 tag 与库一致。默认 `Build_bundled_static.go` 约束增补 `&& !git_experimental_sha256` 以互斥。

### 4.3 sys 头部入口审计结论（已逐一核对 pin commit `f7164261`）

对经 `git2/sys/*.h` 手写 `extern` 引入的入口（不被 `git2.h` 包含、故编译期不冲突，但运行期可能因 arity 错位出错）逐一核对了 pin commit 的头文件源码，结论如下：

| sys 头 / 入口 | 是否含 `#ifdef GIT_EXPERIMENTAL_SHA256` | arity / 类型变化 | 处理结论 |
|---|---|---|---|
| `mempack.h`：`git_mempack_new` | 否 | 无 | **无需改动**（签名稳定） |
| `mempack.h`：`git_mempack_dump`/`reset`/`write_thin_pack`/`object_count` | 否 | 无 | **无需改动** |
| `refdb_backend.h`：`git_refdb_backend_fs`/`git_refdb_set_backend`/`git_refdb_init_backend` | 否 | 无 | **无需改动** |
| `refdb_backend.h`：backend struct 的 `write`/`del`/`unlock` 回调（传 `const git_oid*`） | 否 | 仅指针，尺寸不变 | **无需改动**（cgo 自动适配指针） |
| `refdb.h`：`git_refdb_new` | 否 | 无 | **无需改动** |
| `transport.h`：`git_transport_smart`/`git_transport_new`/`negotiate_fetch`/`shallow_roots` | 否（均在 ifdef 之外） | 无 | **无需改动** |
| `transport.h`：`git_transport` struct **新增** `oid_type` 回调成员 | 是（唯一一处） | 新增可选回调字段 | **无需改动**，且**无需实现**：见下方专项说明 |

> 结论：在 pin commit `f7164261` 下，mempack / refdb / transport 三类 sys 入口**均无破坏性 arity 变化，无需新增 shim**。

#### `git_transport.oid_type` 由 libgit2 内置smart transport 提供（已核实，git2go 无需实现）

早期基于头文件的推断曾把该回调列为 git2go 的"可选增强项"，**这个判断是错的**，经核对上游实现后更正如下：

git2go 的 transport 扩展点是 **smart subtransport**，而不是 `git_transport` 本身——`wrapper.c` 的 `_go_git_transport_smart()` 只是填充 `git_smart_subtransport_definition` 后调用 **libgit2 内置的** `git_transport_smart()`；`RegisterManagedHTTPTransport` / `RegisterManagedSSHTransport` 注册的同样是 subtransport。也就是说 git2go **从不自己实现 `git_transport` 结构体**。

而 libgit2 的内置 smart transport 已经实现了该回调（`src/libgit2/transports/smart.c`）：

```c
#ifdef GIT_EXPERIMENTAL_SHA256
static int git_smart__oid_type(git_oid_t *out, git_transport *transport)
{
    if (t->caps.object_format == NULL)
        *out = GIT_OID_DEFAULT;
    else
        *out = git_oid_type_fromstr(t->caps.object_format);
    ...
}
...
t->parent.oid_type = git_smart__oid_type;   /* 内置装配 */
#endif
```

即远端对象格式协商（读取远端 `object-format` capability）完全在 libgit2 内部完成，git2go 的 subtransport 只负责搬运字节流、与 oid 类型无关。**结论：SHA256 远端协商在实验构建下开箱可用，git2go 侧无代码工作量。**

> 仍需关注（编译期已自动暴露、本次阶段一已处理完毕的）公共头入口：`odb_backend.h` 的 `git_odb_backend_one_pack/loose`、`odb.h` 的 `git_odb_new/hash`、`index.h` 的 `git_index_new/open`、`diff.h` 的 `git_diff_from_buffer`、`indexer.h` 的 `git_indexer_new` —— 这些因被 `git2.h` 包含、实验宏下 arity 变化会**编译报错**，已全部改走 wrapper.c 稳定签名 shim。

#### 可选增强项（非必须，跟随需求推进）

-暂无。（原列于此处的"为 managed smart transport 实现 `git_transport.oid_type`"经核实为**误判**，该回调由 libgit2 内置 smart transport 提供，git2go 无需实现，详见上文专项说明。）

### 4.4 实施路线图

1. **阶段一（本次，已完成并验证）**：双构建骨架 + `Oid` 核心层 + 全量稳定签名 shim + 关键模块贯通 + 编译验证（默认构建编译+单测通过；实验构建编译通过）。
2. **阶段二**：SHA256 仓库端到端用例（init→write→commit→lookup），补 `testdata` 夹具，并在真正以 `-DEXPERIMENTAL_SHA256=ON` 构建的 libgit2 上运行实验单测。
3. **阶段三（sys 头审计已完成，见 4.3）**：经核对 mempack/refdb/transport 三类 sys 入口在 pin 版本下无破坏性变化、无需 shim；剩余 `git_odb_open`、`git_odb_hashfile` 等类型相关 API 视端到端需要再贯通。
4. **阶段四**：跟随上游——当 libgit2 将 SHA256 转正（去掉实验宏）时，合并双实现、收敛 API、更新版本守卫（触发条件与收敛清单见 4.6）。

### 4.5 本次落地的验证结论

三条路径均已在**真实库**上端到端实跑通过：

- **默认（SHA1-only）构建**：`go build ./...`（链接系统 libgit2 1.9.4）通过；`oid_test.go` 全部用例通过；`TestOidZero` 等不回退。默认路径的 shim 走 legacy 无类型函数（`git_oid_fromstrn(out,str,len)` 等），这些在 1.9.x 与 `main` 上签名一致，故对两种子模块基线都兼容。
- **前瞻推荐：libgit2 `main`（当前 pin：`939362a3`）+ `libgit2_next` 代码路径**：通过常规 `libgit2.pc` 接线后全量测试通过——包含 `TestSHA256RepositoryOidType`（`git_repository_oid_type`）、`TestSHA256RepositoryOdbWrite`（`git_object_id_from_buffer` 类型化哈希生效）、`TestSHA256RepositoryCommitRoundTrip`（`git_oid_from_prefix`/`_ext` 全链路 round-trip）、`TestSHA256IndexerForOidType` 及 oid 全套。注意该 main 已将 SHA256 转正，正式收敛方案见 §4.8。
- **兼容：libgit2 1.9.4 overload**：`make test-static-sha256` 全部通过（正确跳过 `libgit2_next` 门控的 `OidType` 用例）。
- **实测发现并修复的 `main` 构建差异**：`USE_HTTPS=OFF` 时 `main` 需显式 `-DUSE_NTLMCLIENT=OFF -DUSE_GSSAPI=OFF`，否则 cmake configure / arm64 链接失败（缺 `gss_*` 符号）；已并入 `build-libgit2.sh`（对 1.9.x 亦安全）。
- 受影响但**无需改动**的文件：`merge.go`/`graph.go`（`[]C.git_oid` 由 cgo 自动按真实结构体尺寸计算步长）、`repository.go` 的 `CreateCommitFromIds`（指针数组步长用 `unsafe.Sizeof` 指针尺寸）、`tag.go`/`note.go`/`tree.go`/`rebase.go`/`stash.go`（回调经统一收口的 `newOidFromC` 自适应）。

### 4.5.1 测试策略

- **默认构建**：`make test-static`，保证现有用例 0 回退。
- **实验构建**：新增 `-tags "static git_experimental_sha256"`（需配套 libgit2 以 `-DGIT_EXPERIMENTAL_SHA256=ON` 构建），运行 oid round-trip、SHA256 仓库基本读写用例。
- **oid 单测**：SHA1/SHA256 各覆盖 `NewOid`(40/64 hex)、`String()`、`Bytes()`、`Cmp/Equal/IsZero`、`newOidFromC↔toC` round-trip。

### 4.6 跟随上游转正：触发条件与收敛清单（阶段四预案）

SHA256 在上游"转正"后，`git_oid` 的 ABI 与解析函数 arity 将统一，届时本项目的双实现可收敛为单实现。此项为**触发式**任务，现在不执行，仅预置触发条件与收敛清单。

**触发条件（满足任一即评估收敛）：**

1. 上游 libgit2 **移除 `GIT_EXPERIMENTAL_SHA256` 宏门控**，SHA256 成为默认编译能力；或
2. 上游将 `git_oid` 默认结构统一为 `{ unsigned char type; unsigned char id[32] }`（无论是否启用宏）；或
3. 上游把 `git_oid_fromstr*`/`git_odb_*`/`git_indexer_new` 等的"带 `git_oid_t`"签名提升为**唯一**签名（不再有无类型旧签名）。

**收敛清单（届时执行）：**

| 收敛动作 | 涉及文件 |
|---|---|
| 合并 `oid_default.go` + `oid_sha256.go` 为单一实现（统一为带类型的 32 字节表示） | `oid.go`/`oid_default.go`/`oid_sha256.go` |
| 删除 `wrapper.c` 中所有 `#ifdef GIT_EXPERIMENTAL_SHA256` 分支，shim 仅保留转正后唯一签名 | `wrapper.c` |
| 将 `sha256_api.go` 的方法并入主 API（去掉 tag 门控），或保留为类型感知的常规 API | `sha256_api.go` → 合入 `repository.go`/`odb.go`/`indexer.go` |
| 删除默认构建的对称降级实现与双向 ABI 守卫（转正后不再有 ABI 分叉） | `sha256_default.go`、`Build_bundled_static_sha256.go` |
| 移除 `git_experimental_sha256` build tag 与 `sha256_experimental.go` 的 CFLAGS 注入 | `sha256_experimental.go`、各 `*_test.go` 的 tag 头 |
| 移除 `build-libgit2.sh` 的 `EXPERIMENTAL_SHA256` 开关（变为默认） | `script/build-libgit2.sh` |
| 更新版本守卫 `LIBGIT2_VER_MINOR` 至转正版本 | `Build_bundled_static.go`、`Build_system_dynamic.go` |

**定位辅助：** 所有需在阶段四处理的点均以 `// TODO(sha256-merge):` 注释标注，转正时可一键 `grep` 定位。建议在版本守卫处加一条提示：当检测到 libgit2 版本 ≥ 转正版本时，于编译期输出"实验路径可合并"的告警。

### 4.7 兼容性完整度自查（转正收敛后）

#### 已完整覆盖

- **SHA1 保留且默认**：40-hex/20-byte oid、默认仓库、默认 ODB/index/diff/indexer 均保持 SHA1。
- **SHA256 本地全生命周期**：仓库 init → ODB buffer/file hash → write/read → standalone ODB +
  loose backend → index → tree → commit → lookup → packbuilder → indexer commit（64-hex pack id）→
  one-pack backend 读取。
- **远端路径**：SHA256 clone/fetch、pack 索引与 64-hex ref target 已验证；对象格式协商由
  libgit2 内置 smart transport 的 `git_smart__oid_type` 完成。
- **表示与比较**：构造、解析、`String`、`Bytes`、`Type`、`Cmp`、`NCmp`（nibble 语义）、
  `Equal`、`IsZero`、`ShortenOids` 均按类型处理。
- **C 入口**：oid 解析、ODB/backend、index、diff、indexer、repository init 全部统一走 promoted
  typed/options/`_ext` API，无旧 overload 分支。
- **构建守卫**：除 SHA256 尺寸宏外，同时检查 object-id/index/diff options 版本宏，避免旧
  experimental 1.9.x 头文件仅因含 SHA256 宏而误通过。

#### 已知边界

| 边界 | 说明 |
|---|---|
| 文件 hash 不应用 repository filters | `HashFile` / `HashFileWithType` 对应 `git_object_id_from_file`，按上游语义只哈希 raw content；需要 attributes/CRLF filters 时应使用 repository-aware hash API（后续可单独绑定） |
| push 未使用真实网络服务端做端到端测试 | clone/fetch/pack 协商及 local receive-pack push 已覆盖；外部 SSH/HTTP push 依赖服务端环境 |
| 正式版本号尚未发布 | 当前以能力宏守卫；正式发布后执行 §4.8 第二步 |
| `Oid` 不再是 `[20]byte` | 属明确 breaking change；迁移见 `sha256-breaking-changes.md` |

#### 结论

SHA1 与 SHA256 在同一构建中共存，SHA1 仍为默认；SHA256 的本地仓库、standalone ODB、
loose/pack backend、index/diff/indexer 与 clone/fetch 主链路均有真实端到端用例。

---

### 4.8 转正后的两步收敛方案（阶段四）

§1.8 确认 SHA256 已转正，§4.6 的触发条件 1 与 3 同时满足。SHA1 **没有被移除**：
`GIT_OID_SHA1`、SHA1 尺寸宏与 SHA1-only 便利解析函数仍在，`GIT_OID_DEFAULT` 仍为 SHA1。
收敛的是 ABI 与构建路径，不是删除 SHA1。

#### 第一步：代码与构建收敛（本分支已完成）

- `Oid` 统一为带 type 的 32 字节结构，运行时同时表示 SHA1/SHA256；SHA1 仍为默认。
- `wrapper.c` 删除实验宏/next 双分支，仅保留转正后的 typed / `_ext` API。
- 删除 `oid_default.go`、`sha256_default.go`、`sha256_experimental.go`、`libgit2_next.go`、
  `Build_bundled_static_sha256.go`；类型感知 API 与测试全部去 build tag。
- 常规 `Build_bundled_static.go` / `libgit2.a` / `libgit2.pc` 成为唯一 bundled-static 路径。
- `build-libgit2.sh` 移除实验开关、实验布局软链与 API 形态探测，保留 in-tree `-I` 优先修复。
- CI 删除实验专属 job 与已失效的宏错配守卫；常规 static job覆盖 SHA1 与 SHA256 全量用例。
- system/static/dynamic 构建增加 `GIT_OID_SHA256_SIZE` 能力守卫，旧 20 字节 `git_oid` ABI
  会在编译期给出明确错误。

#### 第二步：v36-pre → v36.0.0（等待上游正式版本号）

- **已确定**：module 主版本为 v36，路径为 `github.com/libgit2/git2go/v36`；上游正式发布前
  使用 v36-pre 阶段与 `v36.0.0-pre.N` 标签。v35 继续对应 libgit2 1.9.x。
- 上游 main 的 `version.h` 目前仍报告 1.9.0，故暂时保留 `LIBGIT2_VER_MAJOR/MINOR == 1.9`
  版本范围；上游发布包含转正 SHA256 的正式版本后，更新三个 `Build_*.go` 的版本守卫。
- 在最终 Linux / macOS / Windows CI 上验证正式发布包的 dynamic / system-static / bundled-static。
- 将 `docs/sha256-breaking-changes.md` 的清单整理进正式 CHANGELOG / release notes，然后发布
  `v36.0.0`。

完整 breaking changes 与迁移示例见 `docs/sha256-breaking-changes.md`。

---

## 5. 迁移指南（面向使用者）

本轮为**破坏性 API 变更**，包括只使用 SHA1 的调用方：`Oid` 已从 `[20]byte` 改为 typed
结构体，因此数组索引、切片、固定长度转换、反射/unsafe 内存布局依赖都必须迁移。

- SHA1 没有被移除且仍为默认：`InitRepository`、`NewOidFromBytes`、`NewOdb`、`NewIndex`、
  `NewIndexer` 等默认 API 继续使用 SHA1。
- 使用 `oid.Bytes()` 取有效原始字节（SHA1=20、SHA256=32），`oid.String()` 取 hex
  （SHA1=40、SHA256=64），`oid.Type()` 取类型。
- 创建 SHA256 仓库使用 `InitRepositoryWithOidType(..., ObjectIdSHA256)`；standalone ODB、
  loose/one-pack backend、index、diff、indexer 均提供对应 `WithOidType` API。
- 构建不再使用 `EXPERIMENTAL_SHA256`、`git_experimental_sha256` 或 `libgit2_next`；常规
  `libgit2` 构建同时提供 SHA1 与 SHA256。

完整 breaking changes、Changelog 摘要与迁移示例见 `sha256-breaking-changes.md`。
