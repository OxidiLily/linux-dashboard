#!/bin/sh
# Supervisor sesi AI Agent — dijalankan halaman AI → AI Agent, bukan Terminal.
#
# Tanpa ini, Ctrl+C yang membuat agent keluar meninggalkan user di prompt shell
# kosong: halaman AI Agent terlihat "mati" padahal sesinya masih hidup. Skrip
# ini menjalankan agent kembali begitu ia keluar.
#
# Dua hal yang menentukan bentuk kode di bawah:
#
#   1. SIGINT dari Ctrl+C dikirim kernel ke SELURUH foreground process group —
#      agent DAN skrip ini. Supervisor karena itu mengabaikan INT (trap ''),
#      kalau tidak ia ikut mati bersama agent dan tidak ada yang memuat ulang.
#
#   2. Disposisi "abaikan" itu diwariskan menembus exec. Kalau dibiarkan,
#      Ctrl+C tidak akan berpengaruh apa pun di dalam agent — padahal di
#      Claude Code satu Ctrl+C membatalkan operasi berjalan, dan itu justru
#      fungsi yang paling sering dipakai. Karena itu anaknya mengembalikan
#      INT ke default (trap - INT) sebelum exec.
#
# Batas crash: agent yang gagal jalan (belum login, API key salah) akan keluar
# seketika berulang-ulang. Memuat ulang tanpa henti membuat panel memutar loop
# yang tidak pernah bisa berhasil, jadi setelah beberapa kali keluar-cepat
# berturut-turut supervisor menyerah dan menyerahkan shell ke user.

AGENT="$1"
[ -n "$AGENT" ] || exit 64

# Installer resmi para agent tidak sepakat soal ke mana binernya diletakkan,
# dan sebagian tujuannya belum tentu ada di PATH sesi panel:
#
#   claude.ai/install.sh              $HOME/.local/bin
#   chatgpt.com/codex/install.sh      $HOME/.local/bin
#   hermes-agent…/install.sh          $HOME/.local/bin (non-root)
#                                     /usr/local/bin   (root)
#   opencode.ai/install               $HOME/.opencode/bin
#   openclaw.ai/install.sh            bin global npm, atau prefix npm milik
#                                     user, atau $HOME/.openclaw/tools/node/bin
#                                     kalau ia memasang Node.js user-space
#
# Daftar direktori di bawah HARUS tetap sinkron dengan dirBinAgen di
# internal/helper/aiagent.go: yang satu memutuskan apakah kartu Components
# menyalakan tanda "terpasang", yang satu memutuskan apakah sesi ini benar-
# benar menemukan binernya. Kalau keduanya berbeda, panel akan mengaku
# terpasang lalu gagal menjalankannya — atau sebaliknya.
#
# Tanpa penyesuaian ini, agent yang sebenarnya SUDAH terpasang dilaporkan
# "belum terpasang" hanya karena direktorinya tidak diwarisi PTY panel — dan
# saran yang menyertainya (pasang ulang) tidak akan pernah memperbaikinya.
#
# Direktori milik user diletakkan DI DEPAN /usr/bin, bukan di belakang. Mesin
# yang pernah memasang agent lewat panel versi lama masih menyimpan biner npm
# global milik root di /usr/bin — biner yang bisa dijalankan tapi tidak pernah
# bisa memperbarui dirinya sendiri. Kalau jalur sistem tetap menang, memasang
# ulang lewat installer resmi tidak akan mengubah apa pun yang dirasakan user:
# yang jalan tetap salinan lama yang rusak itu. Instalasi milik user memang
# yang lebih baru dan satu-satunya yang utuh, jadi ia yang harus menang —
# sama seperti urutan yang dipasang installer resmi ke berkas rc login.
for d in /usr/local/bin "$HOME/.bun/bin" "$HOME/.npm-global/bin" \
	"$HOME/.openclaw/tools/node/bin" "$HOME/.opencode/bin" "$HOME/.local/bin"; do
	case ":$PATH:" in
		*":$d:"*) ;;
		*) [ -d "$d" ] && PATH="$d:$PATH" ;;
	esac
done
export PATH

# Prefix npm milik user (mis. $HOME/.npm-global/bin) baru ditanyakan kalau
# agent masih belum ketemu: `npm prefix -g` menambah ratusan milidetik pada
# setiap pembukaan sesi, dan mayoritas mesin tidak membutuhkannya.
if ! command -v "$AGENT" >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
	npmbin="$(npm prefix -g 2>/dev/null)/bin"
	[ -x "$npmbin/$AGENT" ] && PATH="$PATH:$npmbin" && export PATH
fi

# Preflight. Agent yang binary-nya tidak ada di PATH tidak akan pernah
# berhasil dimuat ulang, jadi memutar loop tiga kali hanya menunda pesan yang
# sama sambil menutupinya dengan error yang tidak bisa ditindaklanjuti dari
# dalam panel. Sebut langkah berikutnya, lalu berhenti.
if ! command -v "$AGENT" >/dev/null 2>&1; then
	printf '\033[33m[panel] %s belum terpasang di mesin ini.\033[0m\n' "$AGENT"
	printf '\033[2m[panel] Pasang lewat Settings → Components, lalu buka lagi halaman AI Agent.\033[0m\n'
	exit 127
fi

