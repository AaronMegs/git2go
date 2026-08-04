# §5.4 长期项调研报告：反向特性探测、per-worktree 引用、SHA-256 + reftable

> 调研日期：2026-08-03（初版，对照上游 `d29fe50de`）；2026-08-04（复核，对照上游 `939362a3c`）
> 本项目分支：`feat-reftable`
> 当前 vendor：libgit2 main `ddf3b5c85`
> 上游 main HEAD：`939362a3c`（领先 vendor 26 个 commit）

本报告针对 `docs/reftable-research.md` §5.4 的三项长期项做上游状态核查、可行性分析与落地方案。

> **2026-08-04 复核结论**：上游从 `d29fe50de` 前进到 `939362a3c`（新增 4 个 commit：PR #7331 `patch: accept empty-file diffs with no hunk`、PR #7332 `index: fix discarded insertion position in D/F conflict check`）。两者均为与本议题无关的 bugfix，仅触及 `src/libgit2/index.c`、`src/libgit2/patch_parse.c` 及对应测试，**未触及** reftable / oid / feature / worktree 任何代码。**三项结论全部维持不变**，本次复核另补充了 §3.3.1 的宏定义细节。

---

## 0. 执行摘要

| 项 | 上游状态 | 结论 | 建议优先级 |
| --- | --- | --- | --- |
| **C3 反向特性探测**（`GIT_FEATURE_REFTABLE`） | ❌ 仍未提供 | 无法推进；现有 `IsReftableSupported()` 已是最优方案 | 冻结，持续观察 |
| **C4 per-worktree 引用语义** | ❌ 仍为内部 API | 无法推进；不应绑定内部符号 | 冻结，持续观察 |
| **C5 SHA-256 + reftable** | ✅ **已转正**（PR #7261） | **可推进**，但触发 git2go **`Oid` ABI 破坏性变更** | **高**，但需重大重构 |

**最重要发现**：上游 main 已将 SHA256 从实验性转正（`GIT_EXPERIMENTAL_SHA256` 宏从公开头文件完全移除），导致 `git_oid` 结构由 20 字节变为 **33 字节**。git2go 现有的 `type Oid [20]byte` + `unsafe.Pointer` 强转将产生**内存越界**。这是升级 vendor 到最新 main 之前必须解决的阻塞问题。

**已落地的防护与铺垫**（详见 §3.4）：

| 交付 | 内容 |
| --- | --- |
| ✅ 编译期 ABI 守卫 | `git2go_version_check.h` 新增 `#if GIT_OID_MAX_SIZE != 20`，把"运行时内存损坏"降级为"构建期明确报错"，三种链接方式均受保护；已双向验证 |
| ✅ oid type 探测 API | `oid_type.go`：`OidType` 枚举（含 `String()` / `Size()` / `HexSize()`）、`Repository.OidType()`、`IsSha256Supported()`，6 个测试全通过 |
| ✅ 影响面审计 | [oid-refactor-audit.md](./oid-refactor-audit.md)：1 处强转、44 处 `toC()` 调用（**20 处OUT 参数**）、9 处布局依赖；实测布局确认后**推荐方案改为 A** |
| ⬜ 阻塞项 | `Oid` 类型重构（唯一的SHA256 前置，需独立里程碑 + v36 major bump） |

---

## 1. C3：反向特性探测（`GIT_FEATURE_REFTABLE`）

### 1.1 上游现状

核查 `origin/main`（`d29fe50de`）的 `include/git2/common.h`，`git_feature_t` 枚举完整列表：

```c
GIT_FEATURE_THREADS        = (1 << 0),
GIT_FEATURE_HTTPS          = (1 << 1),
GIT_FEATURE_SSH            = (1 << 2),
GIT_FEATURE_NSEC           = (1 << 3),
GIT_FEATURE_HTTP_PARSER    = (1 << 4),
GIT_FEATURE_REGEX          = (1 << 5),
GIT_FEATURE_I18N           = (1 << 6),
GIT_FEATURE_AUTH_NTLM      = (1 << 7),
GIT_FEATURE_AUTH_NEGOTIATE = (1 << 8),
GIT_FEATURE_COMPRESSION    = (1 << 9),
GIT_FEATURE_SHA1           = (1 << 10),
GIT_FEATURE_SHA256         = (1 << 11),
GIT_FEATURE_HTTP           = (1 << 12)
```

