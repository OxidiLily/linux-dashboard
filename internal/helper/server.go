// Package helper adalah daemon privileged yang berjalan sebagai root.
// Ia menerima command terstruktur (bukan string shell) lewat Unix domain
// socket, memverifikasi HMAC, mengecek otorisasi, lalu mengeksekusi.
package helper

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

type Server struct {
	socketPath string
	secret     []byte
	ln         net.Listener

	// seenNonce menolak replay dalam jendela waktu yang diterima.
	mu        sync.Mutex
	seenNonce map[string]time.Time
}

// maxClockSkew adalah umur maksimum request yang masih diterima.
const maxClockSkew = 30 * time.Second

func NewServer(socketPath, secretPath, socketGroup string) (*Server, error) {
	secret, err := loadOrCreateSecret(secretPath, socketGroup)
	if err != nil {
		return nil, err
	}
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0o750); err != nil {
		return nil, err
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	// Socket hanya untuk owner + grup web app.
	if err := os.Chmod(socketPath, 0o660); err != nil {
		return nil, err
	}
	if gid, err := lookupGroupID(socketGroup); err == nil {
		if err := os.Chown(socketPath, 0, gid); err != nil {
			log.Printf("peringatan: chown socket ke grup %s gagal: %v", socketGroup, err)
		}
		// Direktori induk WAJIB ikut di-chown. connect() ke unix socket butuh
		// izin telusur (x) pada tiap direktori di jalurnya — socket yang sudah
		// group-writable tetap tertolak EACCES kalau direktorinya root:root 0750.
		// systemd RuntimeDirectory= membuat direktori ini sebagai root:root,
		// jadi tanpa baris berikut web app selalu gagal menghubungi helper.
		if err := os.Chown(socketDir, 0, gid); err != nil {
			log.Printf("peringatan: chown direktori socket ke grup %s gagal: %v", socketGroup, err)
		}
	} else {
		log.Printf("peringatan: grup %s tidak ada, socket tetap root-only", socketGroup)
	}

	s := &Server{
		socketPath: socketPath,
		secret:     secret,
		ln:         ln,
		seenNonce:  map[string]time.Time{},
	}
	go s.gcNonces()
	return s, nil
}

func lookupGroupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(g.Gid)
}

func loadOrCreateSecret(path, group string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(b)) >= 32 {
		return bytes.TrimSpace(b), nil
	}
	if !errors.Is(err, os.ErrNotExist) && err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	hexSecret := []byte(hex.EncodeToString(secret))
	if err := os.WriteFile(path, hexSecret, 0o640); err != nil {
		return nil, err
	}
	if gid, err := lookupGroupID(group); err == nil {
		_ = os.Chown(path, 0, gid)
	}
	log.Printf("secret helper dibuat di %s", path)
	return hexSecret, nil
}

func (s *Server) gcNonces() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		cutoff := time.Now().Add(-2 * maxClockSkew)
		s.mu.Lock()
		for n, ts := range s.seenNonce {
			if ts.Before(cutoff) {
				delete(s.seenNonce, n)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Server) seen(nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seenNonce[nonce]; ok {
		return true
	}
	s.seenNonce[nonce] = time.Now()
	return false
}

func (s *Server) Serve() error {
	log.Printf("helper daemon mendengarkan di %s", s.socketPath)
	// Panaskan status komponen di latar: probe versi paling mahal dibayar
	// sekarang, bukan saat user membuka halaman Components pertama kali.
	go AllComponentStatus()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.socketPath)
	return err
}

func writeResp(conn net.Conn, resp helperproto.Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(b, '\n'))
}

func fail(conn net.Conn, err error) {
	code := helperproto.ErrInternal
	resp := helperproto.Response{Error: err.Error()}
	var he *helperErr
	if errors.As(err, &he) {
		if he.code != "" {
			code = he.code
		}
		// Kode spesifik + params dikirim apa adanya; frontend yang menyusun
		// kalimatnya sesuai bahasa yang dipilih user.
		if he.kodeUI != "" {
			resp.Code = he.kodeUI
			resp.Params = he.params
			writeResp(conn, resp)
			return
		}
	}
	resp.Code = code
	writeResp(conn, resp)
}