# Agent yang dipasang lewat npm — claude, codex, openclaw — memakai pembungkus
# yang menyimpan biner tiap platform sebagai optionalDependency, dan
# postinstall-lah yang menyalin biner itu menimpa placeholder di bin/. Kalau
# postinstall tidak tuntas, yang tersisa di PATH adalah placeholder yang
# badannya hanya mencetak "native binary not installed" lalu exit 1.
#
# Itu bukan kasus langka. npm 12 memblokir install script secara bawaan
# kecuali paketnya masuk allowScripts, jadi di mesin ber-npm 12 SETIAP
# pemasangan dan pembaruan claude berakhir seperti ini sampai seseorang
# menjalankan `npm config set allow-scripts=@anthropic-ai/claude-code`.
# Perhatikan bahwa ignore-scripts=false tidak menolong: itu mekanisme lain.
#
# Kondisi itu LOLOS preflight di atas: berkasnya memang ada. Yang gagal
# adalah setiap eksekusinya, dalam hitungan milidetik, sehingga user melihat
# tiga salinan error yang sama lalu loop berhenti — padahal binernya sudah
# ada di disk dan perbaikannya satu perintah. Karena penyembuhnya ikut dalam
# paket yang sama, panel bisa menjalankannya sendiri alih-alih menyuruh user
# memasang ulang sesuatu yang sebenarnya sudah terpasang.
#
# Kerusakan ini dikenali dari BERKASNYA, bukan dengan menjalankan agent.
# Menjalankannya untuk menguji — "$AGENT" --version — terlihat menggoda dan
# lolos di semua tes bertulis </dev/null, tapi di PTY panel ada tty sungguhan:
# agent yang tidak mengenal --version akan membuka sesi interaktif dan
# preflight menggantung selamanya. Panel yang membeku lebih buruk daripada
# panel yang menampilkan pesan error, jadi jangan pernah eksekusi di sini.
#
# Dua syarat, keduanya murni pembacaan berkas dan tersedia di POSIX sh mana
# pun (busybox di Alpine, LXC minimal, ARM, musl — semuanya sama):
#
#   1. install.cjs bersebelahan  -> paket ini memang memakai pola pembungkus
#                                   npm tadi; agent lain tidak akan tersentuh
#   2. launcher berukuran mungil -> biner native yang tersalin berukuran
#                                   ratusan MB, sedangkan placeholder-nya di
#                                   bawah satu kilobyte. Ambang 4 KB membuat
#                                   uji ini tidak bergantung pada kata-kata di
#                                   dalam placeholder, yang bisa berubah
#                                   sewaktu-waktu tanpa memberi tahu kita.
#
# Dicoba sekali. Kalau paket native-nya memang tidak ada (--omit=optional),
# install.cjs keluar tanpa mengubah apa pun dan batas crash di bawah yang
# mengambil alih dengan pesannya sendiri.
alat=$(command -v "$AGENT")
nyata=$(readlink -f "$alat" 2>/dev/null) || nyata=$alat
[ -n "$nyata" ] || nyata=$alat
paket=$(dirname "$(dirname "$nyata")")
ukuran=$(wc -c < "$nyata" 2>/dev/null) || ukuran=0

if [ -f "$paket/install.cjs" ] && [ "${ukuran:-0}" -lt 4096 ] && command -v node >/dev/null 2>&1; then
	printf '\033[2m[panel] instalasi %s tidak tuntas — memperbaiki…\033[0m\n' "$AGENT"
	node "$paket/install.cjs" >/dev/null 2>&1
fi

BATAS_CEPAT=5   # detik; keluar lebih cepat dari ini dihitung "gagal jalan"
BATAS_ULANG=3   # gagal-cepat berturut-turut sebelum menyerah

trap '' INT

beruntun=0
while :; do
	mulai=$(date +%s)
	# Subshell: INT dikembalikan ke default HANYA untuk anak, supervisor
	# tetap kebal. exec supaya agent menggantikan subshell — tidak ada
	# proses perantara yang ikut menerima sinyal.
	( trap - INT; exec "$AGENT" )
	kode=$?
	selisih=$(( $(date +%s) - mulai ))

	if [ "$selisih" -lt "$BATAS_CEPAT" ]; then
		beruntun=$(( beruntun + 1 ))
	else
		beruntun=0
	fi

	if [ "$beruntun" -ge "$BATAS_ULANG" ]; then
		printf '\n\033[33m[panel] %s keluar %s kali beruntun dalam hitungan detik — pemuatan ulang dihentikan.\033[0m\n' "$AGENT" "$BATAS_ULANG"
		# Pesan sebelumnya ("jalankan sendiri untuk melihat errornya") tidak
		# menolong: error-nya SUDAH tercetak tiga kali tepat di atas baris ini.
		# Yang belum diketahui user adalah apa yang harus dilakukan dengan
		# error itu, dan untuk kerusakan instalasi jawabannya selalu sama.
		printf '\033[2m[panel] Kalau pesan di atas menyebut instalasi yang cacat, pasang ulang\033[0m\n'
		printf '\033[2m[panel] agent ini lewat Settings → Components. Kalau ia meminta login atau\033[0m\n'
		printf '\033[2m[panel] pemilihan provider, jawab dulu di terminal ini lalu tekan Refresh.\033[0m\n'
		exit "$kode"
	fi

	printf '\n\033[2m[panel] %s keluar (kode %s) — memuat ulang…\033[0m\n' "$AGENT" "$kode"
	sleep 1
done
