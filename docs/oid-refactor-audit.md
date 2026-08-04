# `Oid` 类型重构影响面审计

> 审计日期：2026-08-04
> 目标：为 `docs/reftable-longterm-research.md` §3.4 阶段 2b（`Oid` 类型重构）提供精确的影响面数据与方案决策依据。
> 背景：上游 libgit2 main 将 SHA256 转正后，`git_oid` 由 20 字节变为 33 字节，git2go 的 `type Oid [20]byte` + `unsafe.Pointer` 强转不再成立。

---

## 0. 执行摘要

| 指标 | 数量 | 说明 |
| --- | --- | --- |
| `Oid` → `*C.git_oid` 强转点 | **1** | 仅 `git.go:231` 的 `Oid.toC()`，集中度极好 |
| `oid.toC()` 调用点 | **44** | 分布在 19 个文件 |
| ├─ **OUT 参数（C 写入 Go 内存）** | **20** | ⚠️ **最高风险**，见 §2 |
| ├─ IN 参数（只读） | 22 | 低风险 |
| └─ 值拷贝（`*oid.toC()`） | 2 | 低风险 |
| `newOidFromC()` 调用点 | 见 §3 | 反向转换，需同步改 |
| `Oid` 内部布局依赖 | **6** | 全部在 `git.go`，见 §3 |
| 引用 `C.git_oid` 的文件 | 12 | ~36 处 |

**核心结论**：

1. **强转只有一处**，但被 44 处调用间接依赖，其中 **20 处把 `toC()` 结果当 OUT 参数**。
2. 上游 `git_oid` 实测布局为 `sizeof=33, align=1, offset_type=0, offset_id=1`，Go 侧 `struct{ Type uint8; ID [32]byte }` 可**精确匹配**，因此**方案 A 可保持零拷贝强转**，从而完整保留 OUT 语义。
3. **方案 B（`Oid [32]byte`）的"源码兼容"是假兼容**：它会让 20 处 OUT 参数**静默失效**（编译通过、功能全错），且 `oid[:]`/`len(oid)` 的语义从 20 静默变为 32。风险高于方案 A。
4. **修正原报告的方案推荐**：应选**方案 A**（结构体 + `uint8` type 字段），而非原先建议的方案 B。

---

## 1. 强转点（唯一）

```go
// git.go:230-232
func (oid *Oid) toC() *C.git_oid {
    return (*C.git_oid)(unsafe.Pointer(oid))
}
```

这是整个 git2go 中**唯一**把Go `Oid` 当 C `git_oid` 用的位置（已用 regex 全量扫描确认）。所有 44 处调用都经由它。

**含义**：只要`Oid` 的内存布局与 `C.git_oid` 保持一致，这一处不需要改，44 处调用点也全部不需要改。这是重构能否低成本落地的关键杠杆。

---

## 2. OUT 参数调用点清单（20 处）⚠️ 最高风险

这些调用把 `oid.toC()` 作为**输出参数**传给 libgit2，由 C侧写入 Go 分配的内存。若 `toC()` 改为返回临时副本（方案 B），C 会写进副本，Go 侧的 `Oid` **永远不会被更新** —— 编译通过，但创建 commit/tag/tree 后拿不到正确 OID，属**静默功能性错误**。

| # | 位置 | 调用 |
| --- | --- | --- |
| 1 | `tree.go:235` | `git_treebuilder_write(oid.toC(), ...)` |
| 2 | `index.go:393` | `git_index_write_tree_to(oid.toC(), ...)` |
| 3 | `index.go:425` | `git_index_write_tree(oid.toC(), ...)` |
| 4 | `odb.go:153` | `git_odb_write(oid.toC(), ...)` |
| 5 | `odb.go:266` | `git_odb_hash(oid.toC(), ...)` |
| 6 | `odb.go:458` | `git_odb_stream_finalize_write(stream.Id.toC(), ...)` |
| 7 | `commit.go:224` | `git_commit_amend(oid.toC(), ...)` |
| 8 | `commit.go:290` | `git_commit_create_from_stage(oid.toC(), ...)` |
| 9 | `stash.go:69` | `git_stash_save(oid.toC(), ...)` |
| 10 | `stash.go:122` | `git_stash_save_with_opts(oid.toC(), ...)` |
| 11 | `note.go:54` | `git_note_create(oid.toC(), ...)`（同一调用的 `id.toC()` 是 IN） |
| 12 | `note.go:239` | `git_note_next(noteId.toC(), ...)` |
| 13 | `note.go:239` | `git_note_next(..., annotatedId.toC(), ...)`（同一调用第二个 OUT） |
| 14 | `tag.go:92` | `git_tag_create(oid.toC(), ...)` |
| 15 | `tag.go:141` | `git_tag_create_lightweight(oid.toC(), ...)` |
| 16 | `walk.go:170` | `git_revwalk_next(id.toC(), ...)` |
| 17 | `repository.go:656` | `git_commit_create(oid.toC(), ...)` |
| 18 | `repository.go:692` | `git_commit_create_with_signature(oid.toC(), ...)` |
| 19 | `repository.go:819` | `git_commit_create_from_ids(oid.toC(), ...)` |
| 20 | `rebase.go:437` | `git_rebase_commit(ID.toC(), ...)` |

