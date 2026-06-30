# git2go SHA1 / SHA256 兼容性适配设计文档

> 适用范围：`github.com/libgit2/git2go/v35`（对应 libgit2 1.9.x）
> 上游基线：libgit2 子模块 pin 在 commit `f7164261c9bc0a7e0ebf767c584e5192810a8b24`
> 目标：在**不破坏默认（SHA1-only）构建**的前提下，通过 cgo build tag + `wrapper.c` 条件 shim，为开启 `GIT_EXPERIMENTAL_SHA256` 的 libgit2 提供 SHA256 绑定支持。

---

## 1. 上游 SHA256 API 评估（需求 1）

### 1.1 总体结论

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

> 注：本项目非 system 构建脚本 `script/build-libgit2.sh` 默认 `DEPRECATE_HARD=ON`，因此**继续依赖 `GIT_OID_HEXSZ` 存在编译断裂风险**，应迁移到 `GIT_OID_SHA1_HEXSIZE` / `GIT_OID_MAX_HEXSIZE`。

### 1.5 函数签名变化（仅在实验宏下新增 `git_oid_t type` 入参）

| 函数 | 默认签名 | 实验签名 |
|---|---|---|
| `git_oid_fromstr` | `(out, str)` | `(out, str, type)` |
| `git_oid_fromstrp` | `(out, str)` | `(out, str, type)` |
| `git_oid_fromstrn` | `(out, str, len)` | `(out, str, len, type)` |
| `git_oid_fromraw` | `(out, raw)` | `(out, raw, type)` |

格式化/比较类（`git_oid_fmt`、`git_oid_nfmt`、`git_oid_pathfmt`、`git_oid_tostr`、`git_oid_cpy`、`git_oid_cmp`、`git_oid_equal`、`git_oid_ncmp`、`git_oid_is_zero`、`git_oid_shorten_*`）签名不变，但语义按 oid 类型自适应长度。

### 1.6 其它新增 oid 类型选项的 API

- `git_repository_init_options.oid_type`（经 `git_repository_init_ext`），用于创建 SHA256 仓库。
- `git_odb_options.oid_type` / `git_odb_new(..., opts)`、`git_odb_hash` 系列在实验下需要/接受类型信息。
- `git_indexer_options.oid_type`（本项目 `wrapper.c:_go_git_indexer_new` 已使用 `git_indexer_options`，仅未设置 `oid_type`）。

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

---

## 4. 后续适配重点、技术难点与路线图（需求 4）

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
| `transport.h`：`git_transport` struct **新增** `oid_type` 回调成员 | 是（唯一一处） | 新增可选回调字段 | **无需改动**：该回调由 transport 实现者按需提供，git2go 现有 smart transport 不实现它即可；属"可增强"而非"必适配" |

> 结论：在 pin commit `f7164261` 下，mempack / refdb / transport 三类 sys 入口**均无破坏性 arity 变化，无需新增 shim**。唯一与 SHA256 相关的是 `git_transport` 的可选 `oid_type` 回调——它是面向自定义 transport 的**能力增强点**（用于向远端协商对象类型），列为下方"可选增强项"，不阻塞双构建编译与现有功能。

> 仍需关注（编译期已自动暴露、本次阶段一已处理完毕的）公共头入口：`odb_backend.h` 的 `git_odb_backend_one_pack/loose`、`odb.h` 的 `git_odb_new/hash`、`index.h` 的 `git_index_new/open`、`diff.h` 的 `git_diff_from_buffer`、`indexer.h` 的 `git_indexer_new` —— 这些因被 `git2.h` 包含、实验宏下 arity 变化会**编译报错**，已全部改走 wrapper.c 稳定签名 shim。

#### 可选增强项（非必须，跟随需求推进）

- 自定义 SHA256 远端协商：为 git2go 的 managed smart transport 实现 `git_transport.oid_type` 回调，使其在实验构建下能向远端声明/协商对象类型。当前不实现不影响 SHA256 本地仓库的 init/读写。