- **没有 `GIT_FEATURE_REFTABLE`**，枚举最大值仍为 `GIT_FEATURE_HTTP`。
- `git_libgit2_opt_t` 中亦无 reftable 相关选项。
- 结论与前两轮调研一致：**上游至今未提供 reftable 的特性标志**。

### 1.2 分析

reftable 在 libgit2 的定位是**编译期始终可用的内置后端**（源码位于 `src/libgit2/refdb_reftable.c` + `deps/reftable`，无 CMake 开关将其排除），因此上游可能**根本不打算**为其提供 `GIT_FEATURE_*` 标志——feature 标志的语义是"该构建是否编译进了某可选依赖"（如 HTTPS/SSH 后端），而 reftable 不属于此类。

这意味着：真正需要区分的不是"本构建是否支持 reftable"，而是"**本 libgit2 版本是否新到含有 reftable**"——这本质是版本问题，而非特性问题。

### 1.3 方案：维持现状，无需改动

git2go 现有的 `IsReftableSupported()`（`reftable_on.go` / `reftable_off.go`）已是该场景的最优解：

| 层次 | 手段 | 判定内容 |
| --- | --- | --- |
| 编译期 | `libgit2_reftable` build tag | 本次构建是否编译了 reftable 绑定 |
| 运行时 | `IsReftableSupported()`（临时目录试 init） | 链接的 libgit2 是否真的能创建 reftable 仓库 |
| 仓库级 | `Repository.RefStorageFormat()`（读 `extensions.refStorage`） | 某仓库实际使用哪个后端 |

三层探测已完整覆盖所有实际需求，且**比 feature 标志更可靠**（feature 标志只说明编译能力，不保证运行时可用）。

**行动项**：无。保留 roadmap 条目作为观察项——若上游未来新增 `GIT_FEATURE_REFTABLE`，则在 `features.go` 增补一行常量，并让 `IsReftableSupported()` 优先走该标志（快路径），保留 init 探测作兜底。

---

## 2. C4：per-worktree 引用语义

### 2.1 上游现状

```c
/* src/libgit2/refs.h:108 */
int git_reference__is_per_worktree_ref(const char *ref_name);
```

- 位于 `src/libgit2/`（**内部头**），不在 `include/git2/` 下。
- 双下划线前缀 `git_reference__` 是 libgit2 的内部符号命名约定。
- `include/git2/` 全目录检索无任何 per-worktree 相关公开 API。

### 2.2 分析

该函数是 reftable PR #7117 为支持 per-worktree 引用（如 `HEAD`、`refs/bisect/*`、`refs/worktree/*`）而从静态函数提升为跨文件内部可见的，**目的是供 `refdb_reftable.c` 与 `refdb_fs.c` 共用**，并非面向外部调用者。

绑定内部符号的风险：

- 无 API 稳定性承诺，上游可随时改签名或改回 `static`。
- 静态链接下虽可解析（如本项目此前依赖 `git_odb_hash` 的经验），但动态链接时符号可能未导出——尤其上游近期刚合并 `629a7494c Don't export API symbols on static build` / `52a480e4a cmake: update build visibility`，符号可见性策略正在收紧。

### 2.3 方案：不绑定；提供纯 Go 等价实现（可选）

**不应**绑定 `git_reference__is_per_worktree_ref`。若确有需求，per-worktree 引用的判定规则是 git 的**既定约定**，可在 Go 侧独立实现，零 ABI 风险：

```go
// IsPerWorktreeRef reports whether a reference name is per-worktree rather
// than shared across all worktrees, following git's conventions.
// This mirrors libgit2's internal git_reference__is_per_worktree_ref without
// binding to that unstable internal symbol.
func IsPerWorktreeRef(refName string) bool {
    switch refName {
    case "HEAD", "ORIG_HEAD", "MERGE_HEAD", "CHERRY_PICK_HEAD",
         "REVERT_HEAD", "REBASE_HEAD", "BISECT_HEAD", "AUTO_MERGE":
        return true
    }
    return strings.HasPrefix(refName, "refs/bisect/") ||
           strings.HasPrefix(refName, "refs/worktree/") ||
           strings.HasPrefix(refName, "refs/rewritten/")
}
```

