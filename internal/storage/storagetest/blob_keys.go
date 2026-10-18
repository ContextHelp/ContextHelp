package storagetest

import "github.com/ideacrafterslabs/ctxt/internal/storage/blob/blobtest"

// bk is the blob key a fixture names name: keys are content hashes
// (storage.CheckBlobKey), so a fixture stores its blob under name's.
func bk(name string) string { return blobtest.KeyOf(name) }