func (s *Server) handle(conn net.Conn) {
	closed := false
	defer func() {
		if !closed {
			conn.Close()
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	sig, payload, ok := bytes.Cut(bytes.TrimRight(line, "\n"), []byte(" "))
	if !ok {
		writeResp(conn, helperproto.Response{Code: helperproto.ErrInvalid, Error: "framing request salah"})
		return
	}
	if !helperproto.Verify(s.secret, payload, string(sig)) {
		writeResp(conn, helperproto.Response{Code: helperproto.ErrDenied, Error: "signature tidak valid"})
		return
	}

	var req helperproto.Request
	if err := json.Unmarshal(payload, &req); err != nil {
		writeResp(conn, helperproto.Response{Code: helperproto.ErrInvalid, Error: "request tidak valid"})
		return
	}
	if d := time.Since(time.Unix(req.TS, 0)); d > maxClockSkew || d < -maxClockSkew {
		writeResp(conn, helperproto.Response{Code: helperproto.ErrDenied, Error: "request kedaluwarsa"})
		return
	}
	if s.seen(req.Nonce) {
		writeResp(conn, helperproto.Response{Code: helperproto.ErrDenied, Error: "nonce sudah dipakai"})
		return
	}

	// auth.login adalah satu-satunya command yang identitasnya belum terverifikasi.
	if req.Cmd == helperproto.CmdAuthLogin {
		s.handleLogin(conn, req)
		return
	}

	u, err := lookupUser(req.Username)
	if err != nil {
		fail(conn, errDenied("identitas tidak dikenal"))
		return
	}
	if sudoRequired[req.Cmd] && !u.Sudo {
		fail(conn, errRequiresSudo())
		return
	}

	switch req.Cmd {
	case helperproto.CmdTerminalStart:
		s.handleTerminal(conn, br, u, req)
		closed = true // koneksi diambil alih oleh sesi PTY
		return
	case helperproto.CmdFileRead:
		s.handleFileRead(conn, u, req)
		return
	case helperproto.CmdFileWrite:
		s.handleFileWrite(conn, br, u, req)
		return
	}

	data, err := s.dispatch(u, req)
	if err != nil {
		fail(conn, err)
		return
	}
	writeResp(conn, helperproto.Response{OK: true, Data: data})
}

func decodeArgs[T any](req helperproto.Request) (T, error) {
	var v T
	if len(req.Args) == 0 {
		return v, errInvalid("argumen kosong untuk %s", req.Cmd)
	}
	if err := json.Unmarshal(req.Args, &v); err != nil {
		return v, errInvalid("argumen tidak valid: %v", err)
	}
	return v, nil
}

func (s *Server) dispatch(u *userInfo, req helperproto.Request) (json.RawMessage, error) {
	switch req.Cmd {
	case helperproto.CmdAuthPasswd:
		args, err := decodeArgs[helperproto.PasswdArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, s.changePassword(u, args)

	case helperproto.CmdSysHostnameSet:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, setHostname(args.Path)

	case helperproto.CmdSysDNSSet:
		args, err := decodeArgs[helperproto.DNSArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, setDNS(args.Nameservers)

	case helperproto.CmdProcKill:
		args, err := decodeArgs[helperproto.KillArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, s.killProcess(u, args)

	case helperproto.CmdSvcAction:
		args, err := decodeArgs[helperproto.ServiceArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, serviceAction(args)

	case helperproto.CmdUfwStatus:
		return jsonOf(ufwStatus())

	case helperproto.CmdUfwAdd:
		args, err := decodeArgs[helperproto.UfwRule](req)
		if err != nil {
			return nil, err
		}
		return nil, ufwAdd(args)

	case helperproto.CmdUfwUpdate:
		args, err := decodeArgs[helperproto.UfwUpdateArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, ufwUpdate(args)

	case helperproto.CmdUfwDelete:
		args, err := decodeArgs[helperproto.UfwDeleteArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, ufwDelete(args.Num, args.Spec)

	case helperproto.CmdUfwToggle:
		args, err := decodeArgs[helperproto.UfwToggleArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, ufwToggle(args.Enable)

	case helperproto.CmdFileList, helperproto.CmdFileUsage, helperproto.CmdFileMkdir,
		helperproto.CmdFileRemove, helperproto.CmdFileRename, helperproto.CmdFileCopy,
		helperproto.CmdFileMove, helperproto.CmdFileChmod, helperproto.CmdFileChown:
		return s.fileOp(u, req)

	case helperproto.CmdSambaList:
		return jsonOf(sambaListSemua())
	case helperproto.CmdSambaSave:
		args, err := decodeArgs[helperproto.SambaShare](req)
		if err != nil {
			return nil, err
		}
		return nil, sambaSave(args)
	case helperproto.CmdSambaDelete:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, sambaDelete(args.Path)

	case helperproto.CmdNFSList:
		return jsonOf(nfsList())
	case helperproto.CmdNFSSave:
		args, err := decodeArgs[helperproto.NFSExport](req)
		if err != nil {
			return nil, err
		}
		return nil, nfsSave(args)
	case helperproto.CmdNFSDelete:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, nfsDelete(args.Path)

	case helperproto.CmdDiskUnmount:
		args, err := decodeArgs[helperproto.DiskUnmountArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, diskUnmount(args.Mountpoint, args.Lupakan)

	case helperproto.CmdNFSMountList:
		return jsonOf(nfsMountList())
	case helperproto.CmdNFSMountSave:
		args, err := decodeArgs[helperproto.NFSMount](req)
		if err != nil {
			return nil, err
		}
		return nil, nfsMountSave(args)
	case helperproto.CmdNFSMountDelete:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, nfsMountDelete(args.Path)
	case helperproto.CmdNFSMountToggle:
		args, err := decodeArgs[helperproto.NFSMountToggleArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, nfsMountToggle(args.Mountpoint, args.Lepas)
	case helperproto.CmdNFSMountDiscover:
		args, err := decodeArgs[helperproto.NFSDiscoverArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(nfsDiscover(args.Server))

	case helperproto.CmdPrinterList:
		return jsonOf(printerList())
	case helperproto.CmdPrinterAdd:
		args, err := decodeArgs[helperproto.PrinterAddArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, printerAdd(args)
	case helperproto.CmdPrinterDelete:
		args, err := decodeArgs[helperproto.PrinterNameArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, printerDelete(args.Name)
	case helperproto.CmdPrinterDefault:
		args, err := decodeArgs[helperproto.PrinterNameArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, printerSetDefault(args.Name)
	case helperproto.CmdPrinterEnable:
		args, err := decodeArgs[helperproto.PrinterNameArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, printerEnable(args.Name, args.Enable)
	case helperproto.CmdPrinterDevices:
		return jsonOf(printerDevices())
	case helperproto.CmdPrinterModels:
		return jsonOf(printerModels())
	case helperproto.CmdComponentProgress:
		return jsonOf(ambilProgres(), nil)
	case helperproto.CmdPrinterDeteksi:
		return jsonOf(printerDeteksi())
	case helperproto.CmdPrinterDriverInstall:
		args, err := decodeArgs[helperproto.DriverInstallArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, printerDriverInstall(args.Vendor)
	case helperproto.CmdPrintJobs:
		return jsonOf(printJobs())
	case helperproto.CmdPrintCancel:
		args, err := decodeArgs[helperproto.PrinterNameArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, printCancel(args.Name)
	case helperproto.CmdPrintFile:
		// Satu-satunya perintah printer yang memakai identitas user login:
		// berkasnya diperiksa dan dibaca dengan hak user itu, bukan hak root.
		args, err := decodeArgs[helperproto.PrintFileArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(s.printFile(u, args))

	case helperproto.CmdFail2banList:
		return jsonOf(fail2banList())
	case helperproto.CmdFail2banSave:
		args, err := decodeArgs[helperproto.Fail2banJail](req)
		if err != nil {
			return nil, err
		}
		return nil, fail2banSave(args)
	case helperproto.CmdFail2banDelete:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, fail2banDelete(args.Path)
	case helperproto.CmdFail2banUnban:
		args, err := decodeArgs[helperproto.Fail2banUnbanArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, fail2banUnban(args.Jail, args.IP)

	case helperproto.CmdDiskPrepare:
		args, err := decodeArgs[helperproto.DiskPrepareArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, diskPrepare(args)
	case helperproto.CmdMergerfsList:
		return jsonOf(mergerfsList())
	case helperproto.CmdMergerfsSave:
		args, err := decodeArgs[helperproto.MergerfsPool](req)
		if err != nil {
			return nil, err
		}
		return nil, mergerfsSave(args, u)
	case helperproto.CmdMergerfsDelete:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, mergerfsDelete(args.Path)
	case helperproto.CmdMergerfsMount:
		args, err := decodeArgs[helperproto.MergerfsMountArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, mergerfsMount(args.Mountpoint, args.Lepas)

	case helperproto.CmdSambaUserList:
		return jsonOf(sambaUserList())
	case helperproto.CmdSambaUserSet:
		args, err := decodeArgs[helperproto.SambaUserArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, sambaUserSet(args)
	case helperproto.CmdSambaUserDelete:
		args, err := decodeArgs[helperproto.SambaUserArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, sambaUserDelete(args.Username)

	case helperproto.CmdUserList:
		return jsonOf(listLinuxUsers())
	case helperproto.CmdUserCreate:
		args, err := decodeArgs[helperproto.UserCreateArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, createLinuxUser(args)
	case helperproto.CmdUserModify:
		args, err := decodeArgs[helperproto.UserModifyArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, modifyLinuxUser(args)
	case helperproto.CmdUserDelete:
		args, err := decodeArgs[helperproto.UserDeleteArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, deleteLinuxUser(args)

	case helperproto.CmdComponentStatusAll:
		// Tombol Refresh di halaman Components harus benar-benar memeriksa
		// ulang. Tanpa ini permintaannya dijawab dari cache 30 detik: user
		// menekan Refresh setelah menyalakan service dari terminal, angkanya
		// tidak berubah, dan panel terlihat tidak sinkron dengan mesinnya.
		if args, err := decodeArgs[helperproto.ComponentArgs](req); err == nil && args.Fresh {
			lupakanCacheKomponen()
		}
		return jsonOf(AllComponentStatus(), nil)
	case helperproto.CmdComponentInstall:
		args, err := decodeArgs[helperproto.ComponentArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(installComponent(args.Name, u))
	case helperproto.CmdComponentUninstall:
		args, err := decodeArgs[helperproto.ComponentArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(uninstallComponent(args.Name, args.Purge))
	case helperproto.CmdComponentService:
		args, err := decodeArgs[helperproto.ComponentArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, componentService(args.Name, args.Action, u)

	case helperproto.CmdVPNStatus:
		return jsonOf(vpnStatusAll(), nil)
	case helperproto.CmdVPNConfigure:
		args, err := decodeArgs[helperproto.VPNArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(vpnConfigure(args))

	case helperproto.CmdUninstall:
		args, err := decodeArgs[helperproto.UninstallArgs](req)
		if err != nil {
			return nil, err
		}
		return nil, uninstallJalankan(u, args)

	case helperproto.CmdWGServerInfo:
		return jsonOf(wgServerInfo(), nil)
	case helperproto.CmdWGServerInit:
		args, err := decodeArgs[helperproto.WGServerArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(wgServerInit(args))
	case helperproto.CmdWGPeerAdd:
		args, err := decodeArgs[helperproto.WGPeerArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(wgPeerTambah(args))
	case helperproto.CmdWGPeerDelete:
		args, err := decodeArgs[helperproto.WGPeerArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(wgPeerHapus(args))

	case helperproto.CmdUpdateStatus:
		args, err := decodeArgs[helperproto.UpdateArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(updateStatus(args), nil)
	case helperproto.CmdUpdateStart:
		return jsonOf(updateStart())

	case helperproto.CmdDockerExec:
		args, err := decodeArgs[helperproto.DockerExecArgs](req)
		if err != nil {
			return nil, err
		}
		return jsonOf(dockerExec(args))
	}
	return nil, errInvalid("command tidak dikenal: %s", req.Cmd)
}

func jsonOf[T any](v T, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// run mengeksekusi binary dengan argumen array — tidak pernah lewat `sh -c`,
// sehingga command injection tertutup dari awal.
func run(name string, args ...string) (helperproto.ExecResult, error) {
	return runIn("", nil, name, args...)
}

// runStdin sama dengan run, tapi mengirim isi stdin — dipakai perintah yang
// membaca dari pipe, mis. `wg pubkey` yang menerima private key lewat stdin.
func runStdin(stdin string, name string, args ...string) (helperproto.ExecResult, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LC_ALL=C",
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := helperproto.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return res, &helperErr{code: helperproto.ErrInternal, msg: strings.TrimSpace(firstNonEmpty(stderr.String(), err.Error()))}
	}
	return res, nil
}

func runIn(dir string, extraEnv []string, name string, args ...string) (helperproto.ExecResult, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"DEBIAN_FRONTEND=noninteractive",
		"LC_ALL=C",
	}, extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := helperproto.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, &helperErr{code: helperproto.ErrInternal, msg: strings.TrimSpace(firstNonEmpty(stderr.String(), stdout.String(), ee.Error()))}
	}
	if err != nil {
		return res, &helperErr{code: helperproto.ErrInternal, msg: err.Error()}
	}
	return res, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// loginShell membaca shell user dari /etc/passwd (os/user tidak menyediakannya).
func loginShell(username string) string {
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) >= 7 && parts[0] == username {
			return parts[6]
		}
	}
	return ""
}
