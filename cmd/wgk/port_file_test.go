package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCommitPortConfig(t *testing.T) {
	root := openPortConfigTestRoot(t)
	original, err := readPortConfig(root, "wg0.conf")
	if err != nil {
		t.Fatal(err)
	}
	updated := []byte("[Interface]\nListenPort = 45000\n")
	if err := commitPortConfig(root, "wg0.conf", original, updated, func() error {
		checkPortConfigContents(t, root, string(original.data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	checkPortConfigContents(t, root, string(updated))
	info, err := root.Stat("wg0.conf")
	if err != nil {
		t.Fatal(err)
	}
	before := original.info.Sys().(*syscall.Stat_t)
	after := info.Sys().(*syscall.Stat_t)
	if os.SameFile(original.info, info) || info.Mode() != original.info.Mode() || before.Uid != after.Uid || before.Gid != after.Gid {
		t.Fatalf("atomic replacement did not preserve the owner and mode: %v to %v", original.info, info)
	}
}

func TestCommitPortConfigRechecksBeforeReplacement(t *testing.T) {
	t.Run("service changes", func(t *testing.T) {
		root := openPortConfigTestRoot(t)
		original, err := readPortConfig(root, "wg0.conf")
		if err != nil {
			t.Fatal(err)
		}
		active := errors.New("service became active")
		err = commitPortConfig(root, "wg0.conf", original, []byte("previewed"), func() error { return active })
		if !errors.Is(err, active) {
			t.Fatalf("commit error = %v, want %v", err, active)
		}
		checkPortConfigContents(t, root, string(original.data))
	})
	t.Run("config changes", func(t *testing.T) {
		root := openPortConfigTestRoot(t)
		original, err := readPortConfig(root, "wg0.conf")
		if err != nil {
			t.Fatal(err)
		}
		const changed = "[Interface]\nListenPort = 49000\n"
		err = commitPortConfig(root, "wg0.conf", original, []byte("previewed"), func() error {
			return root.WriteFile("wg0.conf", []byte(changed), 0o640)
		})
		if err == nil || !strings.Contains(err.Error(), "changed since the preview") {
			t.Fatalf("commit error = %v, want stale preview diagnostic", err)
		}
		checkPortConfigContents(t, root, changed)
	})
	t.Run("config directory changes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "wireguard")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		root := openPortConfigTestRootAt(t, path)
		original, err := readPortConfig(root, "wg0.conf")
		if err != nil {
			t.Fatal(err)
		}
		err = commitPortConfig(root, "wg0.conf", original, []byte("previewed"), func() error {
			if err := os.Rename(path, path+"-old"); err != nil {
				return err
			}
			return os.Mkdir(path, 0o700)
		})
		if err == nil || !strings.Contains(err.Error(), "changed since the preview") {
			t.Fatalf("commit error = %v, want stale directory diagnostic", err)
		}
		checkPortConfigContents(t, root, string(original.data))
	})
}

func TestCommitPortConfigUsesExistingUpdaterLock(t *testing.T) {
	root := openPortConfigTestRoot(t)
	original, err := readPortConfig(root, "wg0.conf")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := root.Open("wg0.conf")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	err = commitPortConfig(root, "wg0.conf", original, []byte("previewed"), func() error {
		t.Fatal("commit proceeded while the updater lock was held")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "holds the lock") {
		t.Fatalf("commit error = %v, want lock diagnostic", err)
	}
	checkPortConfigContents(t, root, string(original.data))
}

func TestReadPortConfigRejectsSymlink(t *testing.T) {
	root := openPortConfigTestRoot(t)
	if err := root.Rename("wg0.conf", "target.conf"); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("target.conf", "wg0.conf"); err != nil {
		t.Fatal(err)
	}
	if _, err := readPortConfig(root, "wg0.conf"); err == nil || !strings.Contains(err.Error(), "expected a regular file") {
		t.Fatalf("read error = %v, want regular file diagnostic", err)
	}
}

func openPortConfigTestRoot(t *testing.T) *os.Root {
	t.Helper()
	return openPortConfigTestRootAt(t, t.TempDir())
}

func openPortConfigTestRootAt(t *testing.T, path string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	if err := root.WriteFile("wg0.conf", []byte("[Interface]\nListenPort = 51820\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return root
}

func checkPortConfigContents(t *testing.T, root *os.Root, want string) {
	t.Helper()
	data, err := root.ReadFile("wg0.conf")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("configuration = %q, want %q", data, want)
	}
}
