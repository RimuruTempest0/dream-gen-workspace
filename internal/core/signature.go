package core

import (
	"os"
	"path/filepath"
)

// expectSigned is set to "1" by scripts/build.ps1 when it signs the program
// (-ldflags -X …expectSigned=1). Such a build checks its own Authenticode
// signature before trusting customer.dat: a program changed after signing (for
// example to swap the trusted public key) has a broken signature and refuses the
// licence. Unsigned builds skip the check.
var expectSigned = ""

// programSignatureProblem returns why this signed build must not trust
// customer.dat, or "".
func programSignatureProblem() string {
	if expectSigned != "1" {
		return ""
	}
	exe, e := os.Executable()
	if e != nil {
		return "无法确认程序文件"
	}
	if exe, e = filepath.EvalSymlinks(exe); e == nil && authenticodeValid(exe) {
		return ""
	}
	return "程序文件的数字签名无效（可能被修改或损坏），已停止使用软件授权。请重新完整解压软件提供方发来的软件包。"
}
