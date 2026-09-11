package git

/*
#include <git2.h>

extern int _go_git_odb_backend_one_pack(git_odb_backend **out, const char *index_file, int oid_type);
extern int _go_git_odb_backend_loose(git_odb_backend **out, const char *objects_dir, int compression_level, int do_fsync, unsigned int dir_mode, unsigned int file_mode, int oid_type);
extern int _go_git_odb_new(git_odb **out, int oid_type);
extern int _go_git_odb_hash(git_oid *out, const void *data, size_t len, git_object_t obj_type, int oid_type);
extern int _go_git_object_id_from_file(git_oid *out, const char *path, git_object_t obj_type, int oid_type);
extern int _go_git_odb_foreach(git_odb *db, void *payload);
extern void _go_git_odb_backend_free(git_odb_backend *backend);
extern int _go_git_odb_write_pack(git_odb_writepack **out, git_odb *db, void *progress_payload);
extern int _go_git_odb_writepack_append(git_odb_writepack *writepack, const void *, size_t, git_indexer_progress *);
extern int _go_git_odb_writepack_commit(git_odb_writepack *writepack, git_indexer_progress *);
extern void _go_git_odb_writepack_free(git_odb_writepack *writepack);
*/
import "C"
import (
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"unsafe"
)

type Odb struct {
	doNotCompare
	ptr *C.git_odb

	// oidType is the object id type this database stores, following git_oid_t.
	// Repository.Odb() inherits it from the repository; standalone constructors
	// set it explicitly (NewOdb defaults to SHA1).
	oidType C.int
}

type OdbBackend struct {
	doNotCompare
	ptr *C.git_odb_backend
}

// NewOdb creates a standalone SHA1 object database with no backends.
func NewOdb() (*Odb, error) {
	return newOdbWithOidType(ObjectIdSHA1)
}

func newOdbWithOidType(oidType ObjectIdType) (odb *Odb, err error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	odb = &Odb{oidType: C.int(oidType)}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C._go_git_odb_new(&odb.ptr, C.int(oidType))
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	runtime.SetFinalizer(odb, (*Odb).Free)
	return odb, nil
}

func NewOdbBackendFromC(ptr unsafe.Pointer) (backend *OdbBackend) {
	backend = &OdbBackend{ptr: (*C.git_odb_backend)(ptr)}
	return backend
}

func (v *Odb) AddAlternate(backend *OdbBackend, priority int) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_add_alternate(v.ptr, backend.ptr, C.int(priority))
	runtime.KeepAlive(v)
	if ret < 0 {
		backend.Free()
		return MakeGitError(ret)
	}
	return nil
}

func (v *Odb) AddBackend(backend *OdbBackend, priority int) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_add_backend(v.ptr, backend.ptr, C.int(priority))
	runtime.KeepAlive(v)
	if ret < 0 {
		backend.Free()
		return MakeGitError(ret)
	}
	return nil
}

// NewOdbBackendOnePack creates a SHA1 backend for a single packfile.
func NewOdbBackendOnePack(packfileIndexPath string) (*OdbBackend, error) {
	return newOdbBackendOnePackWithOidType(packfileIndexPath, ObjectIdSHA1)
}

func newOdbBackendOnePackWithOidType(packfileIndexPath string, oidType ObjectIdType) (backend *OdbBackend, err error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cstr := C.CString(packfileIndexPath)
	defer C.free(unsafe.Pointer(cstr))

	var odbOnePack *C.git_odb_backend
	ret := C._go_git_odb_backend_one_pack(&odbOnePack, cstr, C.int(oidType))
	if ret < 0 {
		return nil, MakeGitError(ret)
	}
	return NewOdbBackendFromC(unsafe.Pointer(odbOnePack)), nil
}

// NewOdbBackendLoose creates a SHA1 backend for loose objects.
func NewOdbBackendLoose(objectsDir string, compressionLevel int, doFsync bool, dirMode os.FileMode, fileMode os.FileMode) (*OdbBackend, error) {
	return newOdbBackendLooseWithOidType(objectsDir, compressionLevel, doFsync, dirMode, fileMode, ObjectIdSHA1)
}

