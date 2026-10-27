package configure

import (
	"errors"
	"fmt"
	"strings"
)

// ExposedLiterals says who else can read a configuration file that keeps
// literal secrets: "" when the file is private (the checks of a secrets
// file: owned by the user, no permission for group or others, no ACL entry
// for anyone else; on Windows the owner and the DACL), else why it is not
// ("group or others have access (mode 0644); run: chmod 600 PATH", an
// ACL entry, another owner). It never reads or shows a secret. A file it
// cannot check is reported as such: that is not proof it is private.
//
// It is only a warning: the trust model does not change (a literal is
// deprecated and works; `config validate --strict` refuses every literal,
// exposed or not).
func ExposedLiterals(src *Source) string {
	if src == nil || src.File == nil || !src.Exists || len(Literals(src.File)) == 0 {
		return ""
	}
	return exposure(src.Path)
}

func exposure(path string) string {
	f, err := openSecrets(path, false)
	if err != nil {
		return fmt.Sprintf("its permissions cannot be checked: %v", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return fmt.Sprintf("its permissions cannot be checked: %v", err)
	}
	err = checkPrivateFile(f, path, st, false)
	if err == nil {
		return ""
	}
	// the reason without "the secrets file is not private: "
	msg := err.Error()
	if errors.Is(err, ErrInsecureSecrets) {
		msg = strings.TrimPrefix(msg, ErrInsecureSecrets.Error()+": ")
	}
	return msg
}
