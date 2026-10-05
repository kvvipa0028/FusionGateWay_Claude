//go:build darwin

package codex

import (
	"crypto/sha256"
	"crypto/x509"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Use the same explicit, root-owned public PEM as private official login.
// No SSL_CERT_FILE/SSL_CERT_DIR, user Keychain or caller trust override.
func readSubscriptionCA() (subscriptionCA, error) {
	const path = "/private/etc/ssl/cert.pem"
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	f := os.NewFile(uintptr(fd), "public-system-ca")
	defer f.Close()
	valid := func(st unix.Stat_t) bool {
		return uint32(st.Mode)&unix.S_IFMT == unix.S_IFREG && st.Uid == 0 && uint32(st.Mode)&0022 == 0 && st.Nlink == 1 && st.Size > 0 && st.Size <= 1<<20
	}
	var before, after, current unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !valid(before) {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	info, e := f.Stat()
	if e != nil {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 || unix.Fstat(fd, &after) != nil || !valid(after) || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size || after.Size != int64(len(raw)) || unix.Lstat(path, &current) != nil || !valid(current) || current.Dev != after.Dev || current.Ino != after.Ino {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	last, e := f.Stat()
	if e != nil || !info.ModTime().Equal(last.ModTime()) {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return subscriptionCA{}, ErrSubscriptionTransport
	}
	return subscriptionCA{pool: pool, file: privateFileID{uint64(after.Dev), uint64(after.Ino)}, digest: sha256.Sum256(raw)}, nil
}
