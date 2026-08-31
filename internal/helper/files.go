package helper

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/user"
	"strconv"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// fileOp memetakan command file ke op worker yang berjalan sebagai user login.
func (s *Server) fileOp(u *userInfo, req helperproto.Request) (json.RawMessage, error) {
	switch req.Cmd {
	case helperproto.CmdFileList, helperproto.CmdFileRemove:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		path, err := s.checkPath(u, args.Path)
		if err != nil {
			return nil, err
		}
		op := workerOp{Path: path}
		if req.Cmd == helperproto.CmdFileList {
			op.Op = "list"
			// Sudoer melihat daftar apa adanya (itu memang tugasnya sebagai
			// admin); user biasa hanya melihat yang benar-benar bisa dibuka.
			op.SaringAkses = !u.Sudo
		} else {
			op.Op = "remove"
			op.Recursive = true
		}
		return runAsUser(u, op, nil, nil)

	case helperproto.CmdFileUsage:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		path, err := s.checkPath(u, args.Path)
		if err != nil {
			return nil, err
		}
		return runAsUser(u, workerOp{Op: "usage", Path: path}, nil, nil)

	case helperproto.CmdFileMkdir:
		args, err := decodeArgs[helperproto.PathArgs](req)
		if err != nil {
			return nil, err
		}
		path, err := s.checkPath(u, args.Path)
		if err != nil {
			return nil, err
		}
		return runAsUser(u, workerOp{Op: "mkdir", Path: path}, nil, nil)

	case helperproto.CmdFileRename, helperproto.CmdFileCopy, helperproto.CmdFileMove:
		args, err := decodeArgs[helperproto.TwoPathArgs](req)
		if err != nil {
			return nil, err
		}
		src, err := s.checkPath(u, args.Source)
		if err != nil {
			return nil, err
		}
		dst, err := s.checkPath(u, args.Dest)
		if err != nil {
			return nil, err
		}
		op := "rename"
		switch req.Cmd {
		case helperproto.CmdFileCopy:
			op = "copy"
		case helperproto.CmdFileMove:
			op = "move"
		}
		return runAsUser(u, workerOp{Op: op, Path: src, Dest: dst}, nil, nil)

	case helperproto.CmdFileChmod:
		args, err := decodeArgs[helperproto.ChmodArgs](req)
		if err != nil {
			return nil, err
		}
		path, err := s.checkPath(u, args.Path)
		if err != nil {
			return nil, err
		}
		if args.Mode > 0o7777 {
			return nil, errInvalid("mode tidak valid")
		}
		return runAsUser(u, workerOp{Op: "chmod", Path: path, Mode: args.Mode}, nil, nil)

	case helperproto.CmdFileChown:
		// chown ke owner sembarang memang hanya bisa root — dieksekusi langsung
		// oleh daemon, sudah digate `sudoRequired`.
		args, err := decodeArgs[helperproto.ChownArgs](req)
		if err != nil {
			return nil, err
		}
		path, err := s.checkPath(u, args.Path)
		if err != nil {
			return nil, err
		}
		return nil, chownPath(path, args.Owner, args.Group, args.Recursive)
	}
	return nil, errInvalid("command file tidak dikenal: %s", req.Cmd)
}

func chownPath(path, owner, group string, recursive bool) error {
	uid, gid := -1, -1
	if owner != "" {
		u, err := user.Lookup(owner)
		if err != nil {
			return errInvalid("user %q tidak ditemukan", owner)
		}
		uid, _ = strconv.Atoi(u.Uid)
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return errInvalid("grup %q tidak ditemukan", group)
		}
		gid, _ = strconv.Atoi(g.Gid)
	}
	if !recursive {
		return os.Lchown(path, uid, gid)
	}
	return filepathWalkChown(path, uid, gid)
}