> 这 20 处覆盖了 git2go 几乎所有**写对象**的路径（提交、标签、树、note、stash、odb 写入、rebase、revwalk 遍历）。任何静默失效都会造成极难排查的数据错误。

### 2.1 值拷贝用法（2 处，方案 B 下仍安全）

| 位置 | 代码 | 说明 |
| --- | --- | --- |
| `rebase.go:146,178` | `*_out = *oid.toC()` | 解引用后值拷贝到 C 侧 out 指针，不依赖 `toC()` 返回指针的持久性 |
| `index.go:101` | `dest.id = *source.Id.toC()` | 同上，拷进C 结构字段 |

---

## 3. `Oid` 内部布局依赖（6 处，全在 `git.go`）

重构时必须逐一修改：

| 位置 | 代码 | 方案 A 下的改法 |
| --- | --- | --- |
| `git.go:212` | `type Oid [20]byte` | → `struct{ Type uint8; ID [32]byte }` |
| `git.go:220` | `copy(oid[0:20], C.GoBytes(unsafe.Pointer(coid), 20))` | 按 `Type` 决定长度，从 `offset 1` 拷贝 |
| `git.go:226` | `copy(oid[0:20], b[0:20])` | `NewOidFromBytes`：按长度推断类型 |
| `git.go:250` | `copy(o[:], slice[:20])` | `NewOid`：支持 40 或 64 位 hex |
| `git.go:255` | `hex.EncodeToString(oid[:])` | → `oid.ID[:oid.Type.Size()]` |
| `git.go:259,276` | `bytes.Compare(oid[:], oid2[:])` / `oid[:n]` | 按类型长度比较 |

另需处理：

| 位置 | 代码 | 说明 |
| --- | --- | --- |
| `git.go:235` | `if len(s) > C.GIT_OID_HEXSZ` | `GIT_OID_HEXSZ` 定义为 `GIT_OID_SHA1_HEXSIZE`（=40），位于 `deprecated.h`。SHA256 的 hex 长 64 字符，会被此校验**误判为 "string is too long for oid"**。应改用 `GIT_OID_MAX_HEXSIZE`（=64）。附带风险：该宏在 `deprecated.h` 中，依赖构建时 `DEPRECATE_HARD=OFF`（本项目 `script/build-libgit2.sh` 正是如此设置），重构时宜一并去除对 deprecated 宏的依赖 |
| `git.go:272` | `*oid == Oid{}` (`IsZero`) | 结构体仍可比较，**语法兼容**；但零值 `Type` 会是 0（非法类型），语义需重新定义 |
| `git.go:292-294` | `buf := make([]byte, 41)` / `buf[40] = 0` | `ShortenOids` 的 hex 缓冲区需按 `GIT_OID_MAX_HEXSIZE+1`(65) 分配 |

---

## 4. 上游 `git_oid` 精确布局（实测）

用等价C 结构实测（`unsigned char type; unsigned char id[32];`）：

```text
sizeof=33  align=1  offset_type=0  offset_id=1
```

对应的 Go 等价声明：

```go
type Oid struct {
    Type uint8      // offset 0, 1 byte  ← 必须是 uint8，不能用 OidType(int)
    ID   [32]byte   // offset 1, 32 bytes
}
// sizeof = 33, align = 1 —— 与 C 完全一致，可零拷贝强转
```

**关键约束**：`Type` 字段**必须**声明为 `uint8`。若用 `OidType`（底层 `int`，64 位平台 8 字节）会使结构变为 40 字节且 `ID` 偏移错位，强转立即失效。

Go 侧实测对照（`unsafe.Sizeof` / `Alignof` / `Offsetof`）：

| Go 声明 | sizeof | align | offset(ID) | 与 C（33/1/1）匹配 |
| --- | --- | --- | --- | --- |
| `struct{ Type uint8; ID [32]byte }` | **33** | **1** | **1** | ✅ 完全一致 |
| `struct{ Type OidType; ID [32]byte }`（`OidType` 为 `int`） | 40 | 8 | 8 | ❌ 布局破坏 |

同时实测确认结构体**可比较**且零值字面量可用（`g == OidGood{}` 编译并返回 `true`），这保证了 `IsZero()`、`Equal()` 与 `Oid` 作 map key 的语法兼容性。

> 这也意味着不能直接把公开的 `OidType`（`oid_type.go` 中定义为 `int`）用作字段类型；需提供 `func (o *Oid) OidType() OidType { return OidType(o.Type) }` 之类的访问器，或将字段设为非导出 + 提供访问器。