> 注：落地前需逐条比对上游 `git_reference__is_per_worktree_ref` 的实现与 git 文档，确保规则一致；并加单元测试固化。

**行动项**：仅在有实际调用方需求时实施；否则保持 roadmap 观察状态，等上游公开等价 API。

---

## 3. C5：SHA-256 + reftable 组合（**已解锁，但有重大阻塞**）

### 3.1 上游现状：SHA256 已转正

上游 main 在 vendor 之后新增的 22 个 commit 中，**PR #7261（`ethomson/sha256`）** 是决定性变更：

| commit | 内容 |
| --- | --- |
| `d29fe50de` | Merge PR #7261 `ethomson/sha256` |
| `605f34a01` | **`sha256: it's what's for breakfast`**（转正） |
| `0a25fe369` | `sha256: update documentation` |
| `c0d2d4a62` | **`ci: remove experimental build`**（不再需要实验性构建） |
| `7831747de` | `sha256: skip gitdaemon tests on windows` |
| `3db3d5fb6` | **`reftable: include testrepo_256 reftable resource`**（reftable + SHA256 测试资源） |
| `9b1ca879f` | `reftable: copy the id value, not the struct` |

核查证据：

```sh
$ git grep -c "GIT_EXPERIMENTAL_SHA256" origin/main -- include/git2/
(空)   # 宏已从所有公开头文件中移除
```

`git_repository_init_options.oid_type` 现在**无条件可用**（原先包在 `#ifdef GIT_EXPERIMENTAL_SHA256` 内）：

```c
/* origin/main include/git2/repository.h */
const char *origin_url;
git_oid_t oid_type;     /* ← 不再有 #ifdef */
git_refdb_t refdb_type;
```

`git_oid_t` 枚举无条件包含 SHA256：

```c
typedef enum {
    GIT_OID_SHA1 = 1,
    GIT_OID_SHA256 = 2
} git_oid_t;
```

### 3.2 reftable 后端已原生支持 SHA256

`src/libgit2/refdb_reftable.c`（origin/main）已移除 `#ifdef`，直接按仓库 oid_type 选择 reftable 哈希：

```c
switch (backend->repo->oid_type) {
case GIT_OID_SHA1:
    options.hash_id = REFTABLE_HASH_SHA1;
    break;
case GIT_OID_SHA256:
    options.hash_id = REFTABLE_HASH_SHA256;
    break;
default:
    error = GIT_EINVALID;
    goto out;
}
```

并且上游已提供 `tests/resources/reftable/testrepo_256.git` 作为 reftable+SHA256 的测试夹具。

**结论**：SHA-256 + reftable 组合在上游 main **已完整实现且可测试**。git2go 侧只要能表达 `oid_type`，即可组合 `RefdbReftable` + `OidTypeSha256`。

### 3.3 阻塞问题：`git_oid` ABI 破坏性变更

这是本次调研发现的**最关键风险**。

**上游 main 的 `git_oid`：**

```c
typedef struct git_oid {
    unsigned char type;                   /* 1 字节（无条件） */
    unsigned char id[GIT_OID_MAX_SIZE];   /* 32 字节（GIT_OID_MAX_SIZE = SHA256_SIZE） */
} git_oid;                                /* 合计 33 字节（含对齐） */
```

**当前 vendor（`ddf3b5c85`）的 `git_oid`：**

```c
typedef struct git_oid {
#ifdef GIT_EXPERIMENTAL_SHA256
    unsigned char type;                   /* 未定义宏 → 不存在 */
#endif
    unsigned char id[GIT_OID_MAX_SIZE];   /* 未定义宏 → GIT_OID_MAX_SIZE = SHA1_SIZE = 20 */
} git_oid;                                /* 合计 20 字节 */
```

**git2go 的表示（`git.go:211-232`）：**

```go
type Oid [20]byte                          // 恰好匹配旧的 20 字节布局

func (oid *Oid) toC() *C.git_oid {
    return (*C.git_oid)(unsafe.Pointer(oid))   // ← 直接强转！
}

func newOidFromC(coid *C.git_oid) *Oid {
    oid := new(Oid)
    copy(oid[0:20], C.GoBytes(unsafe.Pointer(coid), 20))  // ← 硬编码 20 + 偏移 0
    return oid
}
```