func filepathWalkChown(root string, uid, gid int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return os.Lchown(root, uid, gid)
	}
	if err := os.Lchown(root, uid, gid); err != nil {
		return err
	}
	for _, e := range entries {
		if err := filepathWalkChown(root+"/"+e.Name(), uid, gid); err != nil {
			return err
		}
	}
	return nil
}

// handleFileRead menstream isi file: response OK dulu (berisi ukuran), lalu
// byte mentah mengalir ke koneksi sampai EOF.
func (s *Server) handleFileRead(conn net.Conn, u *userInfo, req helperproto.Request) {
	defer conn.Close()
	args, err := decodeArgs[helperproto.PathArgs](req)
	if err != nil {
		fail(conn, err)
		return
	}
	path, err := s.checkPath(u, args.Path)
	if err != nil {
		fail(conn, err)
		return
	}
	// Worker mengirim result awal ke pipe; runAsUser mengembalikannya setelah
	// seluruh data selesai disalin, jadi header ditulis oleh worker ke stdout
	// bukan lewat response. Untuk itu response OK dikirim lebih dulu di sini
	// setelah stat berhasil.
	statData, err := runAsUser(u, workerOp{Op: "stat", Path: path, SaringAkses: true}, nil, nil)
	if err != nil {
		fail(conn, err)
		return
	}
	var entry helperproto.FileEntry
	_ = json.Unmarshal(statData, &entry)
	if entry.IsDir {
		fail(conn, errInvalid("%s adalah direktori", path))
		return
	}
	meta, _ := json.Marshal(entry)
	writeResp(conn, helperproto.Response{OK: true, Data: meta})

	if _, err := runAsUser(u, workerOp{Op: "read", Path: path}, nil, conn); err != nil {
		// Response sudah terkirim; satu-satunya sinyal error adalah koneksi
		// ditutup lebih awal.
		return
	}
}

// handleFileWrite menerima byte mentah dari koneksi dan menulisnya ke file
// sebagai user login — streaming, tanpa buffer penuh di RAM (upload unlimited).
func (s *Server) handleFileWrite(conn net.Conn, br *bufio.Reader, u *userInfo, req helperproto.Request) {
	defer conn.Close()
	args, err := decodeArgs[helperproto.WriteArgs](req)
	if err != nil {
		fail(conn, err)
		return
	}
	path, err := s.checkPath(u, args.Path)
	if err != nil {
		fail(conn, err)
		return
	}
	// Response pertama hanya menandakan stream siap menerima data — file belum
	// dibuka, jadi ia BUKAN tanda penulisan berhasil.
	writeResp(conn, helperproto.Response{OK: true})
	if _, err := runAsUser(u, workerOp{Op: "write", Path: path, Append: args.Append}, br, nil); err != nil {
		// Response kedua membawa hasil sebenarnya. Tanpa ini kegagalan menulis
		// hilang tanpa jejak dan pemanggil melaporkan upload sukses untuk berkas
		// yang tidak pernah tertulis.
		fail(conn, err)
		return
	}
	writeResp(conn, helperproto.Response{OK: true})
}

func (s *Server) killProcess(u *userInfo, args helperproto.KillArgs) error {
	if args.PID <= 1 {
		return errInvalid("PID tidak valid")
	}
	sig := args.Signal
	if sig == 0 {
		sig = 15 // SIGTERM
	}
	if sig != 9 && sig != 15 && sig != 1 && sig != 2 {
		return errInvalid("signal tidak diizinkan")
	}
	owner, err := processOwner(args.PID)
	if err != nil {
		return &helperErr{code: helperproto.ErrNotFound, msg: "proses tidak ditemukan"}
	}
	if owner == u.UID {
		// Proses milik sendiri: tidak butuh sudo. Worker berjalan sebagai user
		// tersebut, jadi kernel yang menegakkan izinnya.
		_, err := runAsUser(u, workerOp{Op: "kill", PID: args.PID, Signal: sig}, nil, nil)
		return err
	}
	if !u.Sudo {
		return errRequiresSudo()
	}
	return killAsRoot(args.PID, sig)
}
