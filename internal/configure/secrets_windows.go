//go:build windows

package configure

import (
	"fmt"
	"io/fs"
	"os"
)

// openSecrets opens a secrets file for reading. With noFollow, a symbolic
// link or another reparse point at path is refused. Windows has no
// O_NOFOLLOW: the check is made on the path before the open (a link
// created in between is not seen; docs/security.md).
func openSecrets(path string, noFollow bool) (*os.File, error) {
	if noFollow {
		st, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return nil, fmt.Errorf("%w: it is a symbolic link or a reparse point, which is refused here", ErrInsecureSecrets)
		}
	}
	return os.Open(path)
}

// checkSecretsInfo checks the opened file: a regular file. Its access
// control list is not checked: kjak v0.x does not read Windows ACLs
// (docs/security.md); a file under %AppData% is private to the user by the
// default ACL of the profile.
func checkSecretsInfo(_ string, st os.FileInfo, _ bool) error {
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%w: it is not a regular file", ErrInsecureSecrets)
	}
	return nil
}

// SecretsChecksPermissions: on Windows the ACL of a secrets file is not
// checked.
const SecretsChecksPermissions = false