func newOdbBackendLooseWithOidType(objectsDir string, compressionLevel int, doFsync bool, dirMode os.FileMode, fileMode os.FileMode, oidType ObjectIdType) (backend *OdbBackend, err error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var odbLoose *C.git_odb_backend
	var doFsyncInt C.int
	if doFsync {
		doFsyncInt = C.int(1)
	}

	cstr := C.CString(objectsDir)
	defer C.free(unsafe.Pointer(cstr))

	ret := C._go_git_odb_backend_loose(&odbLoose, cstr, C.int(compressionLevel), doFsyncInt, C.uint(dirMode), C.uint(fileMode), C.int(oidType))
	if ret < 0 {
		return nil, MakeGitError(ret)
	}
	return NewOdbBackendFromC(unsafe.Pointer(odbLoose)), nil
}

func (v *Odb) ReadHeader(oid *Oid) (uint64, ObjectType, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var sz C.size_t
	var cotype C.git_object_t

	ret := C.git_odb_read_header(&sz, &cotype, v.ptr, oid.toC())
	runtime.KeepAlive(v)
	if ret < 0 {
		return 0, ObjectInvalid, MakeGitError(ret)
	}

	return uint64(sz), ObjectType(cotype), nil
}

func (v *Odb) Exists(oid *Oid) bool {
	ret := C.git_odb_exists(v.ptr, oid.toC())
	runtime.KeepAlive(v)
	runtime.KeepAlive(oid)
	return ret != 0
}

func (v *Odb) Write(data []byte, otype ObjectType) (oid *Oid, err error) {
	oid = new(Oid)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var size C.size_t
	if len(data) > 0 {
		size = C.size_t(len(data))
	} else {
		data = []byte{0}
		size = C.size_t(0)
	}

	ret := C.git_odb_write(oid.outC(), v.ptr, unsafe.Pointer(&data[0]), size, C.git_object_t(otype))
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	return oid, nil
}

func (v *Odb) Read(oid *Oid) (obj *OdbObject, err error) {
	obj = new(OdbObject)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_read(&obj.ptr, v.ptr, oid.toC())
	runtime.KeepAlive(v)
	runtime.KeepAlive(oid)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	runtime.SetFinalizer(obj, (*OdbObject).Free)
	return obj, nil
}

func (odb *Odb) Refresh() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_refresh(odb.ptr)
	runtime.KeepAlive(odb)
	if ret < 0 {
		return MakeGitError(ret)
	}

	return nil
}

func (odb *Odb) WriteMultiPackIndex() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_write_multi_pack_index(odb.ptr)
	runtime.KeepAlive(odb)
	if ret < 0 {
		return MakeGitError(ret)
	}

	return nil
}

type OdbForEachCallback func(id *Oid) error
type odbForEachCallbackData struct {
	callback    OdbForEachCallback
	errorTarget *error
}

//export odbForEachCallback
func odbForEachCallback(id *C.git_oid, handle unsafe.Pointer) (ret C.int) {
	defer recoverCallbackCode(&ret)
	data, ok := pointerHandles.Get(handle).(*odbForEachCallbackData)
	if !ok {
		panic("could not retrieve handle")
	}

	err := data.callback(newOidFromC(id))
	if err != nil {
		*data.errorTarget = err
		return C.int(ErrorCodeUser)
	}

	return C.int(ErrorCodeOK)
}

func (v *Odb) ForEach(callback OdbForEachCallback) error {
	var err error
	data := odbForEachCallbackData{
		callback:    callback,
		errorTarget: &err,
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle := pointerHandles.Track(&data)
	defer pointerHandles.Untrack(handle)

	ret := C._go_git_odb_foreach(v.ptr, handle)
	runtime.KeepAlive(v)
	if ret == C.int(ErrorCodeUser) && err != nil {
		return err
	}
	if ret < 0 {
		return MakeGitError(ret)
	}

	return nil
}

// Hash determines the object-ID of a data buffer.
//
// The object id type follows the object database: an Odb obtained from a
// Repository hashes with that repository's object format (so a SHA256 repository
// yields a SHA256 id), while a standalone Odb from NewOdb uses the libgit2
// default (SHA1). Use HashWithType to pick the type explicitly.
func (v *Odb) Hash(data []byte, otype ObjectType) (oid *Oid, err error) {
	oid = new(Oid)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var size C.size_t
	if len(data) > 0 {
		size = C.size_t(len(data))
	} else {
		data = []byte{0}
		size = C.size_t(0)
	}

	// Route through the typed shim using the object database's configured format.
	ret := C._go_git_odb_hash(oid.outC(), unsafe.Pointer(&data[0]), size, C.git_object_t(otype), v.oidType)
	runtime.KeepAlive(data)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}
	return oid, nil
}

