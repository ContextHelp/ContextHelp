//go:build !unix

package pipeline

import "io/fs"

// Go synthesises directory mode bits on these platforms (Windows reports
// 0777 or 0555), so neither the mode nor an owner can be checked. The
// protection is the per-user cache directory's ACL.

func ownedByCurrentUser(fs.FileInfo) bool { return true }

func privateToUser(fs.FileInfo) bool { return true }
