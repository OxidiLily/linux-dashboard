package helper

import (
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Subcommand docker yang boleh dipanggil dari panel. Whitelist, bukan
// blacklist — `docker run` sengaja tidak ada karena setara akses root penuh
// lewat bind-mount ke filesystem host.
var allowedDockerSub = map[string]bool{
	"ps": true, "images": true, "inspect": true, "logs": true, "stats": true,
	"start": true, "stop": true, "restart": true, "version": true, "info": true,
	// `rm` dipakai tombol Hapus container di UI; `-f` ditambahkan API, bukan user.
	"rm":      true,
	"compose": true,
}

var allowedComposeSub = map[string]bool{
	"up": true, "down": true, "restart": true, "ps": true, "logs": true,
	"pull": true, "stop": true, "start": true, "config": true,
	// `ls` dipakai untuk menemukan stack yang sudah jalan tapi belum terdaftar.
	// Tanpa ini helper menolaknya dan daftar stack luar diam-diam kosong.
	"ls": true,
}

func dockerExec(args helperproto.DockerExecArgs) (helperproto.ExecResult, error) {
	if len(args.Args) == 0 {
		return helperproto.ExecResult{}, errInvalid("argumen docker kosong")
	}
	sub := args.Args[0]
	if !allowedDockerSub[sub] {
		return helperproto.ExecResult{}, errInvalid("subcommand docker %q tidak diizinkan", sub)
	}
	if sub == "compose" {
		if err := checkComposeArgs(args.Args[1:]); err != nil {
			return helperproto.ExecResult{}, err
		}
	}
	if args.Dir != "" && !strings.HasPrefix(args.Dir, "/") {
		return helperproto.ExecResult{}, errInvalid("dir harus absolut")
	}
	// Docker belum terpasang dijawab dengan kode yang bisa dikenali UI,
	// bukan dibiarkan jatuh ke exec dan mengembalikan pesan Go mentah
	// `exec: "docker": executable file not found in $PATH` — kalimat yang
	// menyebut $PATH kepada user yang cuma membuka sebuah halaman panel.
	if _, ada := lookBinary("docker"); !ada {
		return helperproto.ExecResult{}, errKode(helperproto.ErrBelumTerpasang,
			"Docker belum terpasang — pasang dulu lewat Settings → Components")
	}
	return runIn(args.Dir, nil, "docker", args.Args...)
}

func checkComposeArgs(rest []string) error {
	// Bentuk yang dipakai panel: compose -f <path> [--env-file <path>] <sub> ...
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "-f" || a == "--file" || a == "--env-file" || a == "-p" || a == "--project-name":
			if i+1 >= len(rest) {
				return errInvalid("opsi %s butuh nilai", a)
			}
			i++
		case strings.HasPrefix(a, "-"):
			// Flag lain (mis. -d, --remove-orphans) tidak membawa path, aman dilewati.
		default:
			if !allowedComposeSub[a] {
				return errInvalid("subcommand compose %q tidak diizinkan", a)
			}
			return nil
		}
	}
	return errInvalid("subcommand compose tidak ditemukan")
}
