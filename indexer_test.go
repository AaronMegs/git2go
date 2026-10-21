package git

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"testing"
)

var (
	// This is a packfile with three objects. The second is a delta which
	// depends on the third, which is also a delta.
	outOfOrderPack = []byte{
		0x50, 0x41, 0x43, 0x4b, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x03,
		0x32, 0x78, 0x9c, 0x63, 0x67, 0x00, 0x00, 0x00, 0x10, 0x00, 0x08, 0x76,
		0xe6, 0x8f, 0xe8, 0x12, 0x9b, 0x54, 0x6b, 0x10, 0x1a, 0xee, 0x95, 0x10,
		0xc5, 0x32, 0x8e, 0x7f, 0x21, 0xca, 0x1d, 0x18, 0x78, 0x9c, 0x63, 0x62,
		0x66, 0x4e, 0xcb, 0xcf, 0x07, 0x00, 0x02, 0xac, 0x01, 0x4d, 0x75, 0x01,
		0xd7, 0x71, 0x36, 0x66, 0xf4, 0xde, 0x82, 0x27, 0x76, 0xc7, 0x62, 0x2c,
		0x10, 0xf1, 0xb0, 0x7d, 0xe2, 0x80, 0xdc, 0x78, 0x9c, 0x63, 0x62, 0x62,
		0x62, 0xb7, 0x03, 0x00, 0x00, 0x69, 0x00, 0x4c, 0xde, 0x7d, 0xaa, 0xe4,
		0x19, 0x87, 0x58, 0x80, 0x61, 0x09, 0x9a, 0x33, 0xca, 0x7a, 0x31, 0x92,
		0x6f, 0xae, 0x66, 0x75,
	}
)

func TestIndexerThinPackRetainsOdb(t *testing.T) {
	for _, oidType := range []ObjectIdType{ObjectIdSHA1, ObjectIdSHA256} {
		t.Run(fmt.Sprint(oidType), func(t *testing.T) {
			odb, err := NewOdbWithOidType(oidType)
			checkFatal(t, err)
			defer func() { odb.Free() }()
			backend, err := NewOdbBackendLooseWithOidType(t.TempDir(), -1, false, 0755, 0644, oidType)
			checkFatal(t, err)
			if err := odb.AddBackend(backend, 1); err != nil {
				backend.Free()
				t.Fatal(err)
			}
			base, err := odb.Write([]byte("ab"), ObjectBlob)
			checkFatal(t, err)
			var pack bytes.Buffer
			pack.Write([]byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 1, 0x76})
			pack.Write(base.Bytes())
			compressed := zlib.NewWriter(&pack)
			_, err = compressed.Write([]byte{2, 3, 3, 'f', 'o', 'o'})
			checkFatal(t, err)
			checkFatal(t, compressed.Close())
			if oidType == ObjectIdSHA1 {
				sum := sha1.Sum(pack.Bytes())
				pack.Write(sum[:])
			} else {
				sum := sha256.Sum256(pack.Bytes())
				pack.Write(sum[:])
			}

			dir := t.TempDir()
			idx, err := NewIndexerForOidType(dir, odb, oidType, nil)
			checkFatal(t, err)
			defer idx.Free()
			if idx.odb != odb {
				t.Fatal("indexer does not retain its borrowed ODB")
			}
			odb = nil
			runtime.GC()
			_, err = idx.Write(pack.Bytes())
			checkFatal(t, err)
			packID, err := idx.Commit()
			checkFatal(t, err)
			if idx.stats.local_objects != 1 {
				t.Fatalf("local base objects = %d, want 1", idx.stats.local_objects)
			}
			odb = idx.odb
			idx.Free()
			if idx.odb != nil {
				t.Fatal("indexer retains ODB after Free")
			}

			packed, err := NewOdbWithOidType(oidType)
			checkFatal(t, err)
			defer packed.Free()
			packBackend, err := NewOdbBackendOnePackWithOidType(path.Join(dir, "pack-"+packID.String()+".idx"), oidType)
			checkFatal(t, err)
			if err := packed.AddBackend(packBackend, 1); err != nil {
				packBackend.Free()
				t.Fatal(err)
			}
			id, err := packed.HashWithType([]byte("foo"), ObjectBlob, oidType)
			checkFatal(t, err)
			obj, err := packed.Read(id)
			checkFatal(t, err)
			defer obj.Free()
			if string(obj.Data()) != "foo" {
				t.Fatal("thin pack delta did not resolve")
			}
		})
	}
}

func TestIndexerOutOfOrder(t *testing.T) {
	t.Parallel()

	tmpPath, err := ioutil.TempDir("", "git2go")
	checkFatal(t, err)
	defer os.RemoveAll(tmpPath)

	var finalStats TransferProgress
	idx, err := NewIndexer(tmpPath, nil, func(stats TransferProgress) error {
		finalStats = stats
		return nil
	})
	checkFatal(t, err)
	defer idx.Free()

	_, err = idx.Write(outOfOrderPack)
	checkFatal(t, err)
	oid, err := idx.Commit()
	checkFatal(t, err)

	// The packfile contains the hash as the last 20 bytes.
	expectedOid := NewOidFromBytes(outOfOrderPack[len(outOfOrderPack)-20:])
	if !expectedOid.Equal(oid) {
		t.Errorf("mismatched packfile hash, expected %v, got %v", expectedOid, oid)
	}
	if finalStats.TotalObjects != 3 {
		t.Errorf("mismatched transferred objects, expected 3, got %v", finalStats.TotalObjects)
	}
	if finalStats.ReceivedObjects != 3 {
		t.Errorf("mismatched received objects, expected 3, got %v", finalStats.ReceivedObjects)
	}
	if finalStats.IndexedObjects != 3 {
		t.Errorf("mismatched indexed objects, expected 3, got %v", finalStats.IndexedObjects)
	}

	odb, err := NewOdb()
	checkFatal(t, err)
	defer odb.Free()

	backend, err := NewOdbBackendOnePack(path.Join(tmpPath, fmt.Sprintf("pack-%s.idx", oid.String())))
	checkFatal(t, err)
	// Transfer the ownership of the backend to the odb, no freeing needed.
	err = odb.AddBackend(backend, 1)
	checkFatal(t, err)

	packfileObjects := 0
	err = odb.ForEach(func(id *Oid) error {
		packfileObjects += 1
		return nil
	})
	checkFatal(t, err)
	if packfileObjects != 3 {
		t.Errorf("mismatched packfile objects, expected 3, got %v", packfileObjects)
	}

	// Inspect one of the well-known objects in the packfile.
	obj, err := odb.Read(NewOidFromBytes([]byte{
		0x19, 0x10, 0x28, 0x15, 0x66, 0x3d, 0x23, 0xf8, 0xb7, 0x5a, 0x47, 0xe7,
		0xa0, 0x19, 0x65, 0xdc, 0xdc, 0x96, 0x46, 0x8c,
	}))
	checkFatal(t, err)
	defer obj.Free()
	if "foo" != string(obj.Data()) {
		t.Errorf("mismatched packfile object contents, expected foo, got %q", string(obj.Data()))
	}
}
