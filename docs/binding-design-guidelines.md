# git2go 绑定层设计准则

> 基线：git2go `feat-sha256-reftable`；libgit2 main `0551dfd4`；git master（`refs/refs-internal.h`）
> 撰写日期：2026-09-11
>
> 本文从 **git 原生实现** 与 **libgit2 自身实现逻辑** 出发，推导 Go 绑定层的正确做法，
> 并逐条对照本仓库现状，标注「已符合」「偏离」「技术债」。所有论断均给出 `file:line`
> 证据或实测输出。

---

## 0. 一条贯穿全文的原则

绑定层不是「把 C 函数逐个包一层」，而是**在两套内存模型、两套错误模型、两套并发模型之间
建立可验证的映射**。判断映射是否正确的标准只有一条：

> **C 侧的任何契约违反，都必须在 Go 侧变成编译错误或明确的 `error`，绝不能变成
> 未定义行为。**

据此可给绑定层的每个决策排优先级：

```
编译期失败  >  panic（快速失败）  >  返回 error  >  静默降级
```

最后一档是绝对禁止的。后文所有结论都是这条原则的推论。

---

## 1. 内存管理与对象生命周期映射

### 1.1 libgit2 的所有权词汇表

libgit2 用**头文件注释**而非类型系统表达所有权，绑定层必须把它翻译成 Go 的类型与生命周期。
上游实际存在四类契约：

| 契约 | C 侧标志 | 代表 | Go 侧应有形态 |
|---|---|---|---|
| **调用方拥有** | 有对应 `git_*_free` | `git_reference`、`git_tree`、`git_refdb` | 拥有型结构 + `Free()` + finalizer |
| **父对象拥有（借用）** | 返回 `const T *`，无 free 函数 | `git_tree_entry_byindex`、`git_index_get_byindex` | **值拷贝**，或持指针但保活父对象 |
| **库拥有（全局）** | 明确写「should not be freed」 | `git_error_last` | 只读，立即 `C.GoString` 拷出 |
| **移交给库** | 明确写「will take ownership」 | `git_refdb_set_backend` | 成功后置空 Go 指针并解 finalizer |

上游对第二类的表述是精确的：

```c
// vendor/libgit2/include/git2/tree.h:102-112
 * This returns a git_tree_entry that is owned by the git_tree.  You don't
 * have to free it, but you must not use it after the git_tree is released.
```

```c
// include/git2/index.h:490-501
 * The entry is not modifiable and should not be freed.  Because the
 * `git_index_entry` struct is a publicly defined struct, you should
 * be able to make your own permanent copy of the data if necessary.
```

第四类同样显式：

```c
// include/git2/sys/refdb_backend.h:401-411
 * The `git_refdb` will take ownership of the `git_refdb_backend` so you
 * should NOT free it after calling this function.
```

### 1.2 准则：借用型一律深拷贝

上游对 `git_index_entry` 主动建议 "make your own permanent copy"。**这个建议应推广到所有
借用型**：只要 Go 侧持有裸 C 指针，就存在悬垂窗口，而 Go 的 GC 与 C 的手动释放之间没有
任何同步机制。

本仓库多数类型已遵循：`TreeEntry`（`tree.go:36-50`，立即复制 name/id/type/mode，
不保留 C 指针）、`IndexEntry`（`index.go:66-91`）、`DiffDelta`（`diff.go:151-159`）、
`Signature`（`signature.go:19-31`）。

**唯一偏离：`ReflogEntry`** 保留裸指针（`reflog.go:41-55`）：

```go
type ReflogEntry struct {
	ptr    *C.git_reflog_entry
	reflog *Reflog
}
```

访问器只 `runtime.KeepAlive(e)`（保活 entry 自身），**从未 `KeepAlive(e.reflog)`**。
`reflog` 字段构成强引用因而阻止 GC，但**不阻止调用方显式 `reflog.Free()`**——此后
`e.ptr` 悬垂。

> 值得注意：上游对 `git_reflog_entry_byindex` **没有**像 `tree.h` 那样写出所有权句子
> （`include/git2/reflog.h:94-105` 只标 `const`），所有权靠「无对应 free 函数」推断。
> **当 C 侧契约本身模糊时，绑定层应选择更保守的一侧**——即深拷贝。

### 1.3 准则：`Free()` 必须幂等

C 的 `free(p)` 两次是未定义行为。Go 侧无法阻止调用方写出 `defer x.Free()` 加一个提前的
`x.Free()`，因此**幂等是绑定层的义务，不是调用方的义务**。

正确形态（本仓库样板，`repository.go:896-907`）：

```go
func (v *Refdb) Free() {
	if v == nil || v.ptr == nil {
		return
	}
	ptr := v.ptr
	v.ptr = nil          // 先置空，再释放
	owner := v.r
	v.r = nil
	runtime.SetFinalizer(v, nil)
	C.git_refdb_free(ptr)
	runtime.KeepAlive(owner)   // 保证父对象存活到 C 调用返回后
}
```

四个要点缺一不可：nil 双检查；**先置空后释放**（否则 finalizer 与显式调用竞争）；
解除终结器；`KeepAlive(parent)` 覆盖到调用之后。

**本仓库现状：全部幂等**（2026-09-11 修复 28 处），并由 `script/check-binding-invariants.go`
作为构建门禁持续保证。修复前有一处已实测为真实缺陷：