**后果**：一旦 vendor 升级到含 SHA256 转正的 main：

1. `toC()` 把 20 字节的 Go 数组当作 33 字节的 C 结构传给 libgit2 → libgit2 读写越界，**破坏 Go 堆内存**（不可预测的崩溃/静默数据损坏）。
2. `newOidFromC()` 从偏移 0 拷 20 字节 → 实际拷到的是 `type` 字段 + 前 19 字节 id，**所有 OID 全部错位**。
3. 影响面极广：`Oid` 是 git2go 最基础的类型，被 commit/tree/blob/reference/index/odb/reflog 等**几乎所有 API** 使用。

> 这也解释了为何当前 vendor（`ddf3b5c85`）一切正常——它尚未转正 SHA256，`git_oid` 恰好是 20 字节。**这是一个"沉默的定时炸弹"：任何人无意中把 vendor 升到最新 main 都会触发。**

#### 3.3.1 尺寸宏的确切定义（复核补充）

`include/git2/oid.h`（`939362a3c`）的相关宏：

| 宏 | 当前 vendor `ddf3b5c85` | 上游 main `939362a3c` |
| --- | --- | --- |
| `GIT_OID_SHA1_SIZE` | 20 | 20 |
| `GIT_OID_SHA256_SIZE` | 32（仅在 `GIT_EXPERIMENTAL_SHA256` 下定义） | **32（无条件）** |
| `GIT_OID_MAX_SIZE` | `= GIT_OID_SHA1_SIZE` → **20** | `= GIT_OID_SHA256_SIZE` → **32** |
| `GIT_OID_MAX_HEXSIZE` | 40 | 64 |
| `GIT_OID_RAWSZ`（deprecated） | 20 | `= GIT_OID_SHA1_SIZE` → 20 |

**易误判之处**：上游 main 中SHA256 相关宏仍写作 `# define`（`#` 后有缩进空格），且上方注释仍保留 "Experimental SHA256 support is a breaking change to the API. This exists for application compatibility testing."。这只是 `#ifdef GIT_EXPERIMENTAL_SHA256` 被删除后**残留的格式与陈旧注释**，实际已是**无条件定义**：

```c
/*
 * Experimental SHA256 support is a breaking change to the API.   ← 陈旧注释
 * This exists for application compatibility testing.
 */

/** Size (in bytes) of a raw/binary sha256 oid */
# define GIT_OID_SHA256_SIZE     32          ← 无 #ifdef 包裹，恒定义
...
#define GIT_OID_MAX_SIZE        GIT_OID_SHA256_SIZE   ← 恒为 32
```

因此 §3.4 阶段 1 的 `#if GIT_OID_MAX_SIZE != 20` 守卫是**可靠**的判据：当前 vendor 下为 20（通过），升级到含 SHA256 转正的 main 后为 32（报错）。

### 3.4 落地方案（分阶段）

#### 阶段 1：立即防护（必做，低成本）✅ 已落地

在 vendor 升级前加入**编译期尺寸断言**，让不兼容的 libgit2 在构建时立即失败，而非运行时静默损坏内存。

**落地情况**：已在 `git2go_version_check.h` 中实现 `#if GIT_OID_MAX_SIZE != 20` 守卫（含完整背景注释），三种链接方式（bundled / system-static / system-dynamic）均自动受保护，因为它们都 include 该头。

**双向验证**（用隔离的模拟头文件，避免真实 `oid.h` 干扰）：

| 模拟场景 | `GIT_OID_MAX_SIZE` | 结果 |
| --- | --- | --- |
| SHA256 转正后的 libgit2 | 32 | ✅ 编译期报错，错误信息指向本文档 §3 |
| 当前 vendor 的 libgit2 | 20 | ✅ 静默通过 |

实现如下：

