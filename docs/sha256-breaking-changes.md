# SHA1 / SHA256 统一对象 ID 支持：Breaking Changes 清单

> 目标发版：待上游 libgit2 发布包含 SHA256 转正的正式版本后确定（建议 git2go 新主版本）。
>
> 基线：libgit2 `main` @ `939362a3cb575de5f2aaebe1b1732c4ec8c1aebb`。
>
> 重要兼容性结论：**libgit2 没有移除 SHA1。** SHA1 仍是 `GIT_OID_DEFAULT`，默认创建的
> 仓库、`NewOidFromBytes`、`NewIndex`、`NewIndexer` 等仍使用 SHA1。此次变化是让 SHA1 与
> SHA256 共用上游唯一的 typed `git_oid` ABI，而不是用 SHA256 替换 SHA1。

## Changelog 摘要（可直接用于发版说明）

### Breaking

- `git.Oid` 从公开的 `[20]byte` 数组变为可同时表示 SHA1/SHA256 的结构体。依赖数组索引、
  切片、数组转换、固定长度、反射布局或 `unsafe` 布局的代码必须迁移。
- git2go 现在要求链接包含**已转正 typed `git_oid` ABI** 的 libgit2；旧版 libgit2 1.9.x
  发布包（默认 20 字节 `git_oid` ABI）不再兼容。当前 vendored main 的版本头仍报告
  `1.9.0`，因此构建额外检查 `GIT_OID_SHA256_SIZE`，而不能只依赖版本号。
- 删除 Go build tags `git_experimental_sha256` 和 `libgit2_next`。在构建命令中保留它们不再
  改变行为；应从 CI、Makefile 和下游构建脚本中移除。
- 删除 `EXPERIMENTAL_SHA256=ON` 构建方式及 `libgit2-experimental` 库/头/pkg-config
  布局。SHA1 与 SHA256 现在由常规 `libgit2` / `libgit2.pc` 同时提供。
- `NewOidFromBytesWithType` 采用 `(*Oid, error)` 返回值并校验类型和最小输入长度；调用方必须
  处理错误。`NewOidFromBytes` 保留原来的单返回值形式并继续构造 SHA1 oid。
- `Oid.Equal` 现在按“规范化类型 + 有效原始字节”比较，而不是对 Go 存储结构做直接 `==`。
  零值 `Oid{}` 被规范化为 all-zero SHA1 oid；这使它与 libgit2 返回的 SHA1 zero oid
  语义一致，但可能改变依赖旧结构体位模式比较的代码。
- `Oid.Cmp` / `Oid.NCmp` 现在与 libgit2 一致：先按 oid 类型排序，再比较有效原始字节。
  SHA1 与 SHA256 即使原始前缀相同也不会比较为相等。依赖旧的“仅字节比较”行为需调整。
- `Oid` 的 Go 零值现在语义化为 all-zero SHA1 id；因此 `Type()` 返回 `ObjectIdSHA1`、
  `Bytes()` 返回 20 个零字节、`String()` 返回 40 个零。依赖“未初始化类型值为 0”的代码会变化。
- `fmt` / JSON / gob / `reflect.DeepEqual` 若直接观察 `Oid` 底层表示，输出或比较结果可能变化；
  对外交换应使用 `String()` / `Bytes()`，语义比较使用 `Equal()`。
- 直接写 `Oid{...}` 的位置复合字面量不再可用（结构体字段为非导出）；使用 `NewOid`、
  `NewOidFromBytes` 或 `NewOidFromBytesWithType`。
- `unsafe.Sizeof(Oid{})`、C 互操作、二进制内存拷贝与持久化布局均发生变化；禁止依赖旧的
  20 字节 Go 内存布局。C 侧必须使用同一版本 libgit2 的 `git_oid`。
- 旧 libgit2 1.9.x experimental overload ABI 与 SHA1-only 默认 ABI 均不再被同一构建兼容；
  如需继续支持，应停留在上一版 git2go，而不是与新 typed ABI 混链。

### Build and dependency compatibility