```go
// remote.go:658-664
func (r *Remote) Free() {
	r.repo.Remotes.untrackRemote(r)   // ← 第二次调用时 r.repo 已是 nil
	if r.weak { return }
	r.free()                           // free() 内部设 r.repo = nil
}
```

一次性探针实测确认（现已固化为 `TestRemoteFreeIsIdempotent`）：

```text
CONFIRMED: second Remote.Free() panicked:
  runtime error: invalid memory address or nil pointer dereference
```

该修复还覆盖了第二个场景：交给 `SmartSubtransportCallback` 的 Remote 没有归属仓库
（`repo == nil`），首次调用即会 panic——由 `TestRemoteFreeWithoutOwningRepository` 固化。

修复前最弱的一处是 `OdbBackend.Free`：既无 nil 检查、不清指针，连 `SetFinalizer(v, nil)`
都没有——显式释放后 finalizer 仍会二次释放。

### 1.4 准则：`KeepAlive` 不是礼节

Go 的 finalizer 可在**最后一次使用之后、C 调用返回之前**运行。凡把 `x.ptr` 传进 C 的地方，
都必须有 `runtime.KeepAlive(x)` 覆盖到调用之后。

本仓库绝大多数遵循，两处遗漏：

| 位置 | 问题 |
|---|---|
| `remote.go:1236-1238` `PruneRefs()` | `return C.git_remote_prune_refs(o.ptr) > 0`——无 `KeepAlive(o)`，finalizer 可在 C 执行期间释放 remote |
| `odb.go:444-458` `OdbObject.Data()` | 三次 C 调用后无 `KeepAlive(object)`，且返回的 slice 直接指向 C 内存 |

`OdbObject.Data()` 还暴露更深的设计问题：它返回零拷贝视图，文档要求调用方
「must make sure the object is referenced for at least as long as the slice is used」。
**这类「文档约定型」生命周期契约在 Go 中不可靠**——调用方拿到的是普通 `[]byte`，
没有任何东西提示它绑定在某个对象上。更稳妥的做法是提供默认拷贝的 `Data()`
与显式标注风险的 `DataUnsafe()`。

### 1.5 准则：所有权移交必须双向清账

`Refdb.SetBackend` 是本仓库处理最好的一处，可作模板（`refdb.go:47-75`）：

```go
ret := C.git_refdb_set_backend(v.ptr, ptr)
if ret < 0 {
	// libgit2 did not take ownership; the caller may retry or free it.
	return MakeGitError(ret)
}
// Ownership transferred to the refdb.
backend.ptr = nil
backend.owner = nil
return nil
```

**失败路径不释放**（libgit2 未接管，释放即释放非自有内存）、**成功路径置空**
（libgit2 已接管，保留 finalizer 即二次释放）。整合前两条路径都错，是真实的 double-free。

它还做了跨仓库归属校验（`refdb.go:55-57`）——**类型系统表达不出、只能运行期检查**
的约束，属于绑定层应主动增加的防护。

---

## 2. 错误码与异常转换

### 2.1 libgit2 的错误模型：返回码 + 线程局部详情

libgit2 的错误是**两段式**的：函数返回 `int`（`< 0` 为错），详细消息存在
**thread-local storage**：

```c
// include/git2/errors.h:119-125
 * This is kept on a per-thread basis if GIT_THREADS was defined when the
 * library was build, otherwise one is kept globally for the library
```

这对 Go 绑定是**根本性挑战**：Go 运行时可在任意抢占点把 goroutine 迁移到另一个 OS 线程。
若「调用 C 拿到 `< 0`」与「调用 `git_error_last()` 读消息」之间发生迁移，读到的是另一个
线程的 TLS。

### 2.2 准则：把 TLS 约束提升为可自动验证的不变式

统一模式：

```go
runtime.LockOSThread()
defer runtime.UnlockOSThread()

ret := C.git_xxx(...)
if ret < 0 {
	return MakeGitError(ret)
}
```

**本仓库最值得称道的一点：把这条约定做成了 CI 门禁**，而非停留在 code review。
`script/check-MakeGitError-thread-lock.go` 用 AST 遍历每个 `FuncDecl`，若函数体含
`MakeGitError` 却不含线程锁定则构建失败；接入 `Makefile:13/40/62`。

> **通用启示**：凡「必须成对出现」的跨语言约定（锁定/解锁、分配/释放、Track/Untrack），
> 都应有静态检查兜底——人工评审对这类约定的漏检率极高。

该门禁有四个已知边界，使用者应知情：

1. **布尔逻辑是两个否定的合取**（已复核 `:63`）：
   ```go
   strings.Contains(src, "MakeGitError") &&
       !strings.Contains(src, "runtime.LockOSThread()") &&
       !strings.Contains(src, "defer runtime.UnlockOSThread()")
   ```
   只有**两者都缺失**才算违规。只写 `LockOSThread()` 而漏掉 `defer Unlock` 会静默通过
   ——那会永久占住一个 OS 线程。**建议改为要求两者同时存在。**
2. 纯文本匹配，不检查顺序。
3. 不跨函数：把 C 调用封在返回原始 `int` 的 helper 可完全逃逸检查。
4. 注释中出现 `MakeGitError` 会误报。

### 2.3 准则：区分「错误」与「预期信号」

libgit2 用负返回码同时表达语义完全不同的几类东西：