```c
#include <git2/oid.h>

/*
 * git2go represents git_oid as a bare Go [20]byte and casts it directly to
 * git_oid*. That is only valid while libgit2's git_oid is exactly 20 bytes
 * (SHA1-only, no `type` field). Upstream main has promoted SHA256 out of
 * GIT_EXPERIMENTAL_SHA256, making git_oid 33 bytes with a leading `type`
 * byte, which would corrupt memory through the cast.
 *
 * Fail loudly at compile time instead. Removing this guard requires reworking
 * the Oid type first (see docs/reftable-longterm-research.md section 3.4).
 */
#if GIT_OID_MAX_SIZE != 20
# error "Incompatible libgit2: git_oid is larger than git2go's Oid ([20]byte). \
This libgit2 has SHA256 object IDs enabled; git2go's Oid type must be reworked \
before it can be used. See docs/reftable-longterm-research.md."
#endif
```

> 该守卫同时保护三种链接方式（bundled/system-static/system-dynamic），因为它们都 include 这个头。

**收益**：把一个"内存损坏级"的隐患降级为"编译期明确报错"，成本仅数行。

#### 阶段 2：`Oid` 类型重构（大工程，SHA256 支持的前提）

目标：让 `Oid` 与新的 `git_oid`（type + 32 字节）等价，且尽量少影响现有调用方。

> ⚠️ **本节的方案对比已被专项审计修正**。详细的调用点清单、实测布局数据与最终结论见 **[oid-refactor-audit.md](./oid-refactor-audit.md)**。要点：
>
> - 审计发现 **20 处把 `oid.toC()` 当OUT 参数**（C 写入 Go 内存）的调用点，覆盖提交/标签/树/note/stash/odb 写入等几乎所有写对象路径。
> - 因此**方案 B 会导致这20 处静默失效**（编译通过但拿不到 OID），其"源码兼容性更好"的原判断**不成立** —— 它只是把错误从编译期推迟到运行期。
> - 实测上游 `git_oid` 布局为 `sizeof=33, align=1, offset_id=1`；Go 侧 `struct{ Type uint8; ID [32]byte }` 可**精确匹配**（33/1/1），故**方案 A 能保持零拷贝强转**，使 44 处调用点（含 20 处 OUT）**零改动**。
> - **最终推荐改为方案 A。**

**方案 A（审计后确认推荐）：结构体化，布局精确匹配 C**

```go
// Oid represents the id for a Git object.
type Oid struct {
    Type uint8       // 必须是 uint8 才能匹配 C 的 unsigned char（offset 0）
    ID   [32]byte    // GIT_OID_MAX_SIZE（offset 1）
}
// sizeof = 33, align = 1 —— 与 C 的 git_oid 完全一致
```

- 优点：与 C 布局一一对应，**保持零拷贝强转**，`toC()` 实现与全部 44 处调用点无需改动；不兼容处均在**编译期暴露**，无静默错误风险。
- 缺点：`oid[:]`、`oid[i]`、`len(oid)`、`[20]byte` 字面量等**数组用法失效**，属破坏性 API 变更，需 **v36 major bump**。
- 注意：`Type` 字段**不能**用公开的 `OidType`（底层 `int`，8 字节）—— 实测会使结构变为 40 字节且 `ID` 偏移错位。应保留 `uint8` 并提供 `OidType()` 访问器。
- 实测确认：结构体**可比较**，故 `*oid == *oid2`（`Equal`）、`Oid{}` 字面量（`IsZero`）、`Oid` 作 map key **语法均兼容**（原文此处判断有误，已修正）。

**方案 B（审计后不再推荐）：保持数组语义，扩容 + 显式转换函数**

```go
type Oid [32]byte    // 仅扩容，不含 type

func (oid *Oid) toC(t OidType) *C.git_oid {
    var coid C.git_oid
    coid._type = C.uchar(t)          // 显式设置类型
    copy((*[32]byte)(unsafe.Pointer(&coid.id))[:], oid[:])
    return &coid                      // 注意：需保证生命周期
}
```

- 优点：`oid[:]` / `==` / `Oid{}` 等惯用法大部分仍可用，源码兼容性冲击较小。
- 缺点：丢失 type 信息（需在调用侧传递，或由 Repository 的 oid_type 推断）；不再能零拷贝强转，所有 `toC` 调用点需审查生命周期（不能返回栈上 `&coid` 给 libgit2 长期持有）。

**方案 C：双类型并存**