### 4.4 实施路线图

1. **阶段一（本次，已完成并验证）**：双构建骨架 + `Oid` 核心层 + 全量稳定签名 shim + 关键模块贯通 + 编译验证（默认构建编译+单测通过；实验构建编译通过）。
2. **阶段二**：SHA256 仓库端到端用例（init→write→commit→lookup），补 `testdata` 夹具，并在真正以 `-DEXPERIMENTAL_SHA256=ON` 构建的 libgit2 上运行实验单测。
3. **阶段三（sys 头审计已完成，见 4.3）**：经核对 mempack/refdb/transport 三类 sys 入口在 pin 版本下无破坏性变化、无需 shim；剩余 `git_odb_open`、`git_odb_hashfile` 等类型相关 API 视端到端需要再贯通。
4. **阶段四**：跟随上游——当 libgit2 将 SHA256 转正（去掉实验宏）时，合并双实现、收敛 API、更新版本守卫（触发条件与收敛清单见 4.6）。

### 4.5 本次落地的验证结论

- 默认构建：`go build ./...`（链接系统 libgit2 1.9.4）通过；新增 `oid_test.go` 全部用例通过；现有 `TestOidZero` 等不回退。
- 实验构建（**已端到端实跑通过**）：以 `EXPERIMENTAL_SHA256=ON ./script/build-libgit2.sh --static` 在 worktree 内构建实验静态 libgit2 1.9.4，`go test -tags "static git_experimental_sha256" -run SHA256 .` 全部通过：
  - `TestSHA256RepositoryOdbWrite`：odb 写出 32 字节 SHA256 oid；`Odb.HashWithType` 与 `Write` 返回 oid 一致；读回数据 round-trip 一致。
  - `TestSHA256RepositoryCommitRoundTrip`：index→tree→commit 各 oid 均 64-hex SHA256；`LookupCommit` 与 `NewOid` 重解析 round-trip 一致（**实证 type 前缀偏移处理正确**）。
  - `TestSHA256IndexerForOidType`、`TestNewOidSHA256`、`TestSHA256OidIsZero` 通过。
- 受影响但**无需改动**的文件：`merge.go`/`graph.go`（`[]C.git_oid` 由 cgo 自动按真实结构体尺寸计算步长）、`repository.go` 的 `CreateCommitFromIds`（指针数组步长用 `unsafe.Sizeof` 指针尺寸）、`tag.go`/`note.go`/`tree.go`/`rebase.go`/`stash.go`（回调经统一收口的 `newOidFromC` 自适应）。

### 4.5 测试策略

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
| 移除 `git_experimental_sha256` build tag 与 `sha256_experimental.go` 的 CFLAGS 注入 | `sha256_experimental.go`、各 `*_test.go` 的 tag 头 |
| 移除 `build-libgit2.sh` 的 `EXPERIMENTAL_SHA256` 开关（变为默认） | `script/build-libgit2.sh` |
| 更新版本守卫 `LIBGIT2_VER_MINOR` 至转正版本 | `Build_bundled_static.go`、`Build_system_dynamic.go` |

**定位辅助：** 所有需在阶段四处理的点均以 `// TODO(sha256-merge):` 注释标注，转正时可一键 `grep` 定位。建议在版本守卫处加一条提示：当检测到 libgit2 版本 ≥ 转正版本时，于编译期输出"实验路径可合并"的告警。

---

## 5. 迁移指南（面向使用者）

- 默认（SHA1-only）用户：**无需任何改动**。
- 启用 SHA256（`git_experimental_sha256` tag）用户：
  - 不要再假设 `Oid` 是 `[20]byte`；用 `oid.Bytes()` 取原始字节、`oid.String()` 取 hex、`oid.Type()` 取类型。
  - 创建 SHA256 仓库使用带 `oid_type` 的 init API；`NewOid` 可直接解析 64 位 hex。
  - 同一仓库内 oid 类型一致；跨类型比较无意义。