| 类别 | 码 | 正确的 Go 映射 |
|---|---|---|
| 真错误 | `GIT_ERROR`、`GIT_EINVALID` | `error` |
| **控制流信号** | `GIT_ITEROVER` | 不应是 `error` |
| **存在性查询** | `GIT_ENOTFOUND`、`GIT_EEXISTS` | 可判定的 `error` |
| **回调协商** | `GIT_PASSTHROUGH` | 内部使用，不外泄 |
| **回调错误载体** | `GIT_EUSER` | 应被原始 Go `error` 替换 |

`MakeGitError` 对 `ITEROVER` 做了正确的特殊处理（`git.go:245`）——不读 last-error，
避免拿到陈旧消息。

**但对 `ITEROVER` 的外部表达不一致**：`OdbReadStream.Read` 映射为 `io.EOF`
（`odb.go:481-483`，因其实现 `io.Reader` 契约）；`BranchIterator.ForEach`、
`RevWalk.Iterate` 内部吞掉（`branch.go:78`、`walk.go:184`）；而
`ReferenceIterator.Next` 等**直接暴露 `ErrorCodeIterOver`**（`reference.go:456-457`）。

暴露 C 语义本身不错（可被 `IsErrorCode` 判定），但**同一概念在同一库里有三种表达**
会持续制造调用方错误。**建议**统一到一个导出哨兵（`ErrIterOver` 或复用 `io.EOF`），
保留 `ErrorCodeIterOver` 作为底层细节。属破坏性变更，宜与 v2 合并。

### 2.4 准则：Go 回调的错误必须保真往返

回调错误要穿越 `Go → C → libgit2 → C → Go` 四道边界。只靠返回码会让原始 Go `error`
（自定义类型、`%w` 包装链）退化成字符串。

本仓库采用**双通道**，规范写在 `wrapper.c:9-97`——这是整个仓库最重要的一段设计文档。

**通道 1 —— `errorTarget`（同栈，保真）**：

```go
// tree.go:140-142
*data.errorTarget = err
return C.int(ErrorCodeUser)

// tree.go:175-177
if ret == C.int(ErrorCodeUser) && err != nil {
	return err          // 原始 error 对象，未退化
}
```

**通道 2 —— `setCallbackError`（跨栈，降级为消息）**，用于无法回到发起栈的场景
（refdb 后端回调可从任意 libgit2 内部栈触发）：

```go
// git.go:278-287
func setCallbackError(errorMessage **C.char, err error) C.int {
	if err != nil {
		*errorMessage = C.CString(err.Error())
		if gitError, ok := err.(*GitError); ok {
			return C.int(gitError.Code)   // 保留原始 libgit2 码
		}
		return C.int(ErrorCodeUser)
	}
	return C.int(ErrorCodeOK)
}
```

关键细节：**消息由 C 侧写入 TLS，不由 Go 侧写**（`wrapper.c:99-112`）：

```c
/**
 * Sets the thread-local error to the provided string. This needs to happen in
 * C because Go might change Goroutines _just_ before returning, which would
 * lose the contents of the error message.
 */
static int set_callback_error(char *error_message, int ret)
```

这是 §2.1 那条 TLS 约束在**反方向**上的同一推论，处理正确。

### 2.5 待改进：`IsErrorCode` 不兼容错误包装

```go
// git.go:231-239
func IsErrorCode(err error, c ErrorCode) bool {
	if gitError, ok := err.(*GitError); ok {   // 类型断言，非 errors.As
		return gitError.Code == c
	}
	return false
}
```

调用方一旦 `fmt.Errorf("open repo: %w", err)`，判定即失效。现代 Go 库应当：

```go
func IsErrorCode(err error, c ErrorCode) bool {
	var gitError *GitError
	if errors.As(err, &gitError) {
		return gitError.Code == c
	}
	return false
}
```

**向后兼容的纯增强**，无破坏性，建议尽早做。

---

## 3. 回调与异步操作的桥接

### 3.1 cgo 指针规则决定了句柄表的必要性

cgo 规定：传给 C 的内存不得含 Go 指针，且 C 不得在调用返回后继续持有 Go 指针。而 libgit2
的 `void *payload` 会被**长期存储**在 `git_refdb_backend`、`git_remote_callbacks`、
`git_smart_subtransport` 中，跨越多次调用。

因此不能传 `unsafe.Pointer(&goStruct)`。解法（`handles.go:29-37`）：

```go
func (v *HandleList) Track(pointer interface{}) unsafe.Pointer {
	handle := C.malloc(1)        // C 堆 1 字节，GC 不可见
	v.Lock()
	v.handles[handle] = pointer  // map value 构成强引用，反向 pin 住 Go 对象
	v.Unlock()
	return handle
}
```

`C.malloc(1)` 的地址满足三个条件：对 Go GC 不可见（不违反 cgo 规则）、未 free 前唯一稳定
（可作 map key）、value 侧强引用防止回收。**这是该问题的标准解法。**

### 3.2 准则：teardown 期的句柄查找必须宽容

libgit2 可能对同一后端多次调用 `free`。若查找失败即 panic，panic 会穿过 cgo 边界——
**这是未定义行为，不是可恢复的 Go panic**。

本仓库区分两套语义（`handles.go:57-80`）：

```go
// 严格：句柄失效视为 use-after-free，panic
func (v *HandleList) Get(handle unsafe.Pointer) interface{}

// 宽容：供 teardown 期回调使用
func (v *HandleList) GetOk(handle unsafe.Pointer) (interface{}, bool)
```

