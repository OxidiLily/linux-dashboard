package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

var cloudflareTokenPath = "/etc/linux-dashboard/cloudflare-dns-token"
var cloudflareTokenOwnerUID = 0

func cloudflareTokenFile() (os.FileInfo, error) {
	info, err := os.Lstat(cloudflareTokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSetuid != 0 {
		return nil, errors.New("izin penyimpanan token Cloudflare tidak aman")
	}
	if !cloudflareOwned(info) {
		return nil, errors.New("pemilik token Cloudflare tidak valid")
	}
	return info, nil
}

func cloudflareTokenStatus() (bool, error) {
	info, err := cloudflareTokenFile()
	return info != nil, err
}

func cloudflareTokenRead() (string, error) {
	info, err := cloudflareTokenFile()
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", errors.New("API token Cloudflare belum disimpan")
	}
	// O_NOFOLLOW prevents a symlink swap between Lstat and Open.
	f, err := openCloudflareTokenNoFollow(cloudflareTokenPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]byte, 4097)
	n, err := io.ReadFull(f, b)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	const prefix = "dns_cloudflare_api_token = "
	stored := string(b[:n])
	if !strings.HasPrefix(stored, prefix) || !strings.HasSuffix(stored, "\n") {
		return "", errors.New("format token Cloudflare tidak valid")
	}
	token := strings.TrimSuffix(strings.TrimPrefix(stored, prefix), "\n")
	if err := validateCloudflareToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func validateCloudflareToken(token string) error {
	if strings.TrimSpace(token) == "" || len(token) > 4000 || strings.ContainsAny(token, "\r\n\x00=#[\\] 	") {
		return errors.New("API token Cloudflare tidak valid")
	}
	return nil
}

func cloudflareTokenSave(token string) error {
	if err := validateCloudflareToken(token); err != nil {
		return err
	}
	if _, err := cloudflareTokenFile(); err != nil {
		return err
	}
	dir := filepath.Dir(cloudflareTokenPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	di, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !di.IsDir() || !cloudflareOwned(di) || di.Mode().Perm()&0o022 != 0 {
		return errors.New("direktori token Cloudflare tidak aman")
	}
	f, err := os.CreateTemp(dir, ".cloudflare-dns-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if err := f.Chown(cloudflareTokenOwnerUID, -1); err != nil {
		return err
	}
	if _, err := f.WriteString("dns_cloudflare_api_token = " + token + "\n"); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), cloudflareTokenPath); err != nil {
		return err
	}
	return syncCloudflareTokenDir(dir)
}

func cloudflareTokenDelete() error {
	info, err := cloudflareTokenFile()
	if err != nil || info == nil {
		return err
	}
	if err := os.Remove(cloudflareTokenPath); err != nil {
		return fmt.Errorf("hapus token Cloudflare: %w", err)
	}
	return syncCloudflareTokenDir(filepath.Dir(cloudflareTokenPath))
}

// Errors from Cloudflare (including transport and JSON errors) may echo the
// bearer token; never let them cross the helper socket into the web process.
func cloudflareDNSArgs(args helperproto.CloudflareDNSArgs) (helperproto.CloudflareDNSArgs, error) {
	if args.Token == "" {
		token, err := cloudflareTokenRead()
		if err != nil {
			return args, err
		}
		args.Token = token
	}
	if err := validateCloudflareToken(args.Token); err != nil {
		return args, err
	}
	return args, nil
}

func cloudflareDNSListSafe(args helperproto.CloudflareDNSArgs) ([]helperproto.CloudflareDNSRecord, error) {
	args, err := cloudflareDNSArgs(args)
	if err != nil {
		return nil, err
	}
	out, err := cloudflareDNSList(args)
	if err == nil {
		err = rejectCloudflareTokenEcho(args.Token, out)
	}
	if err != nil {
		return nil, errors.New("permintaan Cloudflare DNS gagal")
	}
	return out, nil
}

func cloudflareDNSUpsertSafe(args helperproto.CloudflareDNSArgs) (helperproto.CloudflareDNSRecord, error) {
	args, err := cloudflareDNSArgs(args)
	if err != nil {
		return helperproto.CloudflareDNSRecord{}, err
	}
	out, err := cloudflareDNSUpsert(args)
	if err == nil {
		err = rejectCloudflareTokenEcho(args.Token, out)
	}
	if err != nil {
		return helperproto.CloudflareDNSRecord{}, errors.New("permintaan Cloudflare DNS gagal")
	}
	return out, nil
}

func cloudflareDNSDeleteSafe(args helperproto.CloudflareDNSArgs) (int, error) {
	args, err := cloudflareDNSArgs(args)
	if err != nil {
		return 0, err
	}
	out, err := cloudflareDNSDelete(args)
	if err != nil {
		return 0, errors.New("permintaan Cloudflare DNS gagal")
	}
	return out, nil
}

func rejectCloudflareTokenEcho(token string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return errors.New("respons Cloudflare tidak valid")
	}
	if strings.Contains(string(b), token) {
		return errors.New("respons Cloudflare tidak aman")
	}
	return nil
}

func cloudflareOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(cloudflareTokenOwnerUID)
}

func openCloudflareTokenNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !cloudflareOwned(info) {
		f.Close()
		return nil, errors.New("izin penyimpanan token Cloudflare tidak aman")
	}
	return f, nil
}

func syncCloudflareTokenDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
