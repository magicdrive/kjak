package external

import (
	"os"
	"time"
)

// identity is what tells one file from another: the file itself (device
// and inode, or volume and file index on Windows), its size and its
// modification time.
type identity struct {
	fi    os.FileInfo
	size  int64
	mtime time.Time
}

func identityOf(st os.FileInfo) identity {
	return identity{fi: st, size: st.Size(), mtime: st.ModTime()}
}

func (a identity) same(b identity) bool {
	return a.fi != nil && b.fi != nil && os.SameFile(a.fi, b.fi)
}