`GetOk` 是本次整合新增的，refdb 桥的 free 路径使用它。**这个区分正确且必要。**

### 3.3 准则：每个 `//export` 回调都必须有 panic 边界

Go panic 穿过 cgo 边界是未定义行为。因此**每个** `//export` 函数都必须是「panic 隔离舱」。

refdb 桥做得很完整（`refdb_backend.go:233-241`）：

```go
func recoverRefdbBackendCallback(errorMessage **C.char, ret *C.int) {
	if recover() != nil {
		*ret = setCallbackError(errorMessage, &GitError{
			Message: "panic in custom refdb backend callback",
			Class:   ErrorClassCallback,
			Code:    ErrorCodeUser,
		})
	}
}
```

配合**命名返回值**才能在 defer 中改写返回值，17 个返回 int 的回调逐个 defer 它；
两个 `void` 回调用 `defer func() { _ = recover() }()` 保证 C 结构必被释放。

这一实践原本只存在于 refdb 桥（18/56），其余 38 个回调无防护，其中若干还会**主动 panic**
（`pointerHandles.Get` 句柄失效、`panic("invalid treewalk callback")` 等）。
2026-09-11 已补齐至 **56/56**，并由构建门禁保证。

补齐时要区分三种错误通道，否则会引入比崩溃更糟的行为：

| 回调形态 | 守卫 | panic 时的返回 |
|---|---|---|
| 有 `errorMessage **C.char` 出参 | `recoverCallback(errorMessage, &ret, name)` | 原始 panic 值写入 libgit2 错误消息 |
| 只有返回码（原始 error 走 `errorTarget`） | `recoverCallbackCode(&ret)` | `GIT_EUSER`，让 libgit2 中止 |
| 无返回值 | `recoverVoidCallback()` | 仅包含 panic（无错误通道可用） |

**中间一类最容易做错**：如果给它套上「吞掉 panic」的 void 守卫，函数会返回零值 `0`，
而 `0` 在 libgit2 的约定里是**成功**——libgit2 会在一个从未执行完的回调之上继续推进。
这正是 §0 明令禁止的「静默降级」，比让它崩溃更危险。

### 3.4 迭代器桥接：C 契约比 Go 接口更苛刻

`git_reference_iterator.next_name` 的契约要求**迭代器自己持有名字内存直到下次调用**。
Go 侧若直接返回 `string`，跨边界后会被 GC。

处理方式（`wrapper.c:808-838`）展示了「用 C 侧状态弥合契约差异」这一通用手法：

```c
static int _go_refdb_iter_next_name(const char **ref_name, git_reference_iterator *iter)
{
	_go_managed_refdb_iterator *managed = (_go_managed_refdb_iterator *)iter;
	free(managed->last_name);                  // 1) 释放上一轮
	managed->last_name = NULL;
	ret = _go_refdb_iter_next(&ref, iter);     // 2) 复用 next
	if (ret != 0) return ret;
	managed->last_name = strdup(git_reference_name(ref));   // 3) 复制
	git_reference_free(ref);                                // 4) 立即释放
	*ref_name = managed->last_name;
	return 0;
}
```

代价是 `next_name` 比原生多一次 reference 分配，**换来 Go 接口只需实现
`Next() (*Reference, error)`**（`refdb_backend.go:70-79`）。这是正确取舍：把复杂度留在
C 胶水层，让 Go 侧接口保持最小。

它还示范了另一条准则：**分配失败路径也要清账**（`wrapper.c:848-852`）：

```c
_go_managed_refdb_iterator *iter = calloc(1, sizeof(*iter));
if (!iter) {
	refdbBackendIteratorFreeCallback(iterator_handle);   // OOM 时不泄漏 Go 句柄
	git_error_set_oom();
	return -1;
}
```

### 3.5 异步：goroutine 只应存在于「libgit2 主动让出控制权」处

C 回调内起 goroutine 是危险的：libgit2 不知道有并发发生，其内部状态未必线程安全。

本仓库仅在一处使用（managed HTTP transport, `http.go:201-206`），因为 smart transport
协议本身要求「发请求」与「读响应」可交错。三层同步：

1. **`sync.WaitGroup recvReply`**：`Read` 等响应就绪。关键是**先发布结果，后 Done**
   （`http.go:208-217`），保证 `Wait()` 返回后必然可见：
   ```go
   func (self *httpSmartSubtransportStream) sendRequest() error {
       defer self.recvReply.Done()
       err := self.doSendRequest()
       self.setHTTPError(err)     // 先 publish
       return err
   }
   ```
2. **`sync.Mutex httpErrorMu`**：因为 `Write` **不** Wait 就读 `httpError`
   （`http.go:136-141` 有明确注释）——这是本次整合修复的真实数据竞争。
3. **`sentRequest` 的所有权划分**：以注释显式论证「只由发起 goroutine 写」
   （`http.go:131-134`、`271-272`），从而无需加锁。

> **准则**：凡「靠所有权划分而非加锁」保证的安全性，必须在注释中写出论证。
> 否则后续维护者无法判断这是深思熟虑还是遗漏。本仓库这一点做得很好。

对应验证手段也已制度化（`Makefile:71-75`）：

```make
# The refdb backend bridge and the managed HTTP transport both hand memory
# between libgit2's threads and Go, so the race detector is part of the
# supported test matrix rather than an optional extra.
test-static-race: ...
```