---

## 5. 方案重评估（修正原报告结论）

### 5.1 方案 A：结构体化（**推荐**）

```go
type Oid struct {
    Type uint8
    ID   [32]byte
}
```

| 维度 | 评价 |
| --- | --- |
| OUT 参数（20 处） | ✅ **完整保留** —— 布局匹配，`toC()` 强转不变，20 处调用点零改动 |
| `toC()` 实现 | ✅ 不需要改（仍是强转） |
| 44 处调用点 | ✅ 零改动 |
| 内部布局依赖（6+3 处） | ⚠️ 需改，但全部集中在 `git.go` |
| `Oid{}` 字面量 | ✅ 语法兼容（结构体零值） |
| `*oid == *oid2` 比较 | ✅ 语法兼容（结构体可比较） |
| `oid[:]` / `oid[i]` / `len(oid)` | ❌ **破坏** —— 外部调用方需改为 `oid.ID[:]` 等 |
| `Oid` 作 map key | ✅ 兼容（结构体可比较即可作 key） |
| 语义安全性 | ✅ 失效处**编译期报错**，不会静默出错 |

### 5.2 方案 B：数组扩容（**不再推荐**）

```go
type Oid [32]byte
```

| 维度 | 评价 |
| --- | --- |
| OUT 参数（20 处） | ❌ **静默失效** —— 若 `toC()` 返回临时副本，C 写入副本，Go 侧不更新 |
| `toC()` 实现 | ❌ 必须重写为构造副本 + 设置 type；且需为 OUT 场景另设机制 |
| 44 处调用点 | ❌ 需逐一判定 IN/OUT 并分别改造 |
| `oid[:]` / `len(oid)` | ⚠️ **假兼容** —— 语法通过但长度从 20 静默变 32，比编译失败更危险 |
| type 信息 | ❌ 丢失，需从Repository 推断或额外传参 |
| 语义安全性 | ❌ 多处静默错误风险 |

> **原报告（§3.4 方案 B "源码兼容性冲击较小"）的判断需修正**：本次审计发现的20 处 OUT 参数与`oid[:]` 语义漂移，使方案 B 的实际风险显著高于方案 A。方案 B 的"兼容"只是让错误从编译期推迟到运行期。

### 5.3 结论

**选择方案 A**，理由：

1. 保住最大的杠杆点—— 布局匹配 ⇒ 强转不变 ⇒ 44 处调用点（含 20 处 OUT）零改动。
2. 所有不兼容处都会**编译期暴露**，可穷尽修复，无静默错误风险。
3. 改动范围高度集中：`git.go` 的 9 处+ 外部调用方的 `oid[:]` 类用法。

**代价**：`oid[:]`、`oid[i]`、`len(oid)`、`[20]byte` 字面量赋值等数组用法失效，属**破坏性API 变更**，需 **v36 major bump**。

---

## 6. 无关项（已排除）

| 项 | 说明 |
| --- | --- |
| `HostkeyCertificate.HashSHA1 [20]byte`（`remote.go:313`） | SSH host key 指纹，与 `git_oid` 无关 |
| `blob.go:139` `C.git_oid{}` | 直接使用 C 类型，随头文件自动适配 |
| `errorclass_string.go` / `errorcode_string.go` / `difflinetype_string.go` 中的 20/40 | `stringer` 生成的索引表，与 oid 无关 |
| `indexer_test.go:49` 的 `len(pack)-20` | packfile 尾部 SHA1 校验和，属 pack 格式常量（SHA256 pack 为 32，届时需另行处理，但不属`Oid` 重构范围） |

---

## 7. 建议的实施步骤

1. **v36 决策**：确认接受 major bump（`go.mod` module path 改 `/v36`）。
2. **改 `Oid` 定义**（`git.go:212`）为方案 A 结构体，`Type` 用 `uint8`。
3. **加布局静态断言**：Go 侧 `unsafe.Sizeof(Oid{}) == 33`（可用 `var _ [1]struct{}` 技巧或 `//go:build` 测试），C 侧沿用已有的 `GIT_OID_MAX_SIZE` 守卫并把阈值从 20 改为 32。
4. **修 `git.go` 的 9 处布局依赖**（§3 表格）。
5. **绑定新式 oid API**：`git_oid_from_string` / `git_oid_from_prefix` / `git_oid_from_raw`（带 type 参数），替换 `NewOid` / `NewOidFromBytes` 的实现。
6. **`RepositoryInitOptions.OidType`** 接入（此时才安全）。
7. **全量回归**，并新增 SHA1/SHA256 × files/reftable 四组合测试（原报告 §3.4 阶段 3）。
8. **升级 vendor** 到含 SHA256 转正的 main（必须在第 3 步守卫更新后）。

> 步骤 2–5 应在**同一个 PR** 内完成 —— 中间状态无法编译通过。