保留 `Oid [20]byte` 用于 SHA1 路径，新增 `OidAny`/`Oid256` 用于 SHA256——复杂度高、API 割裂，**不推荐**。

**推荐路径**（审计后修正）：**直接采用方案 A**，作为 v36 的形态。原先"方案 B 过渡 + 方案 A 目标"的两步走已被否决 —— 方案 B 会让 20 处OUT 参数静默失效，且 `oid[:]`/`len(oid)` 语义从 20 静默漂移到 32，代价高于直接做方案 A。详见 [oid-refactor-audit.md §5](./oid-refactor-audit.md)。

无论哪种方案，均需完成：

1. ✅ **审计所有 `unsafe.Pointer` 强转点** —— **已完成**，见 [oid-refactor-audit.md](./oid-refactor-audit.md)：全项目仅 `git.go:231` 一处强转，但有 44 处 `oid.toC()` 调用（20 处为 OUT 参数）与 `git.go` 内 9 处布局依赖。
2. **绑定 oid type API**：
   - ✅ `OidType` 枚举（`OidTypeSha1` / `OidTypeSha256`）—— **已落地**（`oid_type.go`）
   - ✅ `Repository.OidType()` ← `git_repository_oid_type` —— **已落地**（该API 在当前 vendor 已无条件公开）
   - ✅ `IsSha256Supported()` ← `Features() & FeatureSHA256` —— **已落地**（SHA256 有 feature 标志，无需探测）
   - ⬜ `RepositoryInitOptions.OidType` ← init options 的 `oid_type`（需 vendor 升级；且在 `Oid` 重构前无法安全使用）
   - ⬜ `git_oid_from_string` / `git_oid_from_prefix` / `git_oid_from_raw`（带 type 参数的新式 API，替代 `git_oid_fromstr` 等旧式）
3. **`NewOid` / `String` / `Cmp` / `NCmp` / `IsZero` 等按 type 感知长度**（SHA1 比较 20 字节，SHA256 比较 32 字节）。
4. **`ShortenOids` 等依赖 hexsize 的逻辑**改用 `GIT_OID_MAX_HEXSIZE`。

> **阶段 2 已落地部分的说明**：`OidType` 常量刻意使用 Go 字面量（`1` / `2`）而非 `C.GIT_OID_SHA1` / `C.GIT_OID_SHA256`，因为在当前 vendor 上 `GIT_OID_SHA256` 仍包在 `#ifdef GIT_EXPERIMENTAL_SHA256` 内、并未定义，引用它会导致编译失败。这与`RefdbType` 的处理方式一致。
>
> `OidType` 另提供 `String()`（`"sha1"` / `"sha256"`，对应 `extensions.objectFormat` 配置值）、`Size()`（20 / 32）与 `HexSize()`（40 / 64）辅助方法，供后续 `Oid` 重构与探测逻辑复用。
>
> 当前这些 API 的定位是**报告与探测**仓库格式，而非创建 SHA256 仓库——后者被阶段 1 的编译期守卫挡住，需先完成 `Oid` 重构。`oid_type_test.go` 中的 `TestOidTypeMatchesOidWidth` 固化了 `len(Oid) == OidTypeSha1.Size()` 这一不变量，与编译期守卫互为呼应。

#### 阶段 3：SHA-256 + reftable 组合验证

前置：阶段 2 完成 + vendor 升级到 `939362a3c` 或更新。

测试矩阵（4 组合）：

| oid_type | refdb_type | 验证点 |
| --- | --- | --- |
| SHA1 | files | 现有基线（回归保护） |
| SHA1 | reftable | 已有 `TestReftableBranchLifecycle` 覆盖 |
| SHA256 | files | SHA256 基础路径 |
| **SHA256** | **reftable** | **本项目标**：`REFTABLE_HASH_SHA256` 路径 |

用例设计（新增 `oid_sha256_test.go` / 扩展 `refdb_reftable_test.go`）：