---

## 4. 数据结构与 API 设计的对齐

### 4.1 options 结构：必须走官方 init

libgit2 的 options 结构带 `version` 字段，且**未来可能新增字段**。手工零值初始化会在上游
新增字段后静默失效（新字段拿到 0，语义未定义）。上游提供 `git_*_options_init` 正是为此。

本仓库 24 处 populate 全部调用官方 init，无一例外用手工 memset ——**应保持的纪律**。

其中 `InitRepositoryExt` 是唯一检查 init 返回值的（`repository.go:278`）：

```go
if ret := C.git_repository_init_options_init(&copts, C.GIT_REPOSITORY_INIT_OPTIONS_VERSION); ret < 0 {
	return nil, MakeGitError(ret)
}
```

**建议**：其余 23 处也应检查——init 失败意味着版本协商失败，静默忽略等于带着未初始化的
结构继续走。

**一处偏离**：`diff.go:200-208` `FindSimilar` 用复合字面量手写
`version: C.GIT_DIFF_FIND_OPTIONS_VERSION`，绕过 `git_diff_find_options_init`，应统一。

C 胶水层维持同样纪律，且把 Go 零值语义正确映射到 libgit2 的「未设置」约定
（`wrapper.c:591-596`）：

```c
git_index_options opts = GIT_INDEX_OPTIONS_INIT;
opts.oid_type = oid_type ? (git_oid_t)oid_type : GIT_OID_DEFAULT;
```

与 Go 侧 `ObjectIdTypeDefault = 0`（`oid.go:16-20`）严格对应。

### 4.2 `git_strarray`：必须区分谁分配

这是最易出错处。**libgit2 分配的必须用 `git_strarray_dispose`；git2go 自己分配的必须用
自己的 `freeStrarray`。** 两者当前实现巧合一致，但依赖实现细节而非 API 契约。

本仓库基本正确（`freeStrarray` 15 处、`git_strarray_dispose` 5 处），
**一处混用**：`remote.go:851-852` 对 `git_remote_rename` 的**输出**用了 `freeStrarray`。
虽当前无害，但应改为 `git_strarray_dispose` 以贴合契约。

`freeStrarray` 还做了清零（`remote.go:957-958`），防二次释放——好实践：

```go
C.free(unsafe.Pointer(arr.strings))
arr.strings = nil
arr.count = 0
```

### 4.3 C 数组转 Go slice：`unsafe.Slice` 取代 `reflect.SliceHeader`

`reflect.SliceHeader` 自 Go 1.20 起 deprecated，且构造它的常见写法在 `-d=checkptr` 下会
报告违规。本次整合已把 8 处转换迁移到 `unsafe.Slice`（`merge.go`、`remote.go`、
`message.go`、`rebase.go`、`repository.go`），`go vet` 由此转清。

**仍有 4 个文件残留**（已复核）：`transport.go`、`odb.go`、`indexer.go`、`blob.go`。
这些不是「数组转换」而是「把 C 缓冲区当 Go slice 用」，其中最难迁移的是 `odb.go:469-485`
——它**写回 `header.Len = int(ret)`** 以原地改长度，`unsafe.Slice` 无法直接表达。
等价改法是先按容量建 slice 再切片：

```go
buf := unsafe.Slice((*byte)(unsafe.Pointer(cbuf)), cap)[:n]
```

### 4.4 值类型的特殊责任：`Oid` 案例

`Oid` 是唯一**故意不嵌入 `doNotCompare`** 的类型（`oid_typed.go:13-25`），因为它必须可比较、
可作 map key。这带来一个非显然的约束：**任何写入 `Oid` 的操作都可能破坏已插入的 map key**。

解法是把「输入」与「输出」分成两个方法（`oid_typed.go:89-129`）：

```go
// toC 用于只读入参：零值 Oid 通过归一化副本兜底，不修改接收者
func (oid *Oid) toC() *C.git_oid {
	if oid.kind == 0 {
		normalized := *oid              // 拷贝，而非原地改
		normalized.kind = uint8(ObjectIdSHA1)
		return (*C.git_oid)(unsafe.Pointer(&normalized))
	}
	return (*C.git_oid)(unsafe.Pointer(oid))
}

// outC 用于输出参数：不拷贝，C 会填充
func (oid *Oid) outC() *C.git_oid
```

注释写明动机：「a silent in-place mutation would change the key of an already-inserted
entry」。**这是「值类型 + cgo」组合下必须显式处理的问题**，容易被忽略。

### 4.5 准则：C API 有正确性缺陷时，绑定层应绕开而非转发

`ShortenOids` 是范例。libgit2 的 `git_oid_shorten` 只检查前 40 个 hex 字符，对第 40 位之后
才分叉的 SHA256 id 会返回**不足以区分**的前缀长度。

本仓库选择用纯 Go 重新实现（`oid.go:129-136` 及后续），而非转发一个已知会给出错误答案的
C API。**这个判断是对的**：绑定层的契约是「给出正确结果」，不是「忠实复述上游」。

同类判断还有：Go 侧预校验枚举值（`oid.go:33-40`）、NUL 字节检查
（`transaction.go:76-81`、`ssh.go:75-88`）。后者尤其重要——C 字符串无法承载 NUL，
不检查就会**静默截断**，而截断后的路径可能指向完全不同的对象。

