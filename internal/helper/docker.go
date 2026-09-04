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
	"rm": true,
	// volume/network/image/system/builder dipakai halaman Docker untuk
	// mengelola sumber daya selain container. Semuanya punya sub-subcommand
	// sendiri yang dicek terpisah di checkDayaArgs — `docker volume` saja
	// tidak berarti apa-apa.
	"volume":  true,
	"network": true,
	"image":   true,
	"system":  true,
	"builder": true,
	"compose": true,
}

// Sub-subcommand yang boleh per sumber daya. Whitelist dengan alasan yang sama
// seperti daftar di atas: `docker image save` dan `docker image load` misalnya
// bisa membaca dan menulis berkas sembarang di host lewat argumennya.
//
// Dipisah per sumber daya, bukan satu daftar bersama, karena dua penghuni
// terakhir memang tidak boleh menerima apa pun selain satu perintah:
//
//   - `system` HANYA df, yaitu laporan pemakaian disk. `system prune` sengaja
//     tidak ada: ia membuang container berhenti, network, cache build, dan
//     — dengan --volumes — seluruh volume yang tidak terpakai dalam SATU
//     perintah. Cakupan sebesar itu tidak bisa dijelaskan dengan jujur di satu
//     dialog konfirmasi, dan tombol per sumber daya sudah menutupi semuanya
//     dengan kalimat yang benar untuk masing-masing.
//
//   - `builder` HANYA prune. Cache build adalah satu-satunya sumber daya
//     docker yang isinya murni hasil turunan: menghapusnya tidak pernah
//     menghilangkan data, paling mahal membuat build berikutnya mulai dari nol.
var allowedDayaSub = map[string]map[string]bool{
	"volume":  {"ls": true, "inspect": true, "rm": true, "prune": true},
	"network": {"ls": true, "inspect": true, "rm": true, "prune": true},
	"image":   {"ls": true, "inspect": true, "rm": true, "prune": true},
	"system":  {"df": true},
	"builder": {"prune": true},
}

// Flag yang boleh menyertai sub-subcommand di atas. Semuanya disusun API
// sendiri, tidak satu pun berasal dari input user — daftar ini adalah jaring
// pengaman kalau suatu saat ada yang lupa.
var flagDayaAman = map[string]bool{
	"-f": true, "--force": true, "--format": true, "--filter": true,
	"-a": true, "--all": true, "-q": true, "--quiet": true,
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
	switch sub {
	case "compose":
		if err := checkComposeArgs(args.Args[1:]); err != nil {
			return helperproto.ExecResult{}, err
		}
	case "volume", "network", "image", "system", "builder":
		if err := checkDayaArgs(sub, args.Args[1:]); err != nil {
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

// checkDayaArgs memvalidasi `docker <volume|network|image|system|builder> <sub> ...`.
//
// Pemeriksaan kedua — nama/ID tidak boleh diawali "-" — bukan formalitas:
// nama volume bernama `--help` akan dibaca docker sebagai flag, sehingga
// `docker volume rm --help` keluar dengan status 0 tanpa menghapus apa pun
// dan panel melaporkan penghapusan yang tidak pernah terjadi. Yang lebih
// buruk, `--filter` atau `-a` yang menyelinap ke `prune` mengubah cakupan
// penghapusan jauh melampaui yang dikonfirmasi user.
func checkDayaArgs(daya string, rest []string) error {
	if len(rest) == 0 {
		return errInvalid("subcommand %s tidak ditemukan", daya)
	}
	if !allowedDayaSub[daya][rest[0]] {
		return errInvalid("subcommand %s %q tidak diizinkan", daya, rest[0])
	}
	for _, a := range rest[1:] {
		if strings.HasPrefix(a, "-") && !flagDayaAman[a] {
			return errInvalid("opsi %q tidak diizinkan untuk docker %s", a, daya)
		}
	}
	return nil
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