// HashFile determines the object id of the raw contents of a file using this
// object database's configured object id type. It does not apply repository
// filters (for example line-ending conversion).
func (v *Odb) HashFile(path string, otype ObjectType) (*Oid, error) {
	return v.hashFileWithOidType(path, otype, ObjectIdType(v.oidType))
}

func (v *Odb) hashFileWithOidType(path string, otype ObjectType, oidType ObjectIdType) (*Oid, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	if strings.IndexByte(path, 0) >= 0 {
		return nil, &GitError{
			Message: "object file path contains a NUL byte",
			Class:   ErrorClassInvalid,
			Code:    ErrorCodeInvalid,
		}
	}

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	oid := new(Oid)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C._go_git_object_id_from_file(oid.outC(), cpath, C.git_object_t(otype), C.int(oidType))
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}
	return oid, nil
}

// NewReadStream opens a read stream from the ODB. Reading from it will give you the
// contents of the object.
func (v *Odb) NewReadStream(id *Oid) (*OdbReadStream, error) {
	stream := new(OdbReadStream)
	var ctype C.git_object_t
	var csize C.size_t

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_open_rstream(&stream.ptr, &csize, &ctype, v.ptr, id.toC())
	runtime.KeepAlive(v)
	runtime.KeepAlive(id)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	stream.Size = uint64(csize)
	stream.Type = ObjectType(ctype)
	runtime.SetFinalizer(stream, (*OdbReadStream).Free)
	return stream, nil
}

// NewWriteStream opens a write stream to the ODB, which allows you to
// create a new object in the database. The size and type must be
// known in advance
func (v *Odb) NewWriteStream(size int64, otype ObjectType) (*OdbWriteStream, error) {
	stream := new(OdbWriteStream)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_open_wstream(&stream.ptr, v.ptr, C.git_object_size_t(size), C.git_object_t(otype))
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	runtime.SetFinalizer(stream, (*OdbWriteStream).Free)
	return stream, nil
}

// NewWritePack opens a stream for writing a pack file to the ODB. If the ODB
// layer understands pack files, then the given packfile will likely be
// streamed directly to disk (and a corresponding index created). If the ODB
// layer does not understand pack files, the objects will be stored in whatever
// format the ODB layer uses.
func (v *Odb) NewWritePack(callback TransferProgressCallback) (*OdbWritepack, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	writepack := new(OdbWritepack)
	populateRemoteCallbacks(&writepack.ccallbacks, &RemoteCallbacks{TransferProgressCallback: callback}, nil)

	ret := C._go_git_odb_write_pack(&writepack.ptr, v.ptr, writepack.ccallbacks.payload)
	runtime.KeepAlive(v)
	if ret < 0 {
		untrackCallbacksPayload(&writepack.ccallbacks)
		return nil, MakeGitError(ret)
	}

	runtime.SetFinalizer(writepack, (*OdbWritepack).Free)
	return writepack, nil
}

// Free releases the backend. It is safe to call more than once.
//
// Note that a backend added to an Odb is owned by that Odb, so this is only for
// a backend that was never attached.
func (v *OdbBackend) Free() {
	if v == nil || v.ptr == nil {
		return
	}
	ptr := v.ptr
	v.ptr = nil
	runtime.SetFinalizer(v, nil)
	C._go_git_odb_backend_free(ptr)
}

type OdbObject struct {
	doNotCompare
	ptr *C.git_odb_object
}

func (v *OdbObject) Free() {
	if v == nil || v.ptr == nil {
		return
	}
	ptr := v.ptr
	v.ptr = nil
	runtime.SetFinalizer(v, nil)
	C.git_odb_object_free(ptr)
}

func (object *OdbObject) Id() (oid *Oid) {
	ret := newOidFromC(C.git_odb_object_id(object.ptr))
	runtime.KeepAlive(object)
	return ret
}

func (object *OdbObject) Len() (len uint64) {
	ret := uint64(C.git_odb_object_size(object.ptr))
	runtime.KeepAlive(object)
	return ret
}