### 4.6 一处 API 不一致

`Free()` 的签名分裂成两派：多数无返回值，少数返回 `error` + `ErrInvalid` 哨兵
（`diff.go:181`、`patch.go:30`、`blame.go:132`、`note.go:155`）。这是历史遗留。
Go 的惯例是 `Close() error`，但释放函数返回错误后调用方通常无法处置。
**建议统一为无返回值 + 幂等**，与 §1.3 一并处理。

---

## 5. 版本兼容性与线程安全策略

### 5.1 准则：能力探测优先于版本比较

libgit2 main 至今自报 `1.9.x`，因此**版本号不可信**。同时，同一版本号的库可能因编译选项
不同而缺少 SHA256 provider 或 reftable。

本仓库因此建立了三层策略，这是本文认为最值得推广的一点：

| 层次 | 手段 | 回答的问题 |
|---|---|---|
| **编译期** | `git2go_version_check.h` 静态断言 | 头文件的 ABI 是否匹配？ |
| **进程启动期** | `oid_typed.go:27-36` `init()` panic | Go 与 C 的结构布局是否一致？ |
| **运行期** | `Features()` / `IsSha256Supported()` / `IsReftableSupported()` | 这个库实际能做什么？ |

编译期断言的价值在于**把「运行期静默损坏」换成「构建失败」**
（`git2go_version_check.h:48-52`）：

```c
/*
 * The Go Oid struct mirrors git_oid field-for-field and is converted by an
 * unchecked pointer cast, so any drift in size, field order or enum values
 * must break the build rather than corrupt object ids at runtime.
 */
typedef char git2go_git_oid_size_must_be_33[(sizeof(git_oid) == 33) ? 1 : -1];
typedef char git2go_git_oid_type_offset_must_be_0[(offsetof(git_oid, type) == 0) ? 1 : -1];
```

一个容易踩的坑已被记录在案（同文件 22-26 行）：必须用 `LIBGIT2_VERSION_*` 而**不是**
`LIBGIT2_VER_*`，因为后者在 `GIT_DEPRECATE_HARD` 构建下未定义，预处理器会**静默按 0 比较**
——守卫失效而无任何提示。**这正是「静默降级」的典型形态，也是 §0 优先级的现实例证。**

运行期探测的必要性也有明确论证（`oid_type_api.go:38-40`）：

> Unlike a version comparison this is a genuine capability probe: libgit2 can be
> compiled without a SHA256 provider even when the headers declare the typed
> object id API.

reftable 无 feature flag，只能真的建仓库探测（`reftable_on.go:86-114`），并用 `sync.Once`
缓存——因为结果只取决于链接的库，进程生命周期内不变。

### 5.2 准则：能力缺失应降级为 SKIP 而非 FAIL

`libgit2_no_reftable` 构建标签（`reftable_off.go`）提供 files-only 降级实现：
`applyRefdbType` 对非默认值返回错误、`IsReftableSupported()` 恒 false。
测试相应降级为 SKIP（实测 202 通过 / 7 跳过）。

**极性选择值得说明**：本仓库把 reftable 设为**默认编入**，用 `libgit2_no_reftable` 选择性
关闭；而上游 reftable 分支曾用 `libgit2_reftable` 选择性开启。默认值应匹配**目标基线的
实际能力**——pin 的 libgit2 始终含 reftable，因此默认开启是正确的。

### 5.3 线程安全：三条边界

**边界一：进入 C 必须锁定 OS 线程**（§2.2 已述）。

**边界二：从 C 回调进入 Go 时不应锁定。** 19 个 refdb 回调、9 个 remote 回调、
8 个 transport 回调均不加锁——**这是正确的**：它们已在 libgit2 自己的线程上执行，
错误消息由 C 侧 `set_callback_error` 在同一 C 栈帧写入 TLS。

两个例外都有充分理由：`transport.go:323-326` 与 `ssh.go:173-174` 在回调内加锁，
因为它们会**反向调用 libgit2**（`SmartCredentials` 等），那条路径要读 last-error。

**边界三：全局状态。** `git.go:159-166` 对无线程支持的库直接 panic，理由写得很坦率：

```go
// Due to the multithreaded nature of Go and its interaction with
// calling C functions, we cannot work with a library that was not built
// with multi-threading support. The most likely outcome is a segfault
// or panic at an incomprehensible time, so let's make it easy by
// panicking right here.
```

**这是 §0 优先级原则的一次正确应用**：把「难以理解的时刻的段错误」换成「启动时的明确
panic」。

### 5.4 init/shutdown：引用计数与顺序

libgit2 的 init 是**引用计数**而非幂等布尔（`include/git2/global.h:20-45`）。
本仓库在包 `init()` 中调用一次，`Shutdown()` 中调用一次，配对正确；关闭顺序严格逆序
（`git.go:186-198`）：

```go
func Shutdown() {
	unregisterManagedTransports()   // ① 先注销（依赖 pointerHandles）
	pointerHandles.Clear()          // ② 释放 C.malloc 句柄
	remotePointers.clear()          // ③ free remote
	C.git_libgit2_shutdown()        // ④ 最后关 C
}
```

得益于 C 侧引用计数，宿主程序若也调 `git_libgit2_init` 是安全的。但两点应知情：