```go
func TestReftableWithSha256(t *testing.T) {
    if !IsSha256Supported() { t.Skip("libgit2 built without SHA256") }

    repo, err := InitRepositoryExt(path, &RepositoryInitOptions{
        Flags:     RepositoryInitMkpath | RepositoryInitBare,
        OidType:   OidTypeSha256,     // 阶段 2 新增
        RefdbType: RefdbReftable,
    })
    // 断言：
    //  - repo.OidType() == OidTypeSha256
    //  - extensions.objectFormat == "sha256"
    //  - extensions.refStorage  == "reftable"
    //  - 提交后 OID 字符串长度为 64
    //  - 分支 CRUD 正常（走 REFTABLE_HASH_SHA256）
}
```

`IsSha256Supported()` 直接用 `Features() & FeatureSHA256 != 0`（git2go 已绑定 `FeatureSHA256`），**无需 init 探测** — 这与 reftable 不同，因为 SHA256 确实有 feature 标志。该函数**已随阶段 2 落地**。

### 3.5 风险与工作量评估

| 阶段 | 工作量 | 风险 | 状态 |
| --- | --- | --- | --- |
| 1 编译期守卫 | 极小（~40 行含注释） | 无 | ✅ **已完成** |
| 2a oid type 探测 API | 小（`oid_type.go` + 6 个测试） | 无 | ✅ **已完成** |
| 2b Oid 重构 | **大**（触及全部 API，需审计所有 unsafe 强转） | **高**（内存安全 + 源码兼容性） | ⬜ 需独立 PR + 充分测试 |
| 3 SHA256+reftable 验证 | 中（测试为主） | 低 | ⬜ 依赖 2b |

---

## 4. 建议的行动顺序

1. ✅ **【已完成】** 阶段 1 的编译期尺寸守卫 —— 防止 vendor 误升级造成内存损坏。已双向验证（模拟 32 字节报错、20 字节通过）。
2. ✅ **【已完成】** 绑定 `OidType` 枚举 + `Repository.OidType()` + `IsSha256Supported()` —— 零风险增量，为后续铺路。6 个新测试全部通过，全量回归基线由 135 增至 141 个用例。
3. ✅ **【已完成】** `Oid` 重构影响面审计 —— 见 [oid-refactor-audit.md](./oid-refactor-audit.md)。结论：方案 A（结构体 + `uint8` type字段）可保持零拷贝强转，44 处调用点零改动；方案 B 因 20 处 OUT 参数静默失效而否决。
4. **【规划】** 阶段 2b `Oid` 重构（方案 A）—— 独立里程碑，需接受 v36 major bump。这是解锁 SHA256 的唯一前置。
5. **【规划】** 阶段 3 SHA256 + reftable 组合测试矩阵。
6. **【冻结】** C3 / C4 保持观察，无上游动作则不改动。

---

## 5. 附：核查命令备忘

```sh
cd vendor/libgit2
git fetch origin +refs/heads/main:refs/remotes/origin/main

# C3: feature标志（应无 GIT_FEATURE_REFTABLE）
git show origin/main:include/git2/common.h | grep "GIT_FEATURE_"

# C4: per-worktree 是否公开（空 = 仍为内部 API）
git grep -l "per_worktree" origin/main -- include/git2/
git grep -n "is_per_worktree_ref" origin/main -- src/libgit2/refs.h

# C5: SHA256 是否转正（空 = 已转正）
git grep -c "GIT_EXPERIMENTAL_SHA256" origin/main -- include/git2/
git show origin/main:include/git2/oid.h | grep -A8 "typedef struct git_oid"

# C5: 尺寸宏（确认 GIT_OID_MAX_SIZE 是否已变为 32）
git show origin/main:include/git2/oid.h | grep -nE "define GIT_OID_(SHA1|SHA256|MAX)_(SIZE|HEXSIZE)"

# reftable 的 SHA256 支持
git show origin/main:src/libgit2/refdb_reftable.c | grep -B6 -A4 "REFTABLE_HASH_SHA256"

# 本地实测当前 vendor 的 git_oid 尺寸（守卫判据）
printf '#include <git2.h>\n#include <stdio.h>\nint main(void){printf("MAX_SIZE=%%d sizeof=%%d\\n",(int)GIT_OID_MAX_SIZE,(int)sizeof(git_oid));return 0;}\n' > /tmp/oidsize.c
cc -I ../../static-build/install/include /tmp/oidsize.c -o /tmp/oidsize && /tmp/oidsize
```