func (object *OdbObject) Type() ObjectType {
	ret := ObjectType(C.git_odb_object_type(object.ptr))
	runtime.KeepAlive(object)
	return ret
}

// Data returns a slice pointing to the unmanaged object memory. You must make
// sure the object is referenced for at least as long as the slice is used.
func (object *OdbObject) Data() (data []byte) {
	var c_blob unsafe.Pointer = C.git_odb_object_data(object.ptr)
	var blob []byte

	len := int(C.git_odb_object_size(object.ptr))

	sliceHeader := (*reflect.SliceHeader)((unsafe.Pointer(&blob)))
	sliceHeader.Cap = len
	sliceHeader.Len = len
	sliceHeader.Data = uintptr(c_blob)

	return blob
}

type OdbReadStream struct {
	doNotCompare
	ptr  *C.git_odb_stream
	Size uint64
	Type ObjectType
}

// Read reads from the stream
func (stream *OdbReadStream) Read(data []byte) (int, error) {
	header := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	ptr := (*C.char)(unsafe.Pointer(header.Data))
	size := C.size_t(header.Cap)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_stream_read(stream.ptr, ptr, size)
	runtime.KeepAlive(stream)
	if ret < 0 {
		return 0, MakeGitError(ret)
	}
	if ret == 0 {
		return 0, io.EOF
	}

	header.Len = int(ret)

	return len(data), nil
}

// Close is a dummy function in order to implement the Closer and
// ReadCloser interfaces
func (stream *OdbReadStream) Close() error {
	return nil
}

func (stream *OdbReadStream) Free() {
	if stream == nil || stream.ptr == nil {
		return
	}
	ptr := stream.ptr
	stream.ptr = nil
	runtime.SetFinalizer(stream, nil)
	C.git_odb_stream_free(ptr)
}

type OdbWriteStream struct {
	doNotCompare
	ptr *C.git_odb_stream
	Id  Oid
}

// Write writes to the stream
func (stream *OdbWriteStream) Write(data []byte) (int, error) {
	header := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	ptr := (*C.char)(unsafe.Pointer(header.Data))
	size := C.size_t(header.Len)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_stream_write(stream.ptr, ptr, size)
	runtime.KeepAlive(stream)
	if ret < 0 {
		return 0, MakeGitError(ret)
	}

	return len(data), nil
}

// Close signals that all the data has been written and stores the
// resulting object id in the stream's Id field.
func (stream *OdbWriteStream) Close() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_odb_stream_finalize_write(stream.Id.outC(), stream.ptr)
	runtime.KeepAlive(stream)
	if ret < 0 {
		return MakeGitError(ret)
	}

	return nil
}

func (stream *OdbWriteStream) Free() {
	if stream == nil || stream.ptr == nil {
		return
	}
	ptr := stream.ptr
	stream.ptr = nil
	runtime.SetFinalizer(stream, nil)
	C.git_odb_stream_free(ptr)
}

// OdbWritepack is a stream to write a packfile to the ODB.
type OdbWritepack struct {
	doNotCompare
	ptr        *C.git_odb_writepack
	stats      C.git_indexer_progress
	ccallbacks C.git_remote_callbacks
}

func (writepack *OdbWritepack) Write(data []byte) (int, error) {
	header := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	ptr := unsafe.Pointer(header.Data)
	size := C.size_t(header.Len)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C._go_git_odb_writepack_append(writepack.ptr, ptr, size, &writepack.stats)
	runtime.KeepAlive(writepack)
	if ret < 0 {
		return 0, MakeGitError(ret)
	}

	return len(data), nil
}

func (writepack *OdbWritepack) Commit() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C._go_git_odb_writepack_commit(writepack.ptr, &writepack.stats)
	runtime.KeepAlive(writepack)
	if ret < 0 {
		return MakeGitError(ret)
	}

	return nil
}

func (writepack *OdbWritepack) Free() {
	if writepack == nil || writepack.ptr == nil {
		return
	}
	ptr := writepack.ptr
	writepack.ptr = nil
	untrackCallbacksPayload(&writepack.ccallbacks)
	runtime.SetFinalizer(writepack, nil)
	C._go_git_odb_writepack_free(ptr)
}