1. `Shutdown()` **不撤销** `C.git_openssl_set_locking()`（`git.go:177`）。该调用本身被自己
   的注释批评：「we may be stomping all over someone else's setup」（`git.go:173-176`）。
   一个库修改进程级 OpenSSL 全局状态是有争议的，长期应移除或改为可选。
2. `Shutdown()` 只清 git2go 的句柄表，**不释放调用方仍持有的对象**；前置条件
   「no references to any git2go objects are live」是纯文档约定，无强制。

### 5.5 一处并发窗口

`RemoteCollection.remotes` 与 `remotePointers.pointers` 维护同一份信息的两张表，
`trackRemote`（`remote.go:674-680`）先锁 collection 再调 `remotePointers.track`，
**两把锁非原子**；`Remote.Free()` 同样跨两张表。当前无并发触发路径，但属结构性隐患
——两张表本应合并为一张。

值得肯定的是两处「锁内快照、锁外操作」的正确写法（`remote.go:818-830`、
`transport.go:46-49`），避免了在持锁状态下回调用户代码。

---

## 6. 现状评估与技术债清单

### 6.1 已达到标准第三方库水准的方面

| 方面 | 依据 |
|---|---|
| TLS 错误约束制度化 | CI 门禁 `check-MakeGitError-thread-lock.go`，非仅靠评审 |
| 回调错误双通道保真 | `errorTarget` + `setCallbackError`，规范文档在 `wrapper.c:9-97` |
| 所有权移交清账 | `Refdb.SetBackend` 双向正确，含跨仓库归属校验 |
| 三层版本/能力策略 | 编译期静态断言 + init 期布局校验 + 运行期能力探测 |
| ABI 漂移防护 | `git2go_version_check.h` 5 条静态断言，把静默损坏变为构建失败 |
| options 纪律 | 24/24 走官方 `*_options_init` |
| 并发论证成文 | `http.go` 对「靠所有权划分而非加锁」给出显式论证 |
| 竞态检测入矩阵 | `make test-static-race` 为一等公民 |
| 生命周期与回调约定制度化 | `script/check-binding-invariants.go` 门禁：56/56 回调有 panic 边界，所有 Free 幂等 |

### 6.2 技术债（按建议优先级）

| # | 项 | 位置 | 性质 | 破坏性 |
|---|---|---|---|---|
| ~~1~~ | ~~`Remote.Free()` 二次调用 nil panic~~ | `remote.go` | **已修复** 2026-09-11，`TestRemoteFreeIsIdempotent` 固化 | — |
| ~~2~~ | ~~38/56 回调无 panic 边界~~ | 全部 | **已修复** 2026-09-11，56/56 并入门禁 | — |
| ~~3~~ | ~~`Free()` 普遍非幂等（28 处）~~ | 见 §1.3 | **已修复** 2026-09-11，并入门禁 | — |
| 4 | `PruneRefs`、`OdbObject.Data` 缺 `KeepAlive` | `remote.go:1236`、`odb.go:444` | finalizer 竞争 | 无 |
| 5 | `IsErrorCode` 不兼容 `%w` 包装 | `git.go:231` | 判定静默失效 | 无（纯增强） |
| 6 | `ReflogEntry` 持裸借用指针 | `reflog.go:41-55` | 悬垂窗口 | 无 |
| 7 | 门禁脚本布尔逻辑过宽 | `check-…:63` | 漏检半边锁定 | 无 |
| 8 | `reflect.SliceHeader` 残留 4 文件 | `odb.go` 等 | deprecated + checkptr | 无 |
| 9 | `git_strarray_dispose` 混用 1 处 | `remote.go:851` | 依赖实现细节 | 无 |
| 10 | 23/24 处不检查 options init 返回值 | 见 §4.1 | 版本协商失败被忽略 | 无 |
| 11 | 迭代器结束语义三种表达 | 见 §2.3 | API 一致性 | **有** |
| 12 | `Free()` 签名两派 | 见 §4.6 | API 一致性 | **有** |
| 13 | 双表非原子 | `remote.go:674` | 结构性隐患 | 无 |
| 14 | `git_openssl_set_locking` 改全局态 | `git.go:177` | 越界副作用 | **有** |

第 1–10 项均为**非破坏性**，可在 v36-pre 线内逐步修复。
第 11、12、14 项属破坏性变更，**宜与 libgit2 v2 适配合并发布**，避免多次破坏调用方。

### 6.3 与 libgit2 v2 适配的关联

本文分析指向一个结论：**v2 适配不应只是「跟上新 API」，而是清理上述 API 一致性债的唯一
低成本窗口**。具体地：

- 若 v2 调整 refdb vtable（如引入 `transaction_prepare/finish/abort`，见
  `docs/reftable-transaction-research.md` §3 方案 D），`RefdbBackendInterface` 必然要扩展
  ——此时一并统一迭代器语义与 `Free()` 签名，只需调用方迁移一次。
- `git2go_version_check.h` 的静态断言是 v2 的**早期预警装置**：若 v2 改动 `git_oid` 布局或
  枚举值，构建会立即失败而非运行期损坏。该装置应在 v2 适配中扩展覆盖新的关键结构。
- 能力探测优先于版本比较的策略，使 v2 过渡期可以「同一份代码同时支持 v1.9 main 与 v2」
  ——前提是不要退回版本号判断。

### 6.4 reftable 事务：一个「不应绕过」的判断范例