- 最低 libgit2 要求不再能仅用 `LIBGIT2_VERSION` 表达：当前转正 main 仍报告 1.9.0，构建以
  `GIT_OID_SHA256_SIZE` 作为 typed oid 能力探针。正式版本发布后会改为明确版本下限。
- bundled static 构建从 `libgit2-experimental.pc` / `libgit2-experimental.a` 迁回
  `libgit2.pc` / `libgit2.a`。
- 旧的 `EXPERIMENTAL_SHA256` 环境变量、`git_experimental_sha256` 与 `libgit2_next` tags、
  `test-static-sha256*` Makefile 目标均被删除。
- 如果下游维护双轨（旧 libgit2 + 新 libgit2），必须使用不同 git2go 主版本；不能在单个
  二进制中混用两种 `git_oid` ABI。

### Added / promoted to regular API

以下 API 不再受 build tag 门控，在每个构建中都可用：

- `ObjectIdType`, `ObjectIdSHA1`, `ObjectIdSHA256`
- `(*Oid).Type()`, `(*Oid).Bytes()`
- `NewOidFromBytesWithType`
- `InitRepositoryWithOidType`
- `(*Repository).OidType()`
- `(*Odb).HashWithType()`
- `NewIndexerForOidType`
- `NewIndexWithOidType`, `OpenIndexWithOidType`
- `DiffFromBufferWithOidType`

### Behavioral compatibility retained

- SHA1 **仍被完整支持且仍是默认类型**：
  - `InitRepository` 创建 SHA1 仓库；
  - `NewOidFromBytes` 创建 SHA1 oid；
  - `NewOdb` / `NewIndex` / `OpenIndex` / `NewIndexer` / `DiffFromBuffer` 默认 SHA1；
  - 40-hex 字符串仍由 `NewOid` 解析为 SHA1；
  - `Oid.String()` 对 SHA1 仍输出 40 hex，`Bytes()` 仍返回 20 bytes。
- 64-hex 字符串由 `NewOid` 解析为 SHA256；SHA256 的 `String()` / `Bytes()` 分别为
  64 hex / 32 bytes。
- `Oid` 仍是 comparable，可继续作为 map key，也可使用 `==`；但推荐使用 `Equal`，以获得
  零值规范化语义。
- `ObjectIdSHA1` 与 `ObjectIdSHA256` 的数值继续对应 libgit2 的 `git_oid_t`（1/2）。

## 迁移示例

### 数组/切片访问

旧代码：

```go
var oid git.Oid
first := oid[0]
raw := oid[:]
```

新代码：

```go
var oid git.Oid
raw := oid.Bytes()
first := raw[0]
```

### 构造 SHA1

旧代码与新代码均可：

```go
oid := git.NewOidFromBytes(raw20)
```

需要错误校验或显式类型时：

```go
oid, err := git.NewOidFromBytesWithType(raw20, git.ObjectIdSHA1)
```

### 构造 SHA256

```go
oid, err := git.NewOidFromBytesWithType(raw32, git.ObjectIdSHA256)
```

### 创建仓库

```go
sha1Repo, err := git.InitRepository(path1, false) // 默认仍是 SHA1
sha256Repo, err := git.InitRepositoryWithOidType(path2, false, git.ObjectIdSHA256)
```

### 构建命令

旧命令：

```sh
EXPERIMENTAL_SHA256=ON ./script/build-libgit2.sh --static
go test -tags "static git_experimental_sha256 libgit2_next" ./...
```

新命令：

```sh
./script/build-libgit2.sh --static
go test -tags static ./...
```

## 发版前待确认

- 上游正式发布版本号及最低兼容版本；当前 main 仍报告 1.9.0。
- git2go 模块主版本是否从 `v35` 提升（建议提升，因为 `Oid` 的公开类型发生破坏性变化）。
- README 的 git2go/libgit2 版本映射表和三个 `Build_*.go` 的版本守卫同步到正式版本。
- 在最终目标 Linux/Windows/macOS CI 上跑常规静态、动态和 system_libgit2 组合。
