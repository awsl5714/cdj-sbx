//go:build unix

package apply

import (
	"os"
	"syscall"
)

// preserveOwnerMode copies ref's mode and (uid, gid) onto tmp, so that an
// atomic replace does not drop a service user's access — e.g. a config that is
// 0640 root:sing-box would otherwise become root-owned and unreadable by the
// sing-box service after rename.
func preserveOwnerMode(tmp, ref string) {
	fi, err := os.Stat(ref)
	if err != nil {
		return
	}
	_ = os.Chmod(tmp, fi.Mode())
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(tmp, int(st.Uid), int(st.Gid))
	}
}