`docs/reftable-transaction-research.md` 记录的判断也属于本文的准则体系。libgit2 的 refdb
vtable 是 per-ref 锁（为 files 的 `.lock` 模型定制），而 reftable 的原子性单位是持有整库
锁的一个 addition。实测第二次 addition 返回 `REFTABLE_LOCK_ERROR (-5)`。

绑定层面对这种失配时，**唯一正确的选择是快速失败并提供事前探测**
（`Repository.RefStorageFormat()`），而不是在 Go 侧模拟批量提交——后者会产出「看似原子
实则不原子」的假象，正落在 §0 明令禁止的「静默降级」一档。

有一个不对称值得记录：**git2go 自己的 refdb 桥支持 lock/unlock**
（`refdb_backend.go:119-131` + capability bit2），因此 Go 自定义后端可以实现事务，
而 libgit2 内建的 reftable 后端不能。

---

## 7. 检查清单（新增绑定时逐项核对）

**内存**
- [ ] 查上游头文件注释，确认所有权类别（拥有/借用/库有/移交）
- [ ] 借用型：深拷贝，不留裸指针
- [ ] 拥有型：`Free()` 幂等（nil 检查 → 置空 → 解 finalizer → 释放）
- [ ] 每个传 `ptr` 进 C 的调用后有 `runtime.KeepAlive`
- [ ] 依赖父对象者：存父引用 **且** `KeepAlive(parent)`
- [ ] 移交给 C 者：成功置空 + 解 finalizer；失败不释放

**错误**
- [ ] `runtime.LockOSThread()` + `defer runtime.UnlockOSThread()` 成对
- [ ] `ret < 0` → `MakeGitError(ret)`
- [ ] 预期码（NotFound / ITEROVER）按语义转换，不当作错误抛出
- [ ] 新错误类型支持 `errors.As`

**回调**
- [ ] payload 走 `pointerHandles.Track`，绝不传 Go 指针
- [ ] `//export` 首行 `defer recoverCallback(...)`（需命名返回值）
- [ ] teardown 路径用 `GetOk` 而非 `Get`
- [ ] 错误经 `errorTarget`（同栈）或 `setCallbackError`（跨栈）
- [ ] 分配失败路径不泄漏已 Track 的句柄

**结构**
- [ ] options 走官方 `git_*_options_init` 并检查返回值
- [ ] strarray 按分配方选择 `freeStrarray` / `git_strarray_dispose`
- [ ] 数组转换用 `unsafe.Slice`，不用 `reflect.SliceHeader`
- [ ] 字符串入参检查 NUL；枚举入参预校验
- [ ] 上游 API 有正确性缺陷时，绕开而非转发

**版本与并发**
- [ ] 新增 ABI 假设 → 加静态断言
- [ ] 能力判断用探测，不用版本号
- [ ] 能力缺失 → 测试 SKIP，而非 FAIL
- [ ] 新增 goroutine → 写出同步论证，并纳入 `-race` 矩阵

---

## 8. 约定的机器化执行

文档会腐化，构建门禁不会。本仓库把两类跨语言约定编码成了构建步骤：

| 门禁 | 强制的不变式 |
|---|---|
| `script/check-MakeGitError-thread-lock.go` | 读取 libgit2 线程局部错误前必须锁定 OS 线程（§2.2） |
| `script/check-binding-invariants.go` | 每个 `//export` 回调有 panic 边界（§3.3）；每个释放 C 指针的 `Free()` 幂等（§1.3） |

两者都接入 `make lint` 与各 `test-*` target，并在 CI 中作为独立 job 运行。

### 为什么这两条特别需要机器执行

它们都属于「**局部看起来完全正常、只在特定时序下出错**」的约定：

- 漏掉 `LockOSThread` 的代码在单线程测试里永远正确，只有在 goroutine 恰好被迁移时才读到
  错误的 TLS；
- 漏掉 panic 边界的回调在用户代码不 panic 时永远正确；
- 非幂等 `Free()` 在调用方不重复释放时永远正确。

人工评审对这类约定的漏检率极高——本仓库在整合前有 38/56 个回调无防护、28 个非幂等
`Free()`，且都通过了历次评审。

### 故意例外的表达方式

两条规则都支持在声明上方写明的选择退出注释，使例外在评审中可见而非被默默容忍：

```go
//git2go:allow-unguarded-callback <reason>
//git2go:allow-nonidempotent-free <reason>
```

检查器另有一个短的白名单（`freeAllowlist`），用于那些以其他等价方式保证幂等的方法。
**白名单应当保持简短并附理由**；它每增长一项，规则的价值就削弱一分。

### 检查器的已知边界

与 §2.2 列出的门禁边界同理，这个检查器也是语法层面的近似：

- `free-idempotent` 只认「前两条语句中出现含 `nil` 的 `if`」这一形态。用别的方式实现幂等
  （例如先 `Untrack` 再判断）会被误报，需要走白名单或选择退出注释。
- 它只对**调用了 `C.git_*` / `C._go_git_*` / `C.free`** 的 `Free()` 生效；纯 Go 的
  `Free()`（测试替身、接口实现）不在范围内。
- `callback-panic-guard` 接受出现在函数体任意位置的 `defer ... recover`，因为 refdb 的
  teardown 回调需要先解析并 untrack 句柄，再只保护用户的 `Free` 调用。因此它保证的是
  「存在 panic 边界」，而不是「边界覆盖了整个函数体」。

这些边界应当在扩展规则时一并收紧，而不是假装不存在。
