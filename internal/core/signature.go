package core

import (
	"os"
	"path/filepath"

	"imageworkbench/internal/i18n"
)

// expectSigned is set to "1" by scripts/build.ps1 when it signs the program
// (-ldflags -X …expectSigned=1). Such a build checks its own Authenticode
// signature before trusting customer.dat: a program changed after signing (for
// example to swap the trusted public key) has a broken signature and refuses the
// licence. Unsigned builds skip the check.
var expectSigned = ""

// programSignatureProblem returns why this signed build must not trust
// customer.dat, or no message.
func programSignatureProblem() i18n.Message {
	if expectSigned != "1" {
		return i18n.Message{}
	}
	exe, e := os.Executable()
	if e != nil {
		return i18n.Msg("license.program.unknown")
	}
	if exe, e = filepath.EvalSymlinks(exe); e == nil && authenticodeValid(exe) {
		return i18n.Message{}
	}
	return i18n.Msg("license.program.signature")
}
