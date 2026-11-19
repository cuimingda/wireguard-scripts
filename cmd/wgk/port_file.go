package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type portConfigSnapshot struct {
	data []byte
	info os.FileInfo
}

func readPortConfig(root *os.Root, name string) (portConfigSnapshot, error) {
	dir, err := root.Open(".")
	if err != nil {
		return portConfigSnapshot{}, err
	}
	defer dir.Close()
	file, err := openPortConfig(root, dir, name)
	if err != nil {
		return portConfigSnapshot{}, err
	}
	defer file.Close()
	return snapshotPortConfig(root, name, file)
}

func commitPortConfig(root *os.Root, name string, original portConfigSnapshot, updated []byte, checkInactive func() error) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	file, err := openPortConfig(root, dir, name)
	if err != nil {
		return err
	}
	defer file.Close()
	// Use the configuration file's lock so random.sh and wgk coordinate updates.
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err != unix.EINTR {
			break
		}
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return fmt.Errorf("another port update holds the lock for %s", file.Name())
	}
	if err != nil {
		return fmt.Errorf("lock %s: %w", file.Name(), err)
	}
	if err := verifyPortConfig(root, name, file, original); err != nil {
		return err
	}

	stagedName := ".wgk-port-" + rand.Text()
	staged, err := root.OpenFile(stagedName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary WireGuard configuration: %w", err)
	}
	defer root.Remove(stagedName)
	defer staged.Close()
	if err := writeStagedPortConfig(staged, original.info, updated); err != nil {
		return fmt.Errorf("stage WireGuard configuration: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close temporary WireGuard configuration: %w", err)
	}
	if err := checkInactive(); err != nil {
		return err
	}
	if err := verifyPortConfig(root, name, file, original); err != nil {
		return err
	}
	if err := verifyPortConfigDirectory(dir); err != nil {
		return err
	}
	if err := root.Rename(stagedName, name); err != nil {
		return fmt.Errorf("replace WireGuard configuration: %w", err)
	}
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("WireGuard configuration was replaced, but syncing its directory failed: %w", err)
	}
	return nil
}

func verifyPortConfigDirectory(dir *os.File) error {
	opened, err := dir.Stat()
	if err != nil {
		return err
	}
	current, err := os.Stat(dir.Name())
	if err != nil {
		return fmt.Errorf("recheck the WireGuard configuration directory: %w", err)
	}
	if !os.SameFile(opened, current) {
		return fmt.Errorf("WireGuard configuration directory %s changed since the preview, run wgk port set again", dir.Name())
	}
	return nil
}

func openPortConfig(root *os.Root, dir *os.File, name string) (*os.File, error) {
	// Openat is used for O_NOFOLLOW because Root.OpenFile resolves confined symlinks.
	if !filepath.IsLocal(name) || name != filepath.Base(name) || name == "." {
		return nil, fmt.Errorf("invalid WireGuard configuration name %q", name)
	}
	path := filepath.Join(root.Name(), name)
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a regular file at %s", path)
	}
	var fd int
	for {
		fd, err = unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !samePortConfigFile(info, opened) {
		file.Close()
		return nil, fmt.Errorf("%s changed while opening it", path)
	}
	return file, nil
}

func snapshotPortConfig(root *os.Root, name string, file *os.File) (portConfigSnapshot, error) {
	info, err := file.Stat()
	if err != nil {
		return portConfigSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return portConfigSnapshot{}, fmt.Errorf("expected a regular file at %s", file.Name())
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return portConfigSnapshot{}, err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return portConfigSnapshot{}, err
	}
	if !samePortConfigFile(info, current) || int64(len(data)) != info.Size() {
		return portConfigSnapshot{}, fmt.Errorf("%s changed while reading it", file.Name())
	}
	return portConfigSnapshot{data: data, info: info}, nil
}

func verifyPortConfig(root *os.Root, name string, file *os.File, original portConfigSnapshot) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	current, err := snapshotPortConfig(root, name, file)
	if err != nil {
		return err
	}
	if !samePortConfigFile(original.info, current.info) || !bytes.Equal(original.data, current.data) {
		return fmt.Errorf("%s changed since the preview, run wgk port set again", file.Name())
	}
	return nil
}

func samePortConfigFile(before, after os.FileInfo) bool {
	beforeStat := before.Sys().(*syscall.Stat_t)
	afterStat := after.Sys().(*syscall.Stat_t)
	return os.SameFile(before, after) && before.Mode() == after.Mode() && before.Size() == after.Size() &&
		before.ModTime().Equal(after.ModTime()) && beforeStat.Uid == afterStat.Uid && beforeStat.Gid == afterStat.Gid
}

func writeStagedPortConfig(file *os.File, original os.FileInfo, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return err
	}
	stat := original.Sys().(*syscall.Stat_t)
	if err := file.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
		return err
	}
	if err := file.Chmod(original.Mode()); err != nil {
		return err
	}
	return file.Sync()
}
